package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"wealthflow/backend/internal/models"
)

// NormalizeSymbol canonicalizes user input like " btc " to "BTC".
func NormalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

// PriceUSD returns the cached market price for a symbol. If the symbol is
// not cached yet (a coin no user held before), it is fetched from the API
// once and cached, so newly added coins work immediately.
func (c *CryptoPrices) PriceUSD(ctx context.Context, symbol string) (float64, error) {
	symbol = NormalizeSymbol(symbol)
	doc, found, err := c.Store.GetCryptoPrice(ctx, symbol)
	if err != nil {
		return 0, err
	}
	if !found {
		if c.APIKey == "" {
			return 0, fmt.Errorf("no cached price for %s", symbol)
		}
		client := c.Client
		if client == nil {
			client = http.DefaultClient
		}
		payload, err := c.fetchSymbol(ctx, client, symbol)
		if err != nil {
			return 0, fmt.Errorf("could not fetch price for %s: %w", symbol, err)
		}
		if err := c.Store.UpsertCryptoPrice(ctx, symbol, payload, time.Now().UTC()); err != nil {
			return 0, err
		}
		doc.Payload = payload
	}
	price, ok := priceFromPayload(doc.Payload)
	if !ok || price <= 0 {
		return 0, fmt.Errorf("no usable price for %s", symbol)
	}
	return price, nil
}

// ValuePortfolio prices a set of holdings: total (USD, INR) plus the
// holdings with symbols normalized. Unknown coins are fetched on demand,
// so a validation error here means the coin really couldn't be priced.
func (c *CryptoPrices) ValuePortfolio(ctx context.Context, holdings []models.CryptoHolding) (usd, inr float64, normalized []models.CryptoHolding, err error) {
	normalized = make([]models.CryptoHolding, 0, len(holdings))
	for i, h := range holdings {
		symbol := NormalizeSymbol(h.Symbol)
		if symbol == "" {
			return 0, 0, nil, fmt.Errorf("coin %d: symbol required", i+1)
		}
		if h.Units <= 0 {
			return 0, 0, nil, fmt.Errorf("%s: units must be greater than zero", symbol)
		}
		price, err := c.PriceUSD(ctx, symbol)
		if err != nil {
			return 0, 0, nil, err
		}
		usd += h.Units * price
		h.Symbol = symbol
		normalized = append(normalized, h)
	}
	inr = usd * c.usdToINR(ctx)
	return usd, inr, normalized, nil
}

// RevalueHoldings recomputes the stored value of every crypto portfolio
// from the latest cached prices. Runs after each price refresh so
// portfolios track the market without user edits.
func (c *CryptoPrices) RevalueHoldings(ctx context.Context) error {
	assets, err := c.Store.ListCryptoAssets(ctx)
	if err != nil {
		return err
	}
	rate := c.usdToINR(ctx)
	revalued := 0
	for _, a := range assets {
		if len(a.CryptoHoldings) == 0 {
			continue
		}
		total, ok := c.portfolioUSDFromCache(ctx, a.CryptoHoldings)
		if !ok {
			// A coin has no cached price; keep the last full valuation
			// rather than storing a partial (understated) one.
			continue
		}
		if err := c.Store.SetAssetValuation(ctx, a.ID, total*rate, total); err != nil {
			if c.Log != nil {
				c.Log.Error("crypto revalue failed", "asset", a.ID, "err", err)
			}
			continue
		}
		revalued++
	}
	if c.Log != nil && revalued > 0 {
		c.Log.Info("crypto portfolios revalued", "count", revalued)
	}
	return nil
}

// portfolioUSDFromCache sums holdings using only cached prices; ok is false
// when any coin is missing a usable price.
func (c *CryptoPrices) portfolioUSDFromCache(ctx context.Context, holdings []models.CryptoHolding) (float64, bool) {
	var total float64
	for _, h := range holdings {
		if h.Units <= 0 {
			continue
		}
		doc, found, err := c.Store.GetCryptoPrice(ctx, NormalizeSymbol(h.Symbol))
		if err != nil || !found {
			return 0, false
		}
		price, ok := priceFromPayload(doc.Payload)
		if !ok || price <= 0 {
			return 0, false
		}
		total += h.Units * price
	}
	return total, true
}

// usdToINR reads the cached FX map ({"USD": ₹ per $, ...}); falls back to
// a sane constant so a dead FX API never zeroes portfolios.
func (c *CryptoPrices) usdToINR(ctx context.Context) float64 {
	if c.Rates != nil {
		if rates, err := c.Rates.Get(ctx); err == nil {
			if v := rates["USD"]; v > 0 {
				return v
			}
		}
	}
	return 83
}

// priceFromPayload digs the last-trade price out of the cached
// FreeCryptoAPI document: {"symbols": [{"last": "64480.01", ...}]}.
func priceFromPayload(p bson.M) (float64, bool) {
	if v, ok := toFloat(p["last"]); ok {
		return v, true
	}
	arr, ok := asSlice(p["symbols"])
	if !ok || len(arr) == 0 {
		return 0, false
	}
	first, ok := asMap(arr[0])
	if !ok {
		return 0, false
	}
	return toFloat(first["last"])
}

func asSlice(v interface{}) ([]interface{}, bool) {
	switch t := v.(type) {
	case primitive.A:
		return t, true
	case []interface{}:
		return t, true
	}
	return nil, false
}

func asMap(v interface{}) (map[string]interface{}, bool) {
	switch t := v.(type) {
	case primitive.M: // == bson.M
		return t, true
	case map[string]interface{}:
		return t, true
	}
	return nil, false
}

func toFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}
