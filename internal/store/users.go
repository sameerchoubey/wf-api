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

func (s *Store) FindUserByID(ctx context.Context, id string) (*models.User, error) {
	var u models.User
	err := s.db.Collection("users").FindOne(ctx, bson.M{"id": id}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateUserProfile sets the display names (empty strings clear them).
func (s *Store) UpdateUserProfile(ctx context.Context, id, firstName, lastName string) error {
	_, err := s.db.Collection("users").UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{
		"first_name": firstName,
		"last_name":  lastName,
	}})
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id, passwordHash string) error {
	_, err := s.db.Collection("users").UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{
		"password_hash": passwordHash,
	}})
	return err
}
