package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"wealthflow/backend/internal/store"
)

// FXRevaluer re-converts assets whose INR value derives from a foreign
// amount — plain foreign-currency assets (amount × rate) and travel
// points (stored USD total × USD rate). Without this their INR value is
// fixed at whatever the rate was when the asset was last edited.
type FXRevaluer struct {
	Store    *store.Store
	Rates    *Rates
	Snapshot *Snapshot
	Log      *slog.Logger

	// Serializes revaluations (nightly job, on-read) and guards lastAttempt.
	mu          sync.Mutex
	lastAttempt time.Time
}

// Revalue runs unconditionally (nightly job path).
func (f *FXRevaluer) Revalue(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revalueLocked(ctx)
}

// RevalueIfStale runs at most once per maxAge (dashboard read path); the
// FX feed updates once a day, so a generous window is fine.
func (f *FXRevaluer) RevalueIfStale(ctx context.Context, maxAge time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.lastAttempt) < maxAge {
		return
	}
	_ = f.revalueLocked(ctx)
}

func (f *FXRevaluer) revalueLocked(ctx context.Context) error {
	f.lastAttempt = time.Now()
	// Rates.Get serves its cache (≤30 min old) or fetches; the hardcoded
	// fallback inside it only applies when the cache is empty AND the FX
	// API is down — practically never once the cache has been written.
	rates, err := f.Rates.Get(ctx)
	if err != nil {
		return err
	}

	updated := 0
	affected := map[string]bool{}

	// Plain foreign-currency assets: INR = foreign amount × today's rate.
	foreign, err := f.Store.ListForeignAssets(ctx)
	if err != nil {
		return err
	}
	for _, a := range foreign {
		if a.ForeignAmount == nil || *a.ForeignAmount <= 0 || a.ForeignCurrency == nil {
			continue
		}
		rate := rates[*a.ForeignCurrency]
		if rate <= 0 {
			continue
		}
		if err := f.Store.SetAssetCurrentValue(ctx, a.ID, *a.ForeignAmount*rate); err != nil {
			if f.Log != nil {
				f.Log.Error("fx revalue failed", "asset", a.ID, "err", err)
			}
			continue
		}
		updated++
		if a.UserID != nil {
			affected[*a.UserID] = true
		}
	}

	// Travel points: programs are valued in USD; re-convert that total.
	travel, err := f.Store.ListTravelPointAssets(ctx)
	if err != nil {
		return err
	}
	usdRate := rates["USD"]
	for _, a := range travel {
		if usdRate <= 0 || a.TotalValueUSD == nil || *a.TotalValueUSD <= 0 {
			continue
		}
		if err := f.Store.SetAssetTravelValuation(ctx, a.ID, *a.TotalValueUSD*usdRate); err != nil {
			if f.Log != nil {
				f.Log.Error("fx travel revalue failed", "asset", a.ID, "err", err)
			}
			continue
		}
		updated++
		if a.UserID != nil {
			affected[*a.UserID] = true
		}
	}

	if updated > 0 && f.Log != nil {
		f.Log.Info("fx assets revalued", "count", updated)
	}
	// Keep today's snapshot (which powers the chart) in step.
	if f.Snapshot != nil {
		for uid := range affected {
			if err := f.Snapshot.CreateDailySnapshot(ctx, uid); err != nil && f.Log != nil {
				f.Log.Error("post-fx-revalue snapshot failed", "user", uid, "err", err)
			}
		}
	}
	return nil
}
