package store

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"wealthflow/backend/internal/models"
)

func (s *Store) ListSnapshots(ctx context.Context, userID string, startDate, endDate *string) ([]models.Snapshot, error) {
	q := bson.M{"user_id": userID}
	if startDate != nil || endDate != nil {
		d := bson.M{}
		if startDate != nil {
			d["$gte"] = *startDate
		}
		if endDate != nil {
			d["$lte"] = *endDate
		}
		q["date"] = d
	}
	cur, err := s.db.Collection("snapshots").Find(ctx, q, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}).SetLimit(10000).SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Snapshot
	for cur.Next(ctx) {
		var sn models.Snapshot
		if err := cur.Decode(&sn); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, cur.Err()
}

func (s *Store) FindSnapshotByUserDate(ctx context.Context, userID, date string) (*models.Snapshot, error) {
	var sn models.Snapshot
	err := s.db.Collection("snapshots").FindOne(ctx, bson.M{"user_id": userID, "date": date}, options.FindOne().SetProjection(bson.M{"_id": 0})).Decode(&sn)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sn, nil
}

// SetSnapshot upserts the daily snapshot document (same behavior as Python update/insert).
func (s *Store) SetSnapshot(ctx context.Context, userID, date string, fields bson.M) error {
	filter := bson.M{"user_id": userID, "date": date}
	_, err := s.db.Collection("snapshots").UpdateOne(ctx, filter, bson.M{"$set": fields}, options.Update().SetUpsert(true))
	return err
}

// AllSnapshots returns every user's snapshots, oldest first.
func (s *Store) AllSnapshots(ctx context.Context) ([]models.Snapshot, error) {
	cur, err := s.db.Collection("snapshots").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}).SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Snapshot
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetSnapshotInvested sets only the invested summary on one snapshot.
func (s *Store) SetSnapshotInvested(ctx context.Context, userID, date string, invested models.AssetCostBasis) error {
	_, err := s.db.Collection("snapshots").UpdateOne(ctx, bson.M{"user_id": userID, "date": date}, bson.M{"$set": bson.M{"invested": invested}})
	return err
}
