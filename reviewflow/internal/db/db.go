package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Client wraps the raw mongo.Client together with the resolved database handle,
// so callers (repositories, handlers) don't have to repeat the database name.
type Client struct {
	Mongo    *mongo.Client
	Database *mongo.Database
}

// Connect dials MongoDB and verifies connectivity with a Ping. It fails fast:
// if the database is unreachable within the timeout, it returns an error and
// the caller is expected to abort startup rather than serve an API that can't
// read or write anything.
func Connect(uri, dbName string) (*Client, error) {
	// A single 10s budget covers the whole connect + ping handshake, including
	// the initial server selection against a cold MongoDB container.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	// mongo.Connect is lazy and does not actually contact the server, so an
	// explicit Ping against the primary is what surfaces an unreachable DB.
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	log.Printf("db: connected to MongoDB, using database %q", dbName)
	return &Client{
		Mongo:    client,
		Database: client.Database(dbName),
	}, nil
}

// Disconnect cleanly closes the underlying connections. Call this during
// graceful shutdown, after the HTTP server has stopped accepting requests.
func (c *Client) Disconnect(ctx context.Context) error {
	return c.Mongo.Disconnect(ctx)
}
