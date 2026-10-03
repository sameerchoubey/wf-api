package service

import (
	"context"

	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/store"
)

// CostBasis works out how much money went into each asset, so the
// dashboard and snapshots can show "invested X → now Y" next to net worth.
//
// Only holdings with a known cost count: a fund with no average NAV or a
// coin with no buy price is left out of both Invested and Current, so the
// gain always compares like with like. Untracked is the current value of
// everything left out (property, electronics, holdings missing a cost).
type CostBasis struct {
	Store *store.Store
	// CryptoPriceUSD overrides the live price cache, e.g. with historical
	// prices when backfilling old snapshots.
	CryptoPriceUSD func(ctx context.Context, symbol string) (float64, bool)
}

// cashLikeTypes hold money at face value: what's in them is what was put
// in, with no market gain to separate out.
var cashLikeTypes = map[string]bool{"bank_accounts": true, "loans": true}

// ForAsset fills a.CostBasis from the stored holdings and cached prices.
func (c *CostBasis) ForAsset(ctx context.Context, a *models.Asset) {
	cb := models.AssetCostBasis{}
	add := func(invested, current float64) {
		cb.Invested += invested
		cb.Current += current
	}
	// INR per USD as implied by the stored valuation, so USD holdings are
	// converted at the same rate the asset's value was.
	fx := 0.0
	if a.TotalValueUSD != nil && *a.TotalValueUSD > 0 {
		fx = a.CurrentValue / *a.TotalValueUSD
	}

	assetType := ""
	if a.AssetType != nil {
		assetType = *a.AssetType
	}
	switch {
	case assetType == "mutual_fund":
		for _, h := range a.MFHoldings {
			if h.AvgNAV != nil && *h.AvgNAV > 0 && h.LastNAV > 0 {
				add(h.Units**h.AvgNAV, h.Units*h.LastNAV)
			}
		}
	case assetType == "gold":
		for _, h := range a.GoldHoldings {
			if h.BuyPerGram != nil && *h.BuyPerGram > 0 && h.LastRate > 0 {
				add(h.Grams**h.BuyPerGram, h.Grams*h.LastRate)
			}
		}
	case assetType == "us_stocks":
		if a.InvestedINR != nil && *a.InvestedINR > 0 {
			// True INR basis covers the whole portfolio, cash included.
			add(*a.InvestedINR, a.CurrentValue)
			break
		}
		if fx > 0 {
			for _, h := range a.StockHoldings {
				if h.BuyPriceUSD != nil && *h.BuyPriceUSD > 0 && h.LastPrice > 0 {
					add(h.Units**h.BuyPriceUSD*fx, h.Units*h.LastPrice*fx)
				}
			}
		}
	case assetType == "crypto":
		if fx > 0 {
			for _, h := range a.CryptoHoldings {
				if h.BuyPriceUSD == nil || *h.BuyPriceUSD <= 0 {
					continue
				}
				price, ok := c.cachedCryptoUSD(ctx, h.Symbol)
				if ok {
					add(h.Units**h.BuyPriceUSD*fx, h.Units*price*fx)
				}
			}
		}
	case assetType == "govt_schemes":
		for _, h := range a.GovtHoldings {
			if h.InvestedINR != nil && *h.InvestedINR > 0 {
				add(*h.InvestedINR, h.CurrentINR)
			}
		}
	case assetType == "bonds":
		for _, h := range a.BondHoldings {
			if h.InvestedINR != nil && *h.InvestedINR > 0 {
				add(*h.InvestedINR, h.CurrentINR)
			}
		}
	case cashLikeTypes[assetType]:
		add(a.CurrentValue, a.CurrentValue)
	}

	cb.Untracked = a.CurrentValue - cb.Current
	if cb.Untracked < 0 {
		cb.Untracked = 0
	}
	a.CostBasis = &cb
}

// Totals fills every asset's CostBasis and returns the sums.
func (c *CostBasis) Totals(ctx context.Context, assets []models.Asset) models.AssetCostBasis {
	var t models.AssetCostBasis
	for i := range assets {
		c.ForAsset(ctx, &assets[i])
		t.Invested += assets[i].CostBasis.Invested
		t.Current += assets[i].CostBasis.Current
		t.Untracked += assets[i].CostBasis.Untracked
	}
	return t
}

func (c *CostBasis) cachedCryptoUSD(ctx context.Context, symbol string) (float64, bool) {
	if c.CryptoPriceUSD != nil {
		return c.CryptoPriceUSD(ctx, NormalizeSymbol(symbol))
	}
	doc, found, err := c.Store.GetCryptoPrice(ctx, NormalizeSymbol(symbol))
	if err != nil || !found {
		return 0, false
	}
	price, ok := priceFromPayload(doc.Payload)
	return price, ok && price > 0
}
