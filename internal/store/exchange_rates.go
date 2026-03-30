package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type exchangeRatesDoc struct {
	Rates     map[string]float64 `bson:"rates"`
	Timestamp string             `bson:"timestamp"`
}

// GetExchangeRatesCache returns cached rates and timestamp if a document exists.
func (s *Store) GetExchangeRatesCache(ctx context.Context) (rates map[string]float64, ts time.Time, ok bool, err error) {
	var doc exchangeRatesDoc
	err = s.db.Collection("exchange_rates").FindOne(ctx, bson.M{}, options.FindOne().SetProjection(bson.M{"_id": 0})).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	if doc.Rates == nil {
		return nil, time.Time{}, false, nil
	}
	if doc.Timestamp == "" {
		return doc.Rates, time.Time{}, true, nil
	}
	t, e := time.Parse(time.RFC3339Nano, doc.Timestamp)
	if e != nil {
		t, e = time.Parse(time.RFC3339, doc.Timestamp)
	}
	if e != nil {
		return doc.Rates, time.Time{}, true, nil
	}
	return doc.Rates, t.UTC(), true, nil
}

func (s *Store) UpsertExchangeRatesCache(ctx context.Context, rates map[string]float64, now time.Time) error {
	iso := now.UTC().Format(time.RFC3339Nano)
	doc := bson.M{
		"rates":      rates,
		"timestamp":  iso,
		"updated_at": iso,
	}
	_, err := s.db.Collection("exchange_rates").UpdateOne(ctx, bson.M{}, bson.M{"$set": doc}, options.Update().SetUpsert(true))
	return err
}
