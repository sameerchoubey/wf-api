package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/store"
)

// IBJA (India Bullion & Jewellers Association) publishes the official
// domestic gold rates — duty and local premium included, which no
// international spot API reflects. There is no official API, so the
// rates page is parsed for its stable ASP.NET label spans, e.g.
//   <span id="lblGold916_PM">144935</span>   (₹ per 10 grams)
// Rates are cached in gold_rates, so a page redesign only means values
// hold at the last known rate while the nightly run turns red.
const ibjaURL = "https://www.ibjarates.com"

const ibjaUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"

// PM rates preferred (published ~5pm IST); AM is the fallback for the
// window before that day's PM fixing exists.
var ibjaLabelRe = regexp.MustCompile(`id="lblGold(999|916|750)_(AM|PM)">\s*([0-9][0-9,.]*)`)

type Gold struct {
	Store    *store.Store
	Client   *http.Client
	Snapshot *Snapshot
	Log      *slog.Logger

	// Serializes refreshes (nightly job, on-read) and guards lastAttempt.
	refreshMu   sync.Mutex
	lastAttempt time.Time
}

// fetchRates scrapes the IBJA page into ₹/gram rates.
func (g *Gold) fetchRates(ctx context.Context) (store.GoldRatesDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ibjaURL, nil)
	if err != nil {
		return store.GoldRatesDoc{}, err
	}
	req.Header.Set("User-Agent", ibjaUA)
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return store.GoldRatesDoc{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return store.GoldRatesDoc{}, fmt.Errorf("ibja: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return store.GoldRatesDoc{}, err
	}

	// per10g[purity][session] = value
	per10g := map[string]map[string]float64{}
	for _, m := range ibjaLabelRe.FindAllStringSubmatch(string(body), -1) {
		clean := ""
		for _, r := range m[3] {
			if r >= '0' && r <= '9' || r == '.' {
				clean += string(r)
			}
		}
		v, err := strconv.ParseFloat(clean, 64)
		if err != nil || v <= 0 {
			continue
		}
		if per10g[m[1]] == nil {
			per10g[m[1]] = map[string]float64{}
		}
		per10g[m[1]][m[2]] = v
	}

	pick := func(purity string) float64 {
		if v := per10g[purity]["PM"]; v > 0 {
			return v / 10
		}
		return per10g[purity]["AM"] / 10
	}
	doc := store.GoldRatesDoc{Rate24K: pick("999"), Rate22K: pick("916"), Rate18K: pick("750")}
	// Sanity: all three present and ordered by purity, or the page layout
	// changed and we must not cache garbage.
	if !(doc.Rate24K > doc.Rate22K && doc.Rate22K > doc.Rate18K && doc.Rate18K > 0) {
		return store.GoldRatesDoc{}, fmt.Errorf("ibja: could not parse rates (page layout changed?)")
	}
	return doc, nil
}

// Rates returns cached rates, fetching on demand when the cache is empty.
func (g *Gold) Rates(ctx context.Context) (store.GoldRatesDoc, error) {
	doc, found, err := g.Store.GetGoldRates(ctx)
	if err != nil {
		return store.GoldRatesDoc{}, err
	}
	if found {
		return doc, nil
	}
	doc, err = g.fetchRates(ctx)
	if err != nil {
		return store.GoldRatesDoc{}, err
	}
	if err := g.Store.UpsertGoldRates(ctx, doc, time.Now()); err != nil {
		return store.GoldRatesDoc{}, err
	}
	return doc, nil
}

func rateForKarat(doc store.GoldRatesDoc, karat string) float64 {
	switch karat {
	case "24":
		return doc.Rate24K
	case "22":
		return doc.Rate22K
	case "18":
		return doc.Rate18K
	}
	return 0
}

// ValueHoldings prices grams × karat rate and returns the holdings
// normalized with the applied rate stamped in.
func (g *Gold) ValueHoldings(ctx context.Context, holdings []models.GoldHolding) (inr float64, normalized []models.GoldHolding, err error) {
	doc, err := g.Rates(ctx)
	if err != nil {
		return 0, nil, err
	}
	normalized = make([]models.GoldHolding, 0, len(holdings))
	for i, h := range holdings {
		rate := rateForKarat(doc, h.Karat)
		if rate <= 0 {
			return 0, nil, fmt.Errorf("item %d: karat must be 24, 22 or 18", i+1)
		}
		if h.Grams <= 0 {
			return 0, nil, fmt.Errorf("item %d: grams must be greater than zero", i+1)
		}
		h.LastRate = rate
		inr += h.Grams * rate
		normalized = append(normalized, h)
	}
	return inr, normalized, nil
}

// Refresh re-fetches the IBJA rates, then revalues all gold portfolios.
// Loud on failure, like the other feeds.
func (g *Gold) Refresh(ctx context.Context) error {
	g.refreshMu.Lock()
	defer g.refreshMu.Unlock()
	return g.refreshLocked(ctx)
}

// RefreshIfStale refreshes only when the last attempt is older than maxAge
// (rates publish twice per business day).
func (g *Gold) RefreshIfStale(ctx context.Context, maxAge time.Duration) {
	g.refreshMu.Lock()
	defer g.refreshMu.Unlock()
	if time.Since(g.lastAttempt) < maxAge {
		return
	}
	_ = g.refreshLocked(ctx)
}

func (g *Gold) refreshLocked(ctx context.Context) error {
	g.lastAttempt = time.Now()
	doc, err := g.fetchRates(ctx)
	if err != nil {
		return fmt.Errorf("gold refresh: %w", err)
	}
	if err := g.Store.UpsertGoldRates(ctx, doc, time.Now()); err != nil {
		return err
	}
	if g.Log != nil {
		g.Log.Info("gold rates refreshed", "rate_22k", doc.Rate22K, "rate_24k", doc.Rate24K)
	}
	return g.Revalue(ctx)
}

// Revalue recomputes every gold portfolio from the cached rates.
func (g *Gold) Revalue(ctx context.Context) error {
	doc, found, err := g.Store.GetGoldRates(ctx)
	if err != nil || !found {
		return err
	}
	assets, err := g.Store.ListGoldAssets(ctx)
	if err != nil {
		return err
	}
	revalued := 0
	affected := map[string]bool{}
	for _, a := range assets {
		if len(a.GoldHoldings) == 0 {
			continue
		}
		total := 0.0
		updated := make([]models.GoldHolding, 0, len(a.GoldHoldings))
		ok := true
		for _, h := range a.GoldHoldings {
			rate := rateForKarat(doc, h.Karat)
			if rate <= 0 || h.Grams <= 0 {
				ok = false
				break
			}
			h.LastRate = rate
			total += h.Grams * rate
			updated = append(updated, h)
		}
		if !ok {
			continue
		}
		if err := g.Store.SetAssetGoldValuation(ctx, a.ID, total, updated); err != nil {
			if g.Log != nil {
				g.Log.Error("gold revalue failed", "asset", a.ID, "err", err)
			}
			continue
		}
		revalued++
		if a.UserID != nil {
			affected[*a.UserID] = true
		}
	}
	if revalued > 0 && g.Log != nil {
		g.Log.Info("gold portfolios revalued", "count", revalued)
	}
	// Keep today's snapshot (which powers the chart) in step.
	if g.Snapshot != nil {
		for uid := range affected {
			if err := g.Snapshot.CreateDailySnapshot(ctx, uid); err != nil && g.Log != nil {
				g.Log.Error("post-gold-revalue snapshot failed", "user", uid, "err", err)
			}
		}
	}
	return nil
}
