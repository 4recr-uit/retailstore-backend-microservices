package db

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type InMemoryRepository struct {
	mu        sync.RWMutex
	orders    map[string]ExistingOrder
	orderList []ExistingOrder
	seenJobs  map[string]bool
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		orders:    make(map[string]ExistingOrder),
		orderList: make([]ExistingOrder, 0),
		seenJobs:  make(map[string]bool),
	}
}

func (r *InMemoryRepository) Close() error {
	return nil
}

func (r *InMemoryRepository) Ping(ctx context.Context) error {
	return nil
}

func (r *InMemoryRepository) ListOrders(ctx context.Context) ([]ExistingOrder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ExistingOrder, len(r.orderList))
	copy(result, r.orderList)
	return result, nil
}

func (r *InMemoryRepository) CreateOrder(ctx context.Context, order Order) (ExistingOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := ExistingOrder{
		ID:              uuid.New().String(),
		CreatedDate:     time.Now().UTC().Format(time.RFC3339),
		ShippingAddress: order.ShippingAddress,
		Items:           make([]OrderItem, len(order.Items)),
	}
	copy(existing.Items, order.Items)

	r.orders[existing.ID] = existing
	r.orderList = append([]ExistingOrder{existing}, r.orderList...)

	return existing, nil
}

func (r *InMemoryRepository) ProcessLight(ctx context.Context, orderID, jobID, customerID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if jobID != "" && r.seenJobs[jobID] {
		return false, nil
	}
	if jobID != "" {
		r.seenJobs[jobID] = true
	}

	if orderID == "" {
		orderID = uuid.New().String()
	}

	existing := ExistingOrder{
		ID:          orderID,
		CreatedDate: time.Now().UTC().Format(time.RFC3339),
		Items:       []OrderItem{},
	}
	r.orders[orderID] = existing
	r.orderList = append([]ExistingOrder{existing}, r.orderList...)

	return true, nil
}

func (r *InMemoryRepository) ProcessFull(ctx context.Context, orderID, jobID, customerID string, order Order) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if jobID != "" && r.seenJobs[jobID] {
		return false, nil
	}
	if jobID != "" {
		r.seenJobs[jobID] = true
	}

	if orderID == "" {
		orderID = uuid.New().String()
	}

	existing := ExistingOrder{
		ID:              orderID,
		CreatedDate:     time.Now().UTC().Format(time.RFC3339),
		ShippingAddress: order.ShippingAddress,
		Items:           make([]OrderItem, len(order.Items)),
	}
	copy(existing.Items, order.Items)

	r.orders[orderID] = existing
	r.orderList = append([]ExistingOrder{existing}, r.orderList...)

	return true, nil
}
