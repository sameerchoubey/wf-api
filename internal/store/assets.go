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
