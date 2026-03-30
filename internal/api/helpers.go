package api

import (
	"go.mongodb.org/mongo-driver/bson"

	"github.com/google/uuid"

	"wealthflow/backend/internal/models"
)

func newID() string {
	return uuid.New().String()
}

func stringPtr(s string) *string {
	return &s
}

func assetUpdateBSON(in models.AssetUpdate) bson.M {
	m := bson.M{}
	if in.Name != nil {
		m["name"] = *in.Name
	}
	if in.Category != nil {
		m["category"] = *in.Category
	}
	if in.CurrentValue != nil {
		m["current_value"] = *in.CurrentValue
	}
	if in.IsForeign != nil {
		m["is_foreign"] = *in.IsForeign
	}
	if in.ForeignCurrency != nil {
		m["foreign_currency"] = *in.ForeignCurrency
	}
	if in.ForeignAmount != nil {
		m["foreign_amount"] = *in.ForeignAmount
	}
	if in.AssetType != nil {
		m["asset_type"] = *in.AssetType
	}
	if in.TravelPointsPrograms != nil {
		m["travel_points_programs"] = in.TravelPointsPrograms
	}
	if in.TotalValueUSD != nil {
		m["total_value_usd"] = *in.TotalValueUSD
	}
	if in.TotalValueINR != nil {
		m["total_value_inr"] = *in.TotalValueINR
	}
	return m
}

func liabilityUpdateBSON(in models.LiabilityUpdate) bson.M {
	m := bson.M{}
	if in.Name != nil {
		m["name"] = *in.Name
	}
	if in.Category != nil {
		m["category"] = *in.Category
	}
	if in.Amount != nil {
		m["amount"] = *in.Amount
	}
	return m
}
