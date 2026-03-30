package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"go.mongodb.org/mongo-driver/mongo"

	"wealthflow/backend/internal/api"
	"wealthflow/backend/internal/config"
	"wealthflow/backend/internal/service"
	"wealthflow/backend/internal/store"
)

// bootstrapHandler serves /healthz with 200 before Mongo is ready so Fly smoke checks pass.
// Other paths return 503 until the full router is swapped in.
func bootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	http.Error(w, "service starting", http.StatusServiceUnavailable)
}

func main() {
	_ = godotenv.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := config.Load()
	if cfg.MongoURL == "" || cfg.DBName == "" {
		log.Error("MONGO_URL and DB_NAME are required (set Fly secrets: fly secrets set MONGO_URL=... DB_NAME=...)")
		os.Exit(1)
	}

	var handler atomic.Value
	handler.Store(http.HandlerFunc(bootstrapHandler))

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.Load().(http.Handler).ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "phase", "bootstrap")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	var client *mongo.Client
	const mongoAttempts = 12
	for attempt := 1; attempt <= mongoAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		c, err := store.Connect(ctx, cfg.MongoURL)
		cancel()
		if err == nil {
			client = c
			if attempt > 1 {
				log.Info("mongodb connected", "attempt", attempt)
			}
			break
		}
		log.Error("mongodb connect failed", "attempt", attempt, "max", mongoAttempts, "err", err)
		if attempt == mongoAttempts {
			os.Exit(1)
		}
		time.Sleep(5 * time.Second)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()

	st := store.New(client.Database(cfg.DBName))
	snapshotSvc := &service.Snapshot{Store: st}
	ratesSvc := &service.Rates{
		Store:  st,
		Client: &http.Client{Timeout: 15 * time.Second},
		Log:    log,
	}

	h := &api.Handler{
		Config:   cfg,
		Store:    st,
		Snapshot: snapshotSvc,
		Rates:    ratesSvc,
	}
	handler.Store(api.NewRouter(cfg, h))
	log.Info("service ready", "mongodb", true)

	c := cron.New(cron.WithLocation(time.UTC))
	_, err := c.AddFunc("0 0 * * *", func() {
		jobCtx, jobCancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer jobCancel()
		log.Info("scheduled snapshot job started")
		if err := snapshotSvc.CreateSnapshotsForAllUsers(jobCtx); err != nil {
			log.Error("scheduled snapshots failed", "err", err)
		} else {
			log.Info("scheduled snapshot job finished")
		}
	})
	if err != nil {
		log.Error("cron schedule", "err", err)
		os.Exit(1)
	}
	c.Start()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutdown signal received")

	ctxShut, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	c.Stop()
	if err := srv.Shutdown(ctxShut); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}
