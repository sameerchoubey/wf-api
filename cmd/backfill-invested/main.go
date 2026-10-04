// Command backfill-invested fills the invested summary on snapshots
// written before cost tracking shipped, using the asset copies each
// snapshot already holds. Crypto is valued with CoinGecko's historical
// daily prices for the snapshot's date. Assets stored as a single value
// with no holdings (older data) are estimated from each user's current
// invested ÷ value ratio for that kind, and the snapshot is marked
// estimated.
//
// Dry run by default; pass -apply to write. Pass -all to also recompute
// snapshots that already have an invested summary.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"

	"wealthflow/backend/internal/config"
	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/service"
	"wealthflow/backend/internal/store"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run)")
	all := flag.Bool("all", false, "recompute snapshots that already have invested data")
	flag.Parse()

	_ = godotenv.Load()
	cfg := config.Load()
	if cfg.MongoURL == "" || cfg.DBName == "" {
		log.Fatal("MONGO_URL and DB_NAME are required")
	}
	ctx := context.Background()
	client, err := store.Connect(ctx, cfg.MongoURL)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)
	st := store.New(client.Database(cfg.DBName))

	snaps, err := st.AllSnapshots(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Historical crypto prices, fetched once per coin.
	history := map[string]map[string]float64{}
	for sym := range map[string]bool{"BTC": true, "ETH": true} {
		h, err := service.CoinGeckoDailyUSD(ctx, sym, 365)
		if err != nil {
			log.Printf("warning: %v — %s will be left out", err, sym)
			continue
		}
		history[sym] = h
		time.Sleep(2 * time.Second) // stay well under the public rate limit
	}

	// Per-user cost ratios from today's assets, for estimating flat history.
	ratiosByUser := map[string]map[string]float64{}
	live := &service.CostBasis{Store: st}

	var updated, skipped, missingPrice, failed int
	for _, sn := range snaps {
		if sn.Invested != nil && !*all {
			skipped++
			continue
		}
		assets, err := decodeAssets(sn.Assets)
		if err != nil {
			log.Printf("%s %s: cannot decode assets: %v", sn.UserID, sn.Date, err)
			failed++
			continue
		}
		date := sn.Date
		cb := &service.CostBasis{
			Store: st,
			CryptoPriceUSD: func(_ context.Context, symbol string) (float64, bool) {
				p, ok := history[symbol][date]
				if !ok {
					missingPrice++
				}
				return p, ok
			},
		}
		ratios, ok := ratiosByUser[sn.UserID]
		if !ok {
			current, err := st.ListAssetsByUser(ctx, sn.UserID)
			if err != nil {
				log.Fatalf("%s: list assets: %v", sn.UserID, err)
			}
			live.Totals(ctx, current)
			ratios = service.Ratios(current)
			ratiosByUser[sn.UserID] = ratios
		}
		cb.Totals(ctx, assets)
		for i := range assets {
			service.Estimate(&assets[i], ratios)
		}
		inv := service.Sum(assets)
		est := ""
		if inv.Estimated {
			est = "  (estimated)"
		}
		fmt.Printf("%s  user=%s  invested=%12.0f  current=%12.0f  untracked=%12.0f%s\n",
			sn.Date, sn.UserID[:8], inv.Invested, inv.Current, inv.Untracked, est)
		if *apply {
			if err := st.SetSnapshotInvested(ctx, sn.UserID, sn.Date, inv); err != nil {
				log.Fatalf("%s %s: %v", sn.UserID, sn.Date, err)
			}
		}
		updated++
	}

	mode := "DRY RUN — nothing written"
	if *apply {
		mode = "written"
	}
	fmt.Fprintf(os.Stderr, "\n%d snapshots %s, %d already had data (skipped), %d FAILED to decode, %d crypto lookups had no price for that date\n",
		updated, mode, skipped, failed, missingPrice)
	if failed > 0 {
		os.Exit(1)
	}
}

func decodeAssets(raw []interface{}) ([]models.Asset, error) {
	out := make([]models.Asset, 0, len(raw))
	for _, r := range raw {
		b, err := bson.Marshal(r)
		if err != nil {
			return nil, err
		}
		// Older snapshots stored updated_at as a BSON date; it isn't needed here.
		var m bson.M
		if err := bson.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		delete(m, "updated_at")
		if b, err = bson.Marshal(m); err != nil {
			return nil, err
		}
		var a models.Asset
		if err := bson.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
