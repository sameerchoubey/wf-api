package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// StockPriceDoc caches the latest quote for one ticker (Yahoo Finance
// data), mirroring crypto_prices / mf_navs. Only USD-quoted instruments
// are cached — the valuation math assumes USD.
type StockPriceDoc struct {
	Symbol    string  `bson:"symbol" json:"symbol"`
	Name      string  `bson:"name" json:"name"`
	PriceUSD  float64 `bson:"price_usd" json:"price_usd"`
	UpdatedAt string  `bson:"updated_at" json:"updated_at"`
}

// UpsertStockPrice replaces the cached quote for a single ticker.
func (s *Store) UpsertStockPrice(ctx context.Context, doc StockPriceDoc, now time.Time) error {
	doc.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	_, err := s.db.Collection("stock_prices").UpdateOne(
		ctx,
		bson.M{"symbol": doc.Symbol},
		bson.M{"$set": doc},
		options.Update().SetUpsert(true),
	)
	return err
}

// GetStockPrice returns one ticker's cached quote if present.
func (s *Store) GetStockPrice(ctx context.Context, symbol string) (StockPriceDoc, bool, error) {
	var d StockPriceDoc
	err := s.db.Collection("stock_prices").FindOne(ctx, bson.M{"symbol": symbol}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return StockPriceDoc{}, false, nil
	}
	if err != nil {
		return StockPriceDoc{}, false, err
	}
	return d, true, nil
}
