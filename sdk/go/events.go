package meterbase

import "context"

// Event is a usage event to ingest.
type Event struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Source  string         `json:"source,omitempty"`
	Subject string         `json:"subject"`
	Time    string         `json:"time,omitempty"` // RFC3339; defaults to server receipt time if empty
	Data    map[string]any `json:"data,omitempty"`
}

// IngestResponse is returned by Track and TrackBatch.
type IngestResponse struct {
	Received   int `json:"received"`
	Duplicates int `json:"duplicates"`
}

// Track ingests a single event.
func (c *Client) Track(ctx context.Context, event Event) (*IngestResponse, error) {
	var resp IngestResponse
	if err := c.do(ctx, "POST", "/v1/events", event, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TrackBatch ingests multiple events in one request.
func (c *Client) TrackBatch(ctx context.Context, events []Event) (*IngestResponse, error) {
	var resp IngestResponse
	if err := c.do(ctx, "POST", "/v1/events", events, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
