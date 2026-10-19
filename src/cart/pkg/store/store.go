package store

import (
	"context"
	"errors"

	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid parameter")
)

// Store defines operations for cart persistence and job idempotency.
type Store interface {
	// Cart operations
	GetCart(ctx context.Context, customerID string) (*model.Cart, error)
	DeleteCart(ctx context.Context, customerID string) (*model.Cart, error)
	MergeCarts(ctx context.Context, customerID, sessionID string) error
	GetItems(ctx context.Context, customerID string) ([]model.Item, error)
	AddItem(ctx context.Context, customerID string, item model.Item) (*model.Item, error)
	UpdateItem(ctx context.Context, customerID string, item model.Item) error
	GetItem(ctx context.Context, customerID, itemID string) (*model.Item, error)
	DeleteItem(ctx context.Context, customerID, itemID string) error

	// Worker job execution and idempotency
	ProcessJob(ctx context.Context, job model.JobMessage) (isNew bool, err error)

	// Health and lifecycle
	Ping(ctx context.Context) error
	Close() error
}
