package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/store"
)

// Yahoo Finance's unofficial endpoints: free, no key, and they cover both
// US and LSE listings (the user's UCITS ETFs trade in USD on the LSE).
// Quotes are cached in stock_prices, so a feed outage only means values
// hold at the last known price. Yahoo rejects Go's default user agent,
// hence the browser UA on every request.
const (
	yahooQuoteURL  = "https://query1.finance.yahoo.com/v8/finance/chart/"
	yahooSearchURL = "https://query2.finance.yahoo.com/v1/finance/search"
	yahooUA        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"
)

const stockSearchLimit = 10

type Stocks struct {
	Store    *store.Store
	Client   *http.Client
	Rates    *Rates
	Snapshot *Snapshot
	Log      *slog.Logger

	// Serializes refreshes (nightly job, on-read) and guards lastAttempt.
	refreshMu   sync.Mutex
	lastAttempt time.Time
}

func (s *Stocks) get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", yahooUA)
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// Search proxies ticker search for the holdings editor.
func (s *Stocks) Search(ctx context.Context, q string) ([]models.StockSearchResult, error) {
	u := yahooSearchURL + "?newsCount=0&quotesCount=10&q=" + url.QueryEscape(q)
	resp, err := s.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stock search: %s", resp.Status)
	}
	var body struct {
		Quotes []struct {
			Symbol    string `json:"symbol"`
			ShortName string `json:"shortname"`
			LongName  string `json:"longname"`
			ExchDisp  string `json:"exchDisp"`
		} `json:"quotes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]models.StockSearchResult, 0, len(body.Quotes))
	for _, q := range body.Quotes {
		if q.Symbol == "" {
			continue
		}
		name := q.LongName
		if name == "" {
			name = q.ShortName
		}
		out = append(out, models.StockSearchResult{Symbol: q.Symbol, Name: name, Exchange: q.ExchDisp})
		if len(out) == stockSearchLimit {
			break
		}
	}
	return out, nil
}

// fetchQuote pulls the live quote for one ticker. Non-USD instruments are
// rejected: the portfolio math assumes USD (e.g. an LSE line quoted in GBp
// would silently value pennies as dollars).
func (s *Stocks) fetchQuote(ctx context.Context, symbol string) (store.StockPriceDoc, error) {
	resp, err := s.get(ctx, yahooQuoteURL+url.PathEscape(symbol)+"?interval=1d&range=1d")
	if err != nil {
		return store.StockPriceDoc{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return store.StockPriceDoc{}, fmt.Errorf("stock quote %s: %s", symbol, resp.Status)
	}
	var body struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Symbol             string  `json:"symbol"`
					Currency           string  `json:"currency"`
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					LongName           string  `json:"longName"`
					ShortName          string  `json:"shortName"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return store.StockPriceDoc{}, err
	}
	if len(body.Chart.Result) == 0 {
		return store.StockPriceDoc{}, fmt.Errorf("stock quote %s: no data", symbol)
	}
	meta := body.Chart.Result[0].Meta
	if meta.RegularMarketPrice <= 0 {
		return store.StockPriceDoc{}, fmt.Errorf("stock quote %s: no usable price", symbol)
	}
	if !strings.EqualFold(meta.Currency, "USD") {
		return store.StockPriceDoc{}, fmt.Errorf("%s is quoted in %s, not USD — pick the USD-quoted listing", symbol, meta.Currency)
	}
	name := meta.LongName
	if name == "" {
		name = meta.ShortName
	}
	return store.StockPriceDoc{Symbol: symbol, Name: name, PriceUSD: meta.RegularMarketPrice}, nil
}

// QuoteFor returns a ticker's quote from cache, fetching (and caching) it
// on demand for tickers nobody held before.
func (s *Stocks) QuoteFor(ctx context.Context, symbol string) (store.StockPriceDoc, error) {
	doc, found, err := s.Store.GetStockPrice(ctx, symbol)
	if err != nil {
		return store.StockPriceDoc{}, err
	}
	if found {
		return doc, nil
	}
	doc, err = s.fetchQuote(ctx, symbol)
	if err != nil {
		return store.StockPriceDoc{}, err
	}
	if err := s.Store.UpsertStockPrice(ctx, doc, time.Now()); err != nil {
		return store.StockPriceDoc{}, err
	}
	return doc, nil
}

