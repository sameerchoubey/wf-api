package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"wealthflow/backend/internal/store"
)

// FreeCryptoAPI uses query parameter "token" for the API key (not "api_key").
const freeCryptoGetData = "https://api.freecryptoapi.com/v1/getData"

// DefaultCryptoSymbols are fetched and upserted into crypto_prices.
var DefaultCryptoSymbols = []string{"BTC", "ETH"}

type CryptoPrices struct {
	APIKey string
	Store  *store.Store
	Client *http.Client
	Log    *slog.Logger
}

// Refresh fetches live data for each default symbol and upserts MongoDB.
func (c *CryptoPrices) Refresh(ctx context.Context) error {
	if c.APIKey == "" {
		if c.Log != nil {
			c.Log.Warn("CRYPTO_PRICE_API_KEY not set; skipping crypto price refresh")
		}
		return nil
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	for _, sym := range DefaultCryptoSymbols {
		payload, err := c.fetchSymbol(ctx, client, sym)
		if err != nil {
			if c.Log != nil {
				c.Log.Error("crypto API request failed", "symbol", sym, "err", err)
			}
			continue
		}
		now := time.Now().UTC()
		if err := c.Store.UpsertCryptoPrice(ctx, sym, payload, now); err != nil {
			if c.Log != nil {
				c.Log.Error("crypto price upsert failed", "symbol", sym, "err", err)
			}
			continue
		}
		if c.Log != nil {
			c.Log.Info("crypto price saved", "symbol", sym)
		}
	}
	return nil
}

func (c *CryptoPrices) fetchSymbol(ctx context.Context, client *http.Client, symbol string) (bson.M, error) {
	u, err := url.Parse(freeCryptoGetData)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("token", c.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("freecryptoapi: %s: %s", resp.Status, truncateForLog(body))
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var raw map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	if st, ok := raw["status"].(bool); ok && !st {
		msg := ""
		if e, ok := raw["error"].(string); ok {
			msg = e
		}
		if msg == "" {
			msg = "API returned status false"
		}
		return nil, fmt.Errorf("freecryptoapi: %s", msg)
	}

	payload := bson.M{}
	if data, ok := raw["data"].(map[string]interface{}); ok && len(data) > 0 {
		for k, v := range data {
			payload[k] = v
		}
	} else {
		for k, v := range raw {
			if k == "status" {
				continue
			}
			payload[k] = v
		}
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("freecryptoapi: empty payload in response")
	}
	return payload, nil
}

func truncateForLog(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
