package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// coinGeckoIDs maps the coins the app supports to CoinGecko's coin IDs.
var coinGeckoIDs = map[string]string{"BTC": "bitcoin", "ETH": "ethereum"}

// CoinGeckoDailyUSD returns daily USD closing prices keyed by date
// (YYYY-MM-DD, UTC) for the last `days` days. The keyless public API
// serves at most 365 days of history.
func CoinGeckoDailyUSD(ctx context.Context, symbol string, days int) (map[string]float64, error) {
	id, ok := coinGeckoIDs[NormalizeSymbol(symbol)]
	if !ok {
		return nil, fmt.Errorf("no CoinGecko id for %s", symbol)
	}
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s/market_chart?vs_currency=usd&days=%d&interval=daily", id, days)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko %s: HTTP %d", id, resp.StatusCode)
	}
	var body struct {
		Prices [][2]float64 `json:"prices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(body.Prices))
	for _, p := range body.Prices {
		date := time.UnixMilli(int64(p[0])).UTC().Format("2006-01-02")
		out[date] = p[1]
	}
	return out, nil
}
