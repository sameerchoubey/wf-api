package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"wealthflow/backend/internal/store"
)

const exchangeAPI = "https://api.exchangerate-api.com/v4/latest/USD"

const cacheTTL = 30 * time.Minute

type Rates struct {
	Store  *store.Store
	Client *http.Client
	Log    *slog.Logger
}

func (r *Rates) Get(ctx context.Context) (map[string]float64, error) {
	rates, ts, ok, err := r.Store.GetExchangeRatesCache(ctx)
	if err != nil {
		return nil, err
	}
	if ok && !ts.IsZero() && time.Since(ts) < cacheTTL && len(rates) > 0 {
		if r.Log != nil {
			r.Log.Info("returning cached exchange rates", "age", time.Since(ts))
		}
		return rates, nil
	}
	if r.Log != nil {
		r.Log.Info("fetching exchange rates from API")
	}
	fetched, err := r.fetchFromAPI(ctx)
	if err != nil {
		if r.Log != nil {
			r.Log.Error("failed to fetch exchange rates", "err", err)
		}
		if len(rates) > 0 {
			return rates, nil
		}
		return map[string]float64{"USD": 83, "EUR": 90}, nil
	}
	now := time.Now().UTC()
	if err := r.Store.UpsertExchangeRatesCache(ctx, fetched, now); err != nil {
		return nil, err
	}
	return fetched, nil
}

func (r *Rates) fetchFromAPI(ctx context.Context) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, exchangeAPI, nil)
	if err != nil {
		return nil, err
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange API: %s", resp.Status)
	}
	var body struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	usdToINR := body.Rates["INR"]
	if usdToINR == 0 {
		usdToINR = 83
	}
	usdToEUR := body.Rates["EUR"]
	if usdToEUR == 0 {
		usdToEUR = 0.92
	}
	eurToUSD := 1 / usdToEUR
	eurToINR := eurToUSD * usdToINR
	return map[string]float64{
		"USD": usdToINR,
		"EUR": eurToINR,
	}, nil
}
