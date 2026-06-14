package meterbase

import (
	"context"
	"fmt"
	"net/url"
)

// MetersClient provides meter querying operations.
type MetersClient struct{ c *Client }

// QueryParams specifies filters for a meter query.
type QueryParams struct {
	From       string   // RFC3339, required
	To         string   // RFC3339, required
	WindowSize string   // MINUTE|HOUR|DAY|MONTH, required
	Subject    string   // optional filter
	CustomerID string   // optional filter
	GroupBy    []string // optional dimension paths
}

// QueryDataPoint is one time bucket in a query result.
type QueryDataPoint struct {
	Bucket string            `json:"bucket"`
	Value  float64           `json:"value"`
	Groups map[string]string `json:"groups,omitempty"`
}

// QueryResult is the response from a meter query.
type QueryResult struct {
	Meter      string           `json:"meter"`
	WindowSize string           `json:"windowSize"`
	Data       []QueryDataPoint `json:"data"`
}

// Query returns time-bucketed aggregates for the named meter.
func (m *MetersClient) Query(ctx context.Context, slug string, p QueryParams) (*QueryResult, error) {
	q := url.Values{}
	q.Set("from", p.From)
	q.Set("to", p.To)
	q.Set("windowSize", p.WindowSize)
	if p.Subject != "" {
		q.Set("subject", p.Subject)
	}
	if p.CustomerID != "" {
		q.Set("customerId", p.CustomerID)
	}
	for _, g := range p.GroupBy {
		q.Add("groupBy", g)
	}
	var result QueryResult
	if err := m.c.do(ctx, "GET", fmt.Sprintf("/v1/meters/%s/query?%s", slug, q.Encode()), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
