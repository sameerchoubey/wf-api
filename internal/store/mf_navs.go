package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MFNavDoc caches the latest NAV for one mutual-fund scheme (AMFI data
// via mfapi.in), mirroring how crypto_prices caches coin prices.
type MFNavDoc struct {
	SchemeCode string  `bson:"scheme_code" json:"scheme_code"`
	SchemeName string  `bson:"scheme_name" json:"scheme_name"`
	NAV        float64 `bson:"nav" json:"nav"`
	NAVDate    string  `bson:"nav_date" json:"nav_date"`
	UpdatedAt  string  `bson:"updated_at" json:"updated_at"`
}

// UpsertMFNav replaces the cached NAV for a single scheme.
func (s *Store) UpsertMFNav(ctx context.Context, doc MFNavDoc, now time.Time) error {
	doc.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	_, err := s.db.Collection("mf_navs").UpdateOne(
		ctx,
		bson.M{"scheme_code": doc.SchemeCode},
		bson.M{"$set": doc},
		options.Update().SetUpsert(true),
	)
	return err
}

// GetMFNav returns one scheme's cached NAV if present.
func (s *Store) GetMFNav(ctx context.Context, schemeCode string) (MFNavDoc, bool, error) {
	var d MFNavDoc
	err := s.db.Collection("mf_navs").FindOne(ctx, bson.M{"scheme_code": schemeCode}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return MFNavDoc{}, false, nil
	}
	if err != nil {
		return MFNavDoc{}, false, err
	}
	return d, true, nil
}