// ValuePortfolio prices holdings + uninvested cash: (USD total, INR total,
// holdings normalized with canonical name + last price stamped in).
func (s *Stocks) ValuePortfolio(ctx context.Context, holdings []models.StockHolding, cashUSD float64) (usd, inr float64, normalized []models.StockHolding, err error) {
	normalized = make([]models.StockHolding, 0, len(holdings))
	for i, h := range holdings {
		symbol := strings.ToUpper(strings.TrimSpace(h.Symbol))
		if symbol == "" {
			return 0, 0, nil, fmt.Errorf("holding %d: ticker required", i+1)
		}
		if h.Units <= 0 {
			return 0, 0, nil, fmt.Errorf("%s: units must be greater than zero", symbol)
		}
		doc, err := s.QuoteFor(ctx, symbol)
		if err != nil {
			return 0, 0, nil, err
		}
		h.Symbol = symbol
		h.Name = doc.Name
		h.LastPrice = doc.PriceUSD
		usd += h.Units * doc.PriceUSD
		normalized = append(normalized, h)
	}
	if cashUSD > 0 {
		usd += cashUSD
	}
	inr = usd * s.usdToINR(ctx)
	return usd, inr, normalized, nil
}

func (s *Stocks) usdToINR(ctx context.Context) float64 {
	if s.Rates != nil {
		if rates, err := s.Rates.Get(ctx); err == nil {
			if v := rates["USD"]; v > 0 {
				return v
			}
		}
	}
	return 83
}

// Refresh re-fetches every held ticker's quote, then revalues all stock
// portfolios. Loud when nothing could be fetched, like the other feeds.
func (s *Stocks) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshLocked(ctx)
}

// RefreshIfStale refreshes only when the last attempt is older than maxAge.
func (s *Stocks) RefreshIfStale(ctx context.Context, maxAge time.Duration) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if time.Since(s.lastAttempt) < maxAge {
		return
	}
	_ = s.refreshLocked(ctx)
}

func (s *Stocks) refreshLocked(ctx context.Context) error {
	s.lastAttempt = time.Now()
	symbols, err := s.Store.DistinctStockSymbols(ctx)
	if err != nil {
		return err
	}
	saved := 0
	var lastErr error
	for _, sym := range symbols {
		doc, err := s.fetchQuote(ctx, sym)
		if err != nil {
			if s.Log != nil {
				s.Log.Error("stock quote fetch failed", "symbol", sym, "err", err)
			}
			lastErr = err
			continue
		}
		if err := s.Store.UpsertStockPrice(ctx, doc, time.Now()); err != nil {
			if s.Log != nil {
				s.Log.Error("stock quote upsert failed", "symbol", sym, "err", err)
			}
			lastErr = err
			continue
		}
		saved++
	}
	if saved > 0 && s.Log != nil {
		s.Log.Info("stock quotes refreshed", "count", saved)
	}
	if err := s.Revalue(ctx); err != nil {
		return err
	}
	if saved == 0 && lastErr != nil {
		return fmt.Errorf("stock refresh: no quotes saved: %w", lastErr)
	}
	return nil
}

// Revalue recomputes every stock portfolio from cached quotes. Assets with
// any uncached ticker are skipped (keep the last full valuation rather
// than a partial one).
func (s *Stocks) Revalue(ctx context.Context) error {
	assets, err := s.Store.ListStockAssets(ctx)
	if err != nil {
		return err
	}
	rate := s.usdToINR(ctx)
	revalued := 0
	affected := map[string]bool{}
	for _, a := range assets {
		if len(a.StockHoldings) == 0 && (a.CashUSD == nil || *a.CashUSD <= 0) {
			continue
		}
		total := 0.0
		updated := make([]models.StockHolding, 0, len(a.StockHoldings))
		ok := true
		for _, h := range a.StockHoldings {
			doc, found, err := s.Store.GetStockPrice(ctx, h.Symbol)
			if err != nil || !found || doc.PriceUSD <= 0 {
				ok = false
				break
			}
			h.Name = doc.Name
			h.LastPrice = doc.PriceUSD
			total += h.Units * doc.PriceUSD
			updated = append(updated, h)
		}
		if !ok {
			continue
		}
		if a.CashUSD != nil && *a.CashUSD > 0 {
			total += *a.CashUSD
		}
		if err := s.Store.SetAssetStockValuation(ctx, a.ID, total*rate, total, updated); err != nil {
			if s.Log != nil {
				s.Log.Error("stock revalue failed", "asset", a.ID, "err", err)
			}
			continue
		}
		revalued++
		if a.UserID != nil {
			affected[*a.UserID] = true
		}
	}
	if revalued > 0 && s.Log != nil {
		s.Log.Info("stock portfolios revalued", "count", revalued)
	}
	// Keep today's snapshot (which powers the chart) in step.
	if s.Snapshot != nil {
		for uid := range affected {
			if err := s.Snapshot.CreateDailySnapshot(ctx, uid); err != nil && s.Log != nil {
				s.Log.Error("post-stock-revalue snapshot failed", "user", uid, "err", err)
			}
		}
	}
	return nil
}
