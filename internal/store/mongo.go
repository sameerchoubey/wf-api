package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Connect dials MongoDB with a bounded server selection timeout.
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	opts := options.Client().ApplyURI(uri).SetServerSelectionTimeout(10 * time.Second)
	return mongo.Connect(ctx, opts)
}
