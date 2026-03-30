package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"wealthflow/backend/internal/config"
	"wealthflow/backend/internal/middleware"
)

func NewRouter(cfg config.Config, h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			for _, o := range cfg.CORSOrigins {
				if o == "*" || o == origin {
					return true
				}
			}
			return false
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           int(12 * time.Hour / time.Second),
	}))

	r.Route("/api", func(r chi.Router) {
		r.Post("/register", h.Register)
		r.Post("/login", h.Login)
		r.Get("/exchange-rates", h.ExchangeRates)

		r.Group(func(r chi.Router) {
			r.Use(middleware.BearerAuth(cfg.JWTSecret))
			r.Get("/assets", h.ListAssets)
			r.Post("/assets", h.CreateAsset)
			r.Put("/assets/{assetID}", h.UpdateAsset)
			r.Delete("/assets/{assetID}", h.DeleteAsset)

			r.Get("/liabilities", h.ListLiabilities)
			r.Post("/liabilities", h.CreateLiability)
			r.Put("/liabilities/{liabilityID}", h.UpdateLiability)
			r.Delete("/liabilities/{liabilityID}", h.DeleteLiability)

			r.Get("/snapshots", h.ListSnapshots)
			r.Get("/dashboard", h.Dashboard)
		})
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return r
}
