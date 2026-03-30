package store

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"wealthflow/backend/internal/models"
)

func (s *Store) FindUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := s.db.Collection("users").FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) InsertUser(ctx context.Context, u *models.User) error {
	_, err := s.db.Collection("users").InsertOne(ctx, u)
	return err
}
