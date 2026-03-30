package store

import "go.mongodb.org/mongo-driver/mongo"

// Store provides MongoDB access for the application.
type Store struct {
	db *mongo.Database
}

func New(db *mongo.Database) *Store {
	return &Store{db: db}
}
