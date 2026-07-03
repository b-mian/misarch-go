package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// ConnectMongo connects to MongoDB using MONGODB_URI and returns the named
// database, retrying until the server is reachable.
func ConnectMongo(ctx context.Context, database string) (*mongo.Database, error) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		return nil, fmt.Errorf("MONGODB_URI is not set")
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = client.Ping(pingCtx, readpref.Primary())
		cancel()
		if err == nil {
			return client.Database(database), nil
		}
		if attempt >= 30 {
			return nil, fmt.Errorf("mongo unreachable after %d attempts: %w", attempt, err)
		}
		slog.Info("waiting for mongo", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
