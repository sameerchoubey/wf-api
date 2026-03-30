package store

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"wealthflow/backend/internal/models"
)

func (s *Store) ListLiabilitiesByUser(ctx context.Context, userID string) ([]models.Liability, error) {
	cur, err := s.db.Collection("liabilities").Find(ctx, bson.M{"user_id": userID}, options.Find().SetLimit(1000).SetProjection(bson.M{"_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Liability
	for cur.Next(ctx) {
		var l models.Liability
		if err := cur.Decode(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, cur.Err()
}

func (s *Store) FindLiability(ctx context.Context, liabilityID, userID string) (*models.Liability, error) {
	var l models.Liability
	err := s.db.Collection("liabilities").FindOne(ctx, bson.M{"id": liabilityID, "user_id": userID}, options.FindOne().SetProjection(bson.M{"_id": 0})).Decode(&l)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) InsertLiability(ctx context.Context, l *models.Liability) error {
	_, err := s.db.Collection("liabilities").InsertOne(ctx, l)
	return err
}

func (s *Store) UpdateLiability(ctx context.Context, liabilityID, userID string, update bson.M) error {
	_, err := s.db.Collection("liabilities").UpdateOne(ctx, bson.M{"id": liabilityID, "user_id": userID}, bson.M{"$set": update})
	return err
}

func (s *Store) DeleteLiability(ctx context.Context, liabilityID, userID string) (bool, error) {
	res, err := s.db.Collection("liabilities").DeleteOne(ctx, bson.M{"id": liabilityID, "user_id": userID})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (s *Store) DistinctLiabilityUserIDs(ctx context.Context) ([]string, error) {
	vals, err := s.db.Collection("liabilities").Distinct(ctx, "user_id", bson.M{})
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
