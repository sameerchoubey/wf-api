package models

import (
	"time"
)

// User is stored in MongoDB (password never returned to clients).
type User struct {
	ID           string `bson:"id" json:"id"`
	Email        string `bson:"email" json:"email"`
	PasswordHash string `bson:"password_hash" json:"-"`
	IsVerified   bool   `bson:"is_verified" json:"is_verified"`
	CreatedAt    string `bson:"created_at" json:"created_at"`
}

type UserRegister struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserLogin struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type TokenResponse struct {
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	Message     *string `json:"message,omitempty"`
}

type Asset struct {
	ID                   string      `bson:"id" json:"id"`
	UserID               *string     `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Name                 string      `bson:"name" json:"name"`
	Category             string      `bson:"category" json:"category"`
	CurrentValue         float64     `bson:"current_value" json:"current_value"`
	IsForeign            *bool       `bson:"is_foreign,omitempty" json:"is_foreign,omitempty"`
	ForeignCurrency      *string     `bson:"foreign_currency,omitempty" json:"foreign_currency,omitempty"`
	ForeignAmount        *float64    `bson:"foreign_amount,omitempty" json:"foreign_amount,omitempty"`
	AssetType            *string     `bson:"asset_type,omitempty" json:"asset_type,omitempty"`
	TravelPointsPrograms interface{} `bson:"travel_points_programs,omitempty" json:"travel_points_programs,omitempty"`
	TotalValueUSD        *float64    `bson:"total_value_usd,omitempty" json:"total_value_usd,omitempty"`
	TotalValueINR        *float64    `bson:"total_value_inr,omitempty" json:"total_value_inr,omitempty"`
	UpdatedAt            string      `bson:"updated_at" json:"updated_at"`
}

type AssetCreate struct {
	Name                 string      `json:"name"`
	Category             string      `json:"category"`
	CurrentValue         float64     `json:"current_value"`
	IsForeign            *bool       `json:"is_foreign"`
	ForeignCurrency      *string     `json:"foreign_currency"`
	ForeignAmount        *float64    `json:"foreign_amount"`
	AssetType            *string     `json:"asset_type"`
	TravelPointsPrograms interface{} `json:"travel_points_programs"`
	TotalValueUSD        *float64    `json:"total_value_usd"`
	TotalValueINR        *float64    `json:"total_value_inr"`
}

type AssetUpdate struct {
	Name                 *string     `json:"name"`
	Category             *string     `json:"category"`
	CurrentValue         *float64    `json:"current_value"`
	IsForeign            *bool       `json:"is_foreign"`
	ForeignCurrency      *string     `json:"foreign_currency"`
	ForeignAmount        *float64    `json:"foreign_amount"`
	AssetType            *string     `json:"asset_type"`
	TravelPointsPrograms interface{} `json:"travel_points_programs"`
	TotalValueUSD        *float64    `json:"total_value_usd"`
	TotalValueINR        *float64    `json:"total_value_inr"`
}

type Liability struct {
	ID        string  `bson:"id" json:"id"`
	UserID    *string `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Name      string  `bson:"name" json:"name"`
	Category  string  `bson:"category" json:"category"`
	Amount    float64 `bson:"amount" json:"amount"`
	UpdatedAt string  `bson:"updated_at" json:"updated_at"`
}

type LiabilityCreate struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

type LiabilityUpdate struct {
	Name     *string  `json:"name"`
	Category *string  `json:"category"`
	Amount   *float64 `json:"amount"`
}

type Snapshot struct {
	ID               string        `bson:"id,omitempty" json:"id,omitempty"`
	UserID           string        `bson:"user_id" json:"user_id"`
	Date             string        `bson:"date" json:"date"`
	Timestamp        string        `bson:"timestamp" json:"timestamp"`
	TotalAssets      float64       `bson:"total_assets" json:"total_assets"`
	TotalAssetsUSD   float64       `bson:"total_assets_usd,omitempty" json:"total_assets_usd,omitempty"`
	TotalLiabilities float64       `bson:"total_liabilities" json:"total_liabilities"`
	NetWorth         float64       `bson:"net_worth" json:"net_worth"`
	Assets           []interface{} `bson:"assets" json:"assets"`
	Liabilities      []interface{} `bson:"liabilities" json:"liabilities"`
}

type DashboardData struct {
	Assets           []Asset     `json:"assets"`
	Liabilities      []Liability `json:"liabilities"`
	TotalAssets      float64     `json:"total_assets"`
	TotalAssetsUSD   float64     `json:"total_assets_usd"`
	TotalLiabilities float64     `json:"total_liabilities"`
	NetWorth         float64     `json:"net_worth"`
}

type ExchangeRatesResponse struct {
	Rates     map[string]float64 `json:"rates"`
	Base      string             `json:"base"`
	Timestamp time.Time          `json:"timestamp"`
}
