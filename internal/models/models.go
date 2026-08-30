package models

import (
	"time"
)

// User is stored in MongoDB (password never returned to clients).
type User struct {
	ID           string `bson:"id" json:"id"`
	Email        string `bson:"email" json:"email"`
	PasswordHash string `bson:"password_hash" json:"-"`
	FirstName    string `bson:"first_name,omitempty" json:"first_name,omitempty"`
	LastName     string `bson:"last_name,omitempty" json:"last_name,omitempty"`
	IsVerified   bool   `bson:"is_verified" json:"is_verified"`
	CreatedAt    string `bson:"created_at" json:"created_at"`
}

// ProfileUpdate is the PUT /me payload.
type ProfileUpdate struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// PasswordChange is the POST /me/password payload.
type PasswordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type UserRegister struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserLogin struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type EmailLookup struct {
	Email string `json:"email"`
}

type EmailLookupResponse struct {
	Exists bool `json:"exists"`
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
	// Crypto portfolio (asset_type == "crypto"): one asset holds many
	// coins; the server prices Σ units × cached market price and
	// clients never send a value.
	CryptoHoldings []CryptoHolding `bson:"crypto_holdings,omitempty" json:"crypto_holdings,omitempty"`
	// Mutual-fund portfolio (asset_type == "mutual_fund"): one asset
	// holds many schemes; the server prices Σ units × latest NAV.
	MFHoldings []MFHolding `bson:"mf_holdings,omitempty" json:"mf_holdings,omitempty"`
	// US/LSE stock portfolio (asset_type == "us_stocks"): USD-quoted
	// tickers plus an uninvested USD cash component; InvestedINR is the
	// total INR actually spent converting to USD, enabling a true
	// INR-basis gain (market + FX together).
	StockHoldings []StockHolding `bson:"stock_holdings,omitempty" json:"stock_holdings,omitempty"`
	CashUSD       *float64       `bson:"cash_usd,omitempty" json:"cash_usd,omitempty"`
	InvestedINR   *float64       `bson:"invested_inr,omitempty" json:"invested_inr,omitempty"`
	// Gold portfolio (asset_type == "gold"): grams per karat, priced from
	// the daily IBJA domestic rates.
	GoldHoldings []GoldHolding `bson:"gold_holdings,omitempty" json:"gold_holdings,omitempty"`
	// Government schemes (asset_type == "govt_schemes"): NPS/PPF/EPF/SSY
	// balances entered manually (no market feed); invested vs current
	// powers the gain display.
	GovtHoldings []GovtHolding `bson:"govt_holdings,omitempty" json:"govt_holdings,omitempty"`
	// Bank accounts (asset_type == "bank_accounts"): balances per account
	// in their native currency; non-INR balances are re-converted by the
	// FX revaluation pass.
	BankHoldings []BankHolding `bson:"bank_holdings,omitempty" json:"bank_holdings,omitempty"`
	// Loans given out (asset_type == "loans"): money owed TO the user.
	LoanHoldings []LoanHolding `bson:"loan_holdings,omitempty" json:"loan_holdings,omitempty"`
	UpdatedAt    string        `bson:"updated_at" json:"updated_at"`
}

// BankHolding is one account inside a bank-accounts portfolio.
type BankHolding struct {
	Label    string  `bson:"label" json:"label"`
	Currency string  `bson:"currency" json:"currency"` // INR, USD or EUR
	Balance  float64 `bson:"balance" json:"balance"`
}

// LoanHolding is one loan inside a loans-given portfolio.
type LoanHolding struct {
	Label     string  `bson:"label" json:"label"`
	AmountINR float64 `bson:"amount_inr" json:"amount_inr"`
}

// GovtHolding is one scheme inside a government-schemes portfolio.
type GovtHolding struct {
	Scheme      string   `bson:"scheme" json:"scheme"` // NPS, PPF, EPF, SSY or OTHER
	Label       string   `bson:"label,omitempty" json:"label,omitempty"`
	InvestedINR *float64 `bson:"invested_inr,omitempty" json:"invested_inr,omitempty"`
	CurrentINR  float64  `bson:"current_inr" json:"current_inr"`
}

// GoldHolding is one item inside a gold portfolio. LastRate (₹/gram) is
// server-maintained from the IBJA feed; BuyPerGram is the user's average
// cost (enables the gain display).
type GoldHolding struct {
	Label      string   `bson:"label,omitempty" json:"label,omitempty"`
	Karat      string   `bson:"karat" json:"karat"` // "24", "22" or "18"
	Grams      float64  `bson:"grams" json:"grams"`
	BuyPerGram *float64 `bson:"buy_per_gram,omitempty" json:"buy_per_gram,omitempty"`
	LastRate   float64  `bson:"last_rate,omitempty" json:"last_rate,omitempty"`
}

