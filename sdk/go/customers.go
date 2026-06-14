package meterbase

import (
	"context"
	"fmt"
)

// CustomersClient provides customer management operations.
type CustomersClient struct{ c *Client }

// CustomerCreate is the request body for creating a customer.
type CustomerCreate struct {
	ExternalID string         `json:"externalId"`
	Name       string         `json:"name,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// CustomerPatch is the request body for updating a customer.
type CustomerPatch struct {
	Name     *string        `json:"name,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Customer is a stored customer resource.
type Customer struct {
	ID         string         `json:"id"`
	ExternalID string         `json:"externalId"`
	Name       string         `json:"name,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  string         `json:"createdAt"`
}

// CustomersPage is a paginated list of customers.
type CustomersPage struct {
	NextCursor *string    `json:"nextCursor"`
	Data       []Customer `json:"data"`
}

// Create creates a new customer.
func (cu *CustomersClient) Create(ctx context.Context, req CustomerCreate) (*Customer, error) {
	var c Customer
	if err := cu.c.do(ctx, "POST", "/v1/customers", req, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Get retrieves a customer by ID.
func (cu *CustomersClient) Get(ctx context.Context, id string) (*Customer, error) {
	var c Customer
	if err := cu.c.do(ctx, "GET", fmt.Sprintf("/v1/customers/%s", id), nil, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// List returns the first page of customers.
func (cu *CustomersClient) List(ctx context.Context) (*CustomersPage, error) {
	var page CustomersPage
	if err := cu.c.do(ctx, "GET", "/v1/customers", nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// Update patches a customer by ID.
func (cu *CustomersClient) Update(ctx context.Context, id string, patch CustomerPatch) (*Customer, error) {
	var c Customer
	if err := cu.c.do(ctx, "PATCH", fmt.Sprintf("/v1/customers/%s", id), patch, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
