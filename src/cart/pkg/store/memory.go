package store

import (
	"context"
	"sync"

	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
)

// MemoryStore provides a thread-safe in-memory store for carts and job tracking.
type MemoryStore struct {
	mu            sync.RWMutex
	carts         map[string]map[string]model.Item
	processedJobs map[string]bool
}

// NewMemoryStore creates and initializes a new MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		carts:         make(map[string]map[string]model.Item),
		processedJobs: make(map[string]bool),
	}
}

func (m *MemoryStore) GetCart(ctx context.Context, customerID string) (*model.Cart, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]model.Item, 0)
	if itemMap, ok := m.carts[customerID]; ok {
		for _, it := range itemMap {
			items = append(items, it)
		}
	}
	return &model.Cart{
		CustomerID: customerID,
		Items:      items,
	}, nil
}

func (m *MemoryStore) DeleteCart(ctx context.Context, customerID string) (*model.Cart, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.carts, customerID)
	return &model.Cart{
		CustomerID: customerID,
		Items:      make([]model.Item, 0),
	}, nil
}

func (m *MemoryStore) MergeCarts(ctx context.Context, customerID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessionItems, ok := m.carts[sessionID]
	if !ok || len(sessionItems) == 0 {
		delete(m.carts, sessionID)
		return nil
	}

	custItems, ok := m.carts[customerID]
	if !ok {
		custItems = make(map[string]model.Item)
		m.carts[customerID] = custItems
	}

	for id, sItem := range sessionItems {
		if cItem, exists := custItems[id]; exists {
			cItem.Quantity += sItem.Quantity
			cItem.UnitPrice = sItem.UnitPrice
			custItems[id] = cItem
		} else {
			custItems[id] = sItem
		}
	}

	delete(m.carts, sessionID)
	return nil
}

func (m *MemoryStore) GetItems(ctx context.Context, customerID string) ([]model.Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]model.Item, 0)
	if itemMap, ok := m.carts[customerID]; ok {
		for _, it := range itemMap {
			items = append(items, it)
		}
	}
	return items, nil
}

func (m *MemoryStore) AddItem(ctx context.Context, customerID string, item model.Item) (*model.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	custItems, ok := m.carts[customerID]
	if !ok {
		custItems = make(map[string]model.Item)
		m.carts[customerID] = custItems
	}

	if existing, exists := custItems[item.ItemID]; exists {
		existing.Quantity += item.Quantity
		existing.UnitPrice = item.UnitPrice
		custItems[item.ItemID] = existing
		res := existing
		return &res, nil
	}

	custItems[item.ItemID] = item
	res := item
	return &res, nil
}

func (m *MemoryStore) UpdateItem(ctx context.Context, customerID string, item model.Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	custItems, ok := m.carts[customerID]
	if !ok {
		return ErrNotFound
	}

	if _, exists := custItems[item.ItemID]; !exists {
		return ErrNotFound
	}

	custItems[item.ItemID] = item
	return nil
}

func (m *MemoryStore) GetItem(ctx context.Context, customerID, itemID string) (*model.Item, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	custItems, ok := m.carts[customerID]
	if !ok {
		return nil, ErrNotFound
	}

	item, exists := custItems[itemID]
	if !exists {
		return nil, ErrNotFound
	}

	res := item
	return &res, nil
}

func (m *MemoryStore) DeleteItem(ctx context.Context, customerID, itemID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if custItems, ok := m.carts[customerID]; ok {
		delete(custItems, itemID)
	}
	return nil
}

func (m *MemoryStore) ProcessJob(ctx context.Context, job model.JobMessage) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if job.DBMode == "none" {
		return true, nil
	}

	if m.processedJobs[job.JobID] {
		return false, nil // duplicate job
	}

	m.processedJobs[job.JobID] = true

	if job.DBMode == "full" && job.CustomerID != "" {
		custItems, ok := m.carts[job.CustomerID]
		if !ok {
			custItems = make(map[string]model.Item)
			m.carts[job.CustomerID] = custItems
		}
		it := custItems["job-item"]
		it.ItemID = "job-item"
		it.Quantity += 1
		it.UnitPrice = 100
		custItems["job-item"] = it
	}

	return true, nil
}

func (m *MemoryStore) Ping(ctx context.Context) error {
	return nil
}

func (m *MemoryStore) Close() error {
	return nil
}
