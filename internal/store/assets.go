package store

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"wealthflow/backend/internal/models"
)

func (s *Store) ListAssetsByUser(ctx context.Context, userID string) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"user_id": userID}, options.Find().SetLimit(1000).SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

func (s *Store) FindAsset(ctx context.Context, assetID, userID string) (*models.Asset, error) {
	var a models.Asset
	err := s.db.Collection("assets").FindOne(ctx, bson.M{"id": assetID, "user_id": userID}, options.FindOne().SetProjection(bson.M{"_id": 0})).Decode(&a)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) InsertAsset(ctx context.Context, a *models.Asset) error {
	_, err := s.db.Collection("assets").InsertOne(ctx, a)
	return err
}

func (s *Store) UpdateAsset(ctx context.Context, assetID, userID string, update bson.M) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID, "user_id": userID}, bson.M{"$set": update})
	return err
}

func (s *Store) DeleteAsset(ctx context.Context, assetID, userID string) (bool, error) {
	res, err := s.db.Collection("assets").DeleteOne(ctx, bson.M{"id": assetID, "user_id": userID})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (s *Store) DistinctAssetUserIDs(ctx context.Context) ([]string, error) {
	vals, err := s.db.Collection("assets").Distinct(ctx, "user_id", bson.M{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// ListCryptoAssets returns every units-based crypto asset across all users,
// used by the hourly revaluation pass.
func (s *Store) ListCryptoAssets(ctx context.Context) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"asset_type": "crypto"}, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

// DistinctCryptoSymbols lists the coin symbols currently held by any user,
// so the price refresh covers everything in portfolios.
func (s *Store) DistinctCryptoSymbols(ctx context.Context) ([]string, error) {
	vals, err := s.db.Collection("assets").Distinct(ctx, "crypto_holdings.symbol", bson.M{"asset_type": "crypto"})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// SetAssetValuation updates just the computed value fields of an asset
// (used by revaluation; deliberately leaves updated_at alone so it still
// reflects the user's last edit).
func (s *Store) SetAssetValuation(ctx context.Context, assetID string, valueINR, valueUSD float64) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$set": bson.M{
		"current_value":   valueINR,
		"total_value_usd": valueUSD,
	}})
	return err
}

// ListMFAssets returns every mutual-fund portfolio across all users,
// used by the daily NAV revaluation pass.
func (s *Store) ListMFAssets(ctx context.Context) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"asset_type": "mutual_fund"}, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

// DistinctMFSchemeCodes lists the scheme codes currently held by any user,
// so the daily NAV refresh covers everything in portfolios.
func (s *Store) DistinctMFSchemeCodes(ctx context.Context) ([]string, error) {
	vals, err := s.db.Collection("assets").Distinct(ctx, "mf_holdings.scheme_code", bson.M{"asset_type": "mutual_fund"})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// SetAssetMFValuation updates an MF portfolio's computed value and its
// holdings (whose last_nav/nav_date the revaluer refreshes); updated_at
// is left alone so it still reflects the user's last edit.
func (s *Store) SetAssetMFValuation(ctx context.Context, assetID string, valueINR float64, holdings []models.MFHolding) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$set": bson.M{
		"current_value": valueINR,
		"mf_holdings":   holdings,
	}})
	return err
}

// ListForeignAssets returns plain foreign-currency assets across all users
// for FX revaluation. Units-based portfolios (crypto, mutual funds) are
// excluded — they are revalued by their own passes and may carry stale
// is_foreign leftovers from before they were converted to units tracking.
func (s *Store) ListForeignAssets(ctx context.Context) ([]models.Asset, error) {
	filter := bson.M{
		"is_foreign": true,
		"asset_type": bson.M{"$nin": bson.A{"crypto", "mutual_fund", "travel_points", "us_stocks", "gold", "govt_schemes", "bank_accounts", "loans", "bonds"}},
	}
	cur, err := s.db.Collection("assets").Find(ctx, filter, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

// ListTravelPointAssets returns every travel-points asset across all users.
func (s *Store) ListTravelPointAssets(ctx context.Context) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"asset_type": "travel_points"}, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

// SetAssetCurrentValue updates only an asset's INR value (FX revaluation
// of plain foreign assets); updated_at is left as the user's last edit.
func (s *Store) SetAssetCurrentValue(ctx context.Context, assetID string, valueINR float64) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$set": bson.M{
		"current_value": valueINR,
	}})
	return err
}

// SetAssetTravelValuation updates a travel-points asset's INR value fields
// (the USD total stays; only the conversion moves with the rate).
func (s *Store) SetAssetTravelValuation(ctx context.Context, assetID string, valueINR float64) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$set": bson.M{
		"current_value":   valueINR,
		"total_value_inr": valueINR,
	}})
	return err
}

// ClearForeignFields removes the manual foreign-currency fields from an
// asset. Called when an asset becomes a units-based portfolio: $set-based
// updates never remove fields, so without this the stale is_foreign /
// foreign_amount of a former manual entry would ride along forever.
func (s *Store) ClearForeignFields(ctx context.Context, assetID string) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$unset": bson.M{
		"is_foreign":       "",
		"foreign_currency": "",
		"foreign_amount":   "",
	}})
	return err
}

// ListStockAssets returns every US-stocks portfolio across all users,
// used by the quote revaluation pass.
func (s *Store) ListStockAssets(ctx context.Context) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"asset_type": "us_stocks"}, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

// DistinctStockSymbols lists the tickers currently held by any user, so
// the quote refresh covers everything in portfolios.
func (s *Store) DistinctStockSymbols(ctx context.Context) ([]string, error) {
	vals, err := s.db.Collection("assets").Distinct(ctx, "stock_holdings.symbol", bson.M{"asset_type": "us_stocks"})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// SetAssetStockValuation updates a US-stocks portfolio's computed values
// and its holdings (whose name/last_price the revaluer refreshes);
// updated_at is left alone so it still reflects the user's last edit.
func (s *Store) SetAssetStockValuation(ctx context.Context, assetID string, valueINR, valueUSD float64, holdings []models.StockHolding) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$set": bson.M{
		"current_value":   valueINR,
		"total_value_usd": valueUSD,
		"stock_holdings":  holdings,
	}})
	return err
}

// ListGoldAssets returns every gold portfolio across all users, used by
// the daily IBJA-rate revaluation pass.
func (s *Store) ListGoldAssets(ctx context.Context) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"asset_type": "gold"}, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}

// SetAssetGoldValuation updates a gold portfolio's computed value and its
// holdings (whose last_rate the revaluer refreshes); updated_at is left
// alone so it still reflects the user's last edit.
func (s *Store) SetAssetGoldValuation(ctx context.Context, assetID string, valueINR float64, holdings []models.GoldHolding) error {
	_, err := s.db.Collection("assets").UpdateOne(ctx, bson.M{"id": assetID}, bson.M{"$set": bson.M{
		"current_value": valueINR,
		"gold_holdings": holdings,
	}})
	return err
}

// ListBankAssets returns every bank-accounts portfolio across all users,
// used by the FX revaluation pass (non-INR balances move with rates).
func (s *Store) ListBankAssets(ctx context.Context) ([]models.Asset, error) {
	cur, err := s.db.Collection("assets").Find(ctx, bson.M{"asset_type": "bank_accounts"}, options.Find().SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Asset
	for cur.Next(ctx) {
		var a models.Asset
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, cur.Err()
}
