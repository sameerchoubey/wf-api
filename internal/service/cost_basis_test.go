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
	property := models.Asset{CurrentValue: 50000}
	bank := models.Asset{AssetType: ptr("bank_accounts"), CurrentValue: 2000}
	stocks := models.Asset{
		AssetType:     ptr("us_stocks"),
		CurrentValue:  8400,
		TotalValueUSD: ptr(100.0), // implied ₹84/$
		StockHoldings: []models.StockHolding{{Units: 1, BuyPriceUSD: ptr(50.0), LastPrice: 100}},
	}

	got := (&CostBasis{}).Totals(context.Background(), []models.Asset{mf, property, bank, stocks})
	want := models.AssetCostBasis{
		Invested:  1000 + 2000 + 4200,
		Current:   1500 + 2000 + 8400,
		Untracked: 1500 + 50000,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
