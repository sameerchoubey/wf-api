package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GoldRatesDoc caches the latest IBJA domestic gold rates in ₹/gram,
// one document (id "latest"), mirroring the other price caches.
type GoldRatesDoc struct {
	Rate24K   float64 `bson:"rate_24k" json:"rate_24k"` // 999 purity
	Rate22K   float64 `bson:"rate_22k" json:"rate_22k"` // 916 purity
	Rate18K   float64 `bson:"rate_18k" json:"rate_18k"` // 750 purity
	UpdatedAt string  `bson:"updated_at" json:"updated_at"`
}

// UpsertGoldRates replaces the cached rates.
func (s *Store) UpsertGoldRates(ctx context.Context, doc GoldRatesDoc, now time.Time) error {
	doc.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	_, err := s.db.Collection("gold_rates").UpdateOne(
		ctx,
		bson.M{"_id": "latest"},
		bson.M{"$set": doc},
		options.Update().SetUpsert(true),
	)
	return err
}

// GetGoldRates returns the cached rates if present.
func (s *Store) GetGoldRates(ctx context.Context) (GoldRatesDoc, bool, error) {
	var d GoldRatesDoc
	err := s.db.Collection("gold_rates").FindOne(ctx, bson.M{"_id": "latest"}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return GoldRatesDoc{}, false, nil
	}
	if err != nil {
		return GoldRatesDoc{}, false, err
	}
	return d, true, nil
}