// StockHolding is one ticker inside a US-stocks portfolio. Name and
// LastPrice are server-maintained from the quote feed; BuyPriceUSD is the
// user's average cost (enables the USD-basis gain display).
type StockHolding struct {
	Symbol      string   `bson:"symbol" json:"symbol"`
	Name        string   `bson:"name,omitempty" json:"name,omitempty"`
	Units       float64  `bson:"units" json:"units"`
	BuyPriceUSD *float64 `bson:"buy_price_usd,omitempty" json:"buy_price_usd,omitempty"`
	LastPrice   float64  `bson:"last_price,omitempty" json:"last_price,omitempty"`
}

// StockSearchResult is one row returned by the ticker-search proxy.
type StockSearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
}

// CryptoHolding is one coin inside a crypto portfolio asset.
type CryptoHolding struct {
	Symbol      string   `bson:"symbol" json:"symbol"`
	Units       float64  `bson:"units" json:"units"`
	BuyPriceUSD *float64 `bson:"buy_price_usd,omitempty" json:"buy_price_usd,omitempty"`
}

// MFHolding is one scheme inside a mutual-fund portfolio asset.
// SchemeName, LastNAV and NAVDate are server-maintained from the NAV
// feed; AvgNAV is the user's average buy NAV (enables gain display).
type MFHolding struct {
	SchemeCode string   `bson:"scheme_code" json:"scheme_code"`
	SchemeName string   `bson:"scheme_name" json:"scheme_name"`
	Units      float64  `bson:"units" json:"units"`
	AvgNAV     *float64 `bson:"avg_nav,omitempty" json:"avg_nav,omitempty"`
	LastNAV    float64  `bson:"last_nav,omitempty" json:"last_nav,omitempty"`
	NAVDate    string   `bson:"nav_date,omitempty" json:"nav_date,omitempty"`
}

// MFSearchResult is one row returned by the fund-search proxy.
type MFSearchResult struct {
	SchemeCode int    `json:"schemeCode"`
	SchemeName string `json:"schemeName"`
}

type AssetCreate struct {
	Name                 string          `json:"name"`
	Category             string          `json:"category"`
	CurrentValue         float64         `json:"current_value"`
	IsForeign            *bool           `json:"is_foreign"`
	ForeignCurrency      *string         `json:"foreign_currency"`
	ForeignAmount        *float64        `json:"foreign_amount"`
	AssetType            *string         `json:"asset_type"`
	TravelPointsPrograms interface{}     `json:"travel_points_programs"`
	TotalValueUSD        *float64        `json:"total_value_usd"`
	TotalValueINR        *float64        `json:"total_value_inr"`
	CryptoHoldings       []CryptoHolding `json:"crypto_holdings"`
	MFHoldings           []MFHolding     `json:"mf_holdings"`
	StockHoldings        []StockHolding  `json:"stock_holdings"`
	CashUSD              *float64        `json:"cash_usd"`
	InvestedINR          *float64        `json:"invested_inr"`
	GoldHoldings         []GoldHolding   `json:"gold_holdings"`
	GovtHoldings         []GovtHolding   `json:"govt_holdings"`
	BankHoldings         []BankHolding   `json:"bank_holdings"`
	LoanHoldings         []LoanHolding   `json:"loan_holdings"`
}

type AssetUpdate struct {
	Name                 *string         `json:"name"`
	Category             *string         `json:"category"`
	CurrentValue         *float64        `json:"current_value"`
	IsForeign            *bool           `json:"is_foreign"`
	ForeignCurrency      *string         `json:"foreign_currency"`
	ForeignAmount        *float64        `json:"foreign_amount"`
	AssetType            *string         `json:"asset_type"`
	TravelPointsPrograms interface{}     `json:"travel_points_programs"`
	TotalValueUSD        *float64        `json:"total_value_usd"`
	TotalValueINR        *float64        `json:"total_value_inr"`
	CryptoHoldings       []CryptoHolding `json:"crypto_holdings"`
	MFHoldings           []MFHolding     `json:"mf_holdings"`
	StockHoldings        []StockHolding  `json:"stock_holdings"`
	CashUSD              *float64        `json:"cash_usd"`
	InvestedINR          *float64        `json:"invested_inr"`
	GoldHoldings         []GoldHolding   `json:"gold_holdings"`
	GovtHoldings         []GovtHolding   `json:"govt_holdings"`
	BankHoldings         []BankHolding   `json:"bank_holdings"`
	LoanHoldings         []LoanHolding   `json:"loan_holdings"`
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
