package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type inserterKey struct{}

// WithClient lets code outside a job (CLI, watcher) enqueue through Enqueue. Inside a job River's own
// client is found in the context.
func WithClient(ctx context.Context, client *Client) context.Context {
	return context.WithValue(ctx, inserterKey{}, client)
}

var errNoClient = errors.New("no river client in context")

func clientFrom(ctx context.Context) (*Client, error) {
	if c, ok := ctx.Value(inserterKey{}).(*Client); ok {
		return c, nil
	}
	if c, err := river.ClientFromContextSafely[pgx.Tx](ctx); err == nil {
		return c, nil
	}
	return nil, errNoClient
}

// Enqueue inserts jobs in tx, so they exist only if the state change that caused them commits (ATLAS.md §7.1).
// Each job's queue and uniqueness come from its InsertOpts.
func Enqueue(ctx context.Context, tx pgx.Tx, args ...river.JobArgs) error {
	if len(args) == 0 {
		return nil
	}
	client, err := clientFrom(ctx)
	if err != nil {
		return err
	}
	params := make([]river.InsertManyParams, len(args))
	for i, a := range args {
		params[i] = river.InsertManyParams{Args: a}
	}
	if _, err := client.InsertManyTx(ctx, tx, params); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}
