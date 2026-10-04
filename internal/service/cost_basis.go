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
// gain always compares like with like. Travel points are never invested
// (they cost nothing); personal items count at their purchase price.
type CostBasis struct {
	Store *store.Store
	// CryptoPriceUSD overrides the live price cache, e.g. with historical
	// prices when backfilling old snapshots.
	CryptoPriceUSD func(ctx context.Context, symbol string) (float64, bool)
}

// Asset kinds. Market kinds can gain or lose value; cash is held at face
// value (what's in it is what was put in); personal items count at their
// purchase price; rewards (travel points) are never invested.
const (
	KindCash     = "cash"
	KindPersonal = "personal"
	KindRewards  = "rewards"
	KindOther    = "other"
)

// kindByType covers assets the app tracks as structured holdings.
var kindByType = map[string]string{
	"mutual_fund": "mutual_funds", "crypto": "crypto", "us_stocks": "stocks",
	"gold": "gold", "govt_schemes": "govt_schemes", "bonds": "bonds",
	"bank_accounts": KindCash, "loans": KindCash,
	"personal": KindPersonal, "travel_points": KindRewards,
}

// kindByCategory classifies flat-value assets ("liquid", "fixed" or no
// type), including older ones saved before structured holdings existed.
var kindByCategory = map[string]string{
	"Mutual Funds": "mutual_funds", "Crypto": "crypto",
	"Stocks": "stocks", "US Stocks": "stocks", "Gold": "gold",
	"PPF": "govt_schemes", "NPS": "govt_schemes", "EPF": "govt_schemes",
	"SSY": "govt_schemes", "Govt Schemes": "govt_schemes", "Bonds": "bonds",
	"Savings Account": KindCash, "Cash": KindCash, "Fixed Deposit": KindCash,
	"Recurring Deposit": KindCash, "Loan": KindCash, "Loans": KindCash,
	"Electronics": KindPersonal, "Jewellery": KindPersonal, "Jewelry": KindPersonal,
	"Vehicles": KindPersonal, "Travel Points": KindRewards,
}

// structuredTypes hold per-holding units and costs.
var structuredTypes = map[string]bool{
	"mutual_fund": true, "crypto": true, "us_stocks": true,
	"gold": true, "govt_schemes": true, "bonds": true,
}

// Kind classifies an asset; market kinds are named after their category.
func Kind(a models.Asset) string {
	if a.AssetType != nil {
		if k, ok := kindByType[*a.AssetType]; ok {
			return k
		}
	}
	if k, ok := kindByCategory[a.Category]; ok {
		return k
	}
	return KindOther
}

// IsFlatMarket reports a market asset stored as a single value with no
// holdings, whose cost can only be estimated.
func IsFlatMarket(a models.Asset) bool {
	switch Kind(a) {
	case KindCash, KindPersonal, KindRewards, KindOther:
		return false
	}
	return a.AssetType == nil || !structuredTypes[*a.AssetType]
}

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
	kind := Kind(*a)
	if !structuredTypes[assetType] {
		assetType = "" // flat value: classified by kind below
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
	case kind == KindCash:
		add(a.CurrentValue, a.CurrentValue)
		cb.Cash = a.CurrentValue
	case kind == KindPersonal:
		buy := a.CurrentValue
		if a.PurchasePrice != nil && *a.PurchasePrice > 0 {
			buy = *a.PurchasePrice
		}
		add(buy, a.CurrentValue)
		cb.PersonalInvested, cb.PersonalCurrent = buy, a.CurrentValue
	case kind == KindRewards:
		cb.Rewards = a.CurrentValue
	}

	if kind != KindRewards {
		cb.NoCost = a.CurrentValue - cb.Current
		if cb.NoCost < 0 {
			cb.NoCost = 0
		}
	}
	cb.Untracked = cb.Rewards + cb.NoCost
	a.CostBasis = &cb
}

// Estimate prices a flat market asset from a cost ratio (invested ÷
// current for the same kind, taken from holdings whose cost is known).
// Used only for history from before structured holdings existed.
func Estimate(a *models.Asset, ratios map[string]float64) {
	ratio, ok := ratios[Kind(*a)]
	if !ok || !IsFlatMarket(*a) || a.CostBasis == nil {
		return
	}
	cb := a.CostBasis
	cb.Invested += a.CurrentValue * ratio
	cb.Current += a.CurrentValue
	cb.NoCost = 0
	cb.Untracked = cb.Rewards
	cb.Estimated = true
}

// Ratios returns invested ÷ current per market kind from assets whose
// CostBasis is already filled.
func Ratios(assets []models.Asset) map[string]float64 {
	inv, cur := map[string]float64{}, map[string]float64{}
	for _, a := range assets {
		if a.CostBasis == nil || IsFlatMarket(a) {
			continue
		}
		k := Kind(a)
		if k == KindCash || k == KindPersonal || k == KindRewards || k == KindOther {
			continue
		}
		inv[k] += a.CostBasis.Invested
		cur[k] += a.CostBasis.Current
	}
	out := map[string]float64{}
	for k, c := range cur {
		if c > 0 && inv[k] > 0 {
			out[k] = inv[k] / c
		}
	}
	return out
}

// Sum adds up already-filled CostBasis values.
func Sum(assets []models.Asset) models.AssetCostBasis {
	var t models.AssetCostBasis
	for _, a := range assets {
		if a.CostBasis == nil {
			continue
		}
		cb := a.CostBasis
		t.Invested += cb.Invested
		t.Current += cb.Current
		t.Untracked += cb.Untracked
		t.PersonalInvested += cb.PersonalInvested
		t.PersonalCurrent += cb.PersonalCurrent
		t.Cash += cb.Cash
		t.Rewards += cb.Rewards
		t.NoCost += cb.NoCost
		t.Estimated = t.Estimated || cb.Estimated
	}
	return t
}

// Totals fills every asset's CostBasis and returns the sums.
func (c *CostBasis) Totals(ctx context.Context, assets []models.Asset) models.AssetCostBasis {
	for i := range assets {
		c.ForAsset(ctx, &assets[i])
	}
	return Sum(assets)
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
