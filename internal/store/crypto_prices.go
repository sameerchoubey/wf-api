package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CryptoPriceDoc is one row of cached FreeCryptoAPI data per symbol.
type CryptoPriceDoc struct {
	Symbol    string `bson:"symbol" json:"symbol"`
	Payload   bson.M `bson:"payload" json:"payload"`
	UpdatedAt string `bson:"updated_at" json:"updated_at"`
}

// UpsertCryptoPrice replaces the stored snapshot for a single symbol.
func (s *Store) UpsertCryptoPrice(ctx context.Context, symbol string, payload bson.M, now time.Time) error {
	iso := now.UTC().Format(time.RFC3339Nano)
	doc := bson.M{
		"symbol":     symbol,
		"payload":    payload,
		"updated_at": iso,
	}
	_, err := s.db.Collection("crypto_prices").UpdateOne(ctx, bson.M{"symbol": symbol}, bson.M{"$set": doc}, options.Update().SetUpsert(true))
	return err
}

// ListCryptoPrices returns all cached crypto rows (e.g. BTC, ETH), sorted by symbol.
func (s *Store) ListCryptoPrices(ctx context.Context) ([]CryptoPriceDoc, error) {
	cur, err := s.db.Collection("crypto_prices").Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"symbol": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []CryptoPriceDoc
	for cur.Next(ctx) {
		var d CryptoPriceDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, cur.Err()
}

// GetCryptoPrice returns one symbol's row if present.
func (s *Store) GetCryptoPrice(ctx context.Context, symbol string) (CryptoPriceDoc, bool, error) {
	var d CryptoPriceDoc
	err := s.db.Collection("crypto_prices").FindOne(ctx, bson.M{"symbol": symbol}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return CryptoPriceDoc{}, false, nil
	}
	if err != nil {
		return CryptoPriceDoc{}, false, err
	}
	return d, true, nil
}
