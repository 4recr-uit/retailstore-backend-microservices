package db

import (
	"context"
)

type ShippingAddress struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2,omitempty"`
	City      string `json:"city"`
	ZipCode   string `json:"zipCode"`
	State     string `json:"state"`
}

type OrderItem struct {
	ID        int    `json:"id,omitempty"`
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
	UnitCost  int    `json:"unitCost"`
	TotalCost int    `json:"totalCost"`
}

type Order struct {
	ShippingAddress ShippingAddress `json:"shippingAddress"`
	Items           []OrderItem     `json:"items"`
}

type ExistingOrder struct {
	ID              string          `json:"id"`
	CreatedDate     string          `json:"createdDate"`
	ShippingAddress ShippingAddress `json:"shippingAddress"`
	Items           []OrderItem     `json:"items"`
}

type Repository interface {
	Close() error
	ListOrders(ctx context.Context) ([]ExistingOrder, error)
	CreateOrder(ctx context.Context, order Order) (ExistingOrder, error)
	ProcessLight(ctx context.Context, orderID, jobID, customerID string) (isNew bool, err error)
	ProcessFull(ctx context.Context, orderID, jobID, customerID string, order Order) (isNew bool, err error)
	Ping(ctx context.Context) error
}
