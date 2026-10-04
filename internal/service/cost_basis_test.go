package service

import (
	"context"
	"testing"

	"wealthflow/backend/internal/models"
)

func ptr[T any](v T) *T { return &v }

func TestCostBasisSkipsHoldingsWithoutCost(t *testing.T) {
	mf := models.Asset{
		AssetType:    ptr("mutual_fund"),
		CurrentValue: 3000,
		MFHoldings: []models.MFHolding{
			{Units: 10, AvgNAV: ptr(100.0), LastNAV: 150}, // 1000 → 1500
			{Units: 10, LastNAV: 150},                     // no cost: untracked
		},
	}
	property := models.Asset{AssetType: ptr("fixed"), CurrentValue: 50000}
	laptop := models.Asset{AssetType: ptr("personal"), CurrentValue: 900, PurchasePrice: ptr(1500.0)}
	phone := models.Asset{AssetType: ptr("personal"), CurrentValue: 300} // no price: = value
	points := models.Asset{AssetType: ptr("travel_points"), CurrentValue: 700}
	bank := models.Asset{AssetType: ptr("bank_accounts"), CurrentValue: 2000}
	stocks := models.Asset{
		AssetType:     ptr("us_stocks"),
		CurrentValue:  8400,
		TotalValueUSD: ptr(100.0), // implied ₹84/$
		StockHoldings: []models.StockHolding{{Units: 1, BuyPriceUSD: ptr(50.0), LastPrice: 100}},
	}

	got := (&CostBasis{}).Totals(context.Background(), []models.Asset{mf, property, bank, stocks, laptop, phone, points})
	want := models.AssetCostBasis{
		Invested:         1000 + 2000 + 4200 + 1500 + 300,
		Current:          1500 + 2000 + 8400 + 900 + 300,
		Untracked:        1500 + 50000 + 700,
		PersonalInvested: 1500 + 300,
		PersonalCurrent:  900 + 300,
		Cash:             2000,
		Rewards:          700,
		NoCost:           1500 + 50000,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEstimateFlatHistory(t *testing.T) {
	today := []models.Asset{{
		AssetType:    ptr("mutual_fund"),
		CurrentValue: 1100,
		MFHoldings:   []models.MFHolding{{Units: 10, AvgNAV: ptr(100.0), LastNAV: 110}},
	}}
	cb := &CostBasis{}
	cb.Totals(context.Background(), today)
	ratios := Ratios(today)

	old := []models.Asset{
		{Category: "Mutual Funds", AssetType: ptr("liquid"), CurrentValue: 550}, // flat MF
		{Category: "Savings Account", AssetType: ptr("liquid"), CurrentValue: 200},
		{Category: "Crypto", CurrentValue: 300}, // no crypto ratio known
	}
	cb.Totals(context.Background(), old)
	for i := range old {
		Estimate(&old[i], ratios)
	}
	got := Sum(old)
	if got.Invested != 500+200 || got.Current != 550+200 || got.NoCost != 300 || !got.Estimated {
		t.Fatalf("got %+v", got)
	}
}
