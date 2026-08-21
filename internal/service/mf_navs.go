package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/store"
)

// mfapi.in serves AMFI NAV data as JSON (amfiindia.com blocks non-browser
// clients). NAVs are cached in our own mf_navs collection, so a feed
// outage only means values hold at the last known NAV.
const mfAPIBase = "https://api.mfapi.in/mf"

const mfSearchLimit = 20

type MFNavs struct {
	Store  *store.Store
	Client *http.Client
	Log    *slog.Logger

	// Serializes refreshes (cron, boot, on-read) and guards lastAttempt.
	refreshMu   sync.Mutex
	lastAttempt time.Time
}

// RefreshIfStale refreshes only when the last attempt is older than maxAge
// (NAVs change once per business day, so callers pass a generous window).
func (m *MFNavs) RefreshIfStale(ctx context.Context, maxAge time.Duration) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if time.Since(m.lastAttempt) < maxAge {
		return
	}
	_ = m.refreshLocked(ctx)
}

func (m *MFNavs) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return http.DefaultClient
}

// Search proxies fund search so the frontend never talks to the feed
// directly (no CORS issues, and the source can be swapped server-side).
func (m *MFNavs) Search(ctx context.Context, q string) ([]models.MFSearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mfAPIBase+"/search?q="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mf search: %s", resp.Status)
	}
	var out []models.MFSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out) > mfSearchLimit {
		out = out[:mfSearchLimit]
	}
	return out, nil
}

// fetchLatest pulls the latest NAV for one scheme from the feed.
func (m *MFNavs) fetchLatest(ctx context.Context, schemeCode string) (store.MFNavDoc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mfAPIBase+"/"+url.PathEscape(schemeCode)+"/latest", nil)
	if err != nil {
		return store.MFNavDoc{}, err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return store.MFNavDoc{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return store.MFNavDoc{}, fmt.Errorf("mf nav %s: %s", schemeCode, resp.Status)
	}
	var body struct {
		Meta struct {
			SchemeName string `json:"scheme_name"`
		} `json:"meta"`
		Data []struct {
			Date string `json:"date"`
			NAV  string `json:"nav"`
		} `json:"data"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return store.MFNavDoc{}, err
	}
	if len(body.Data) == 0 || body.Meta.SchemeName == "" {
		return store.MFNavDoc{}, fmt.Errorf("mf nav %s: no data", schemeCode)
	}
	nav, err := strconv.ParseFloat(body.Data[0].NAV, 64)
	if err != nil || nav <= 0 {
		return store.MFNavDoc{}, fmt.Errorf("mf nav %s: bad NAV %q", schemeCode, body.Data[0].NAV)
	}
	return store.MFNavDoc{
		SchemeCode: schemeCode,
		SchemeName: body.Meta.SchemeName,
		NAV:        nav,
		NAVDate:    body.Data[0].Date,
	}, nil
}

// navFor returns a scheme's NAV from cache, fetching (and caching) it on
// demand for schemes nobody held before.
func (m *MFNavs) NavFor(ctx context.Context, schemeCode string) (store.MFNavDoc, error) {
	doc, found, err := m.Store.GetMFNav(ctx, schemeCode)
	if err != nil {
		return store.MFNavDoc{}, err
	}
	if found {
		return doc, nil
	}
	doc, err = m.fetchLatest(ctx, schemeCode)
	if err != nil {
		return store.MFNavDoc{}, err
	}
	if err := m.Store.UpsertMFNav(ctx, doc, time.Now()); err != nil {
		return store.MFNavDoc{}, err
	}
	return doc, nil
}

// ValuePortfolio prices a set of MF holdings in INR and returns them
// normalized (canonical scheme name + last NAV stamped in).
func (m *MFNavs) ValuePortfolio(ctx context.Context, holdings []models.MFHolding) (inr float64, normalized []models.MFHolding, err error) {
	normalized = make([]models.MFHolding, 0, len(holdings))
	for i, h := range holdings {
		code := strings.TrimSpace(h.SchemeCode)
		if code == "" {
			return 0, nil, fmt.Errorf("fund %d: scheme required", i+1)
		}
		if h.Units <= 0 {
			return 0, nil, fmt.Errorf("fund %d: units must be greater than zero", i+1)
		}
		doc, err := m.NavFor(ctx, code)
		if err != nil {
			return 0, nil, err
		}
		h.SchemeCode = code
		h.SchemeName = doc.SchemeName
		h.LastNAV = doc.NAV
		h.NAVDate = doc.NAVDate
		inr += h.Units * doc.NAV
		normalized = append(normalized, h)
	}
	return inr, normalized, nil
}

// Refresh re-fetches the NAV of every scheme held by any user, then
// revalues all MF portfolios. Runs at boot and on the daily cron (NAVs
// publish once per business day).
func (m *MFNavs) Refresh(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	return m.refreshLocked(ctx)
}

func (m *MFNavs) refreshLocked(ctx context.Context) error {
	m.lastAttempt = time.Now()
	codes, err := m.Store.DistinctMFSchemeCodes(ctx)
	if err != nil {
		return err
	}
	for _, code := range codes {
		doc, err := m.fetchLatest(ctx, code)
		if err != nil {
			if m.Log != nil {
				m.Log.Error("mf nav fetch failed", "scheme", code, "err", err)
			}
			continue
		}
		if err := m.Store.UpsertMFNav(ctx, doc, time.Now()); err != nil && m.Log != nil {
			m.Log.Error("mf nav upsert failed", "scheme", code, "err", err)
		}
	}
	if len(codes) > 0 && m.Log != nil {
		m.Log.Info("mf navs refreshed", "count", len(codes))
	}
	return m.Revalue(ctx)
}

// Revalue recomputes every MF portfolio from cached NAVs. Assets with any
// uncached scheme are skipped (keep the last full valuation rather than a
// partial one).
func (m *MFNavs) Revalue(ctx context.Context) error {
	assets, err := m.Store.ListMFAssets(ctx)
	if err != nil {
		return err
	}
	revalued := 0
	for _, a := range assets {
		if len(a.MFHoldings) == 0 {
			continue
		}
		total := 0.0
		updated := make([]models.MFHolding, 0, len(a.MFHoldings))
		ok := true
		for _, h := range a.MFHoldings {
			doc, found, err := m.Store.GetMFNav(ctx, h.SchemeCode)
			if err != nil || !found || doc.NAV <= 0 {
				ok = false
				break
			}
			h.SchemeName = doc.SchemeName
			h.LastNAV = doc.NAV
			h.NAVDate = doc.NAVDate
			total += h.Units * doc.NAV
			updated = append(updated, h)
		}
		if !ok {
			continue
		}
		if err := m.Store.SetAssetMFValuation(ctx, a.ID, total, updated); err != nil {
			if m.Log != nil {
				m.Log.Error("mf revalue failed", "asset", a.ID, "err", err)
			}
			continue
		}
		revalued++
	}
	if revalued > 0 && m.Log != nil {
		m.Log.Info("mf portfolios revalued", "count", revalued)
	}
	return nil
}
