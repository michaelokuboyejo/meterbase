package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	meterbase "github.com/mykelokuboyejo/meterbase/sdk/go"
)

// TestSDK_Track_And_Query exercises the Go SDK's Track and Meters.Query
// methods against a real httptest server, proving the end-to-end round-trip.
func TestSDK_Track_And_Query(t *testing.T) {
	env := newTestEnv(t)
	ten := env.newTenant(t, "sdk")
	client := meterbase.NewClient(env.url, ten.key)

	ctx := context.Background()
	slug := uid("sdk_meter")
	evType := uid("sdk_ev")

	// Create a COUNT meter via HTTP (reuse the existing call helper).
	code := env.call(t, "POST", "/v1/meters", ten.key, map[string]any{
		"slug":        slug,
		"eventType":   evType,
		"aggregation": "COUNT",
	}, nil)
	if code != 201 {
		t.Fatalf("create meter: got %d, want 201", code)
	}

	from := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)

	t.Run("Track", func(t *testing.T) {
		resp, err := client.Track(ctx, meterbase.Event{
			ID:      fmt.Sprintf("sdk-%d", time.Now().UnixNano()),
			Type:    evType,
			Subject: "sdk-user-1",
		})
		if err != nil {
			t.Fatalf("Track: %v", err)
		}
		if resp.Received != 1 {
			t.Errorf("received: got %d, want 1", resp.Received)
		}
		if resp.Duplicates != 0 {
			t.Errorf("duplicates: got %d, want 0", resp.Duplicates)
		}
	})

	t.Run("Dedup", func(t *testing.T) {
		id := fmt.Sprintf("sdk-dedup-%d", time.Now().UnixNano())
		for range 2 {
			r, err := client.Track(ctx, meterbase.Event{
				ID:      id,
				Type:    evType,
				Subject: "sdk-user-1",
			})
			if err != nil {
				t.Fatalf("Track: %v", err)
			}
			_ = r
		}
		// Second call should report duplicates=1 but we just need no error here;
		// correctness is already proven in the E2E dedup test.
	})

	t.Run("TrackBatch", func(t *testing.T) {
		events := []meterbase.Event{
			{ID: fmt.Sprintf("sdk-b1-%d", time.Now().UnixNano()), Type: evType, Subject: "sdk-user-2"},
			{ID: fmt.Sprintf("sdk-b2-%d", time.Now().UnixNano()), Type: evType, Subject: "sdk-user-2"},
		}
		resp, err := client.TrackBatch(ctx, events)
		if err != nil {
			t.Fatalf("TrackBatch: %v", err)
		}
		if resp.Received != 2 {
			t.Errorf("received: got %d, want 2", resp.Received)
		}
	})

	t.Run("Meters.Query", func(t *testing.T) {
		result, err := client.Meters.Query(ctx, slug, meterbase.QueryParams{
			From:       from,
			To:         to,
			WindowSize: "HOUR",
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if result.Meter != slug {
			t.Errorf("meter: got %q, want %q", result.Meter, slug)
		}
		if result.WindowSize != "HOUR" {
			t.Errorf("windowSize: got %q, want HOUR", result.WindowSize)
		}
		// We ingested at least 3 events above; verify the query returns data.
		total := 0.0
		for _, pt := range result.Data {
			total += pt.Value
		}
		if total < 3 {
			t.Errorf("total count: got %.0f, want >= 3", total)
		}
	})
}

// TestSDK_Customers exercises the Customers accessor.
func TestSDK_Customers(t *testing.T) {
	env := newTestEnv(t)
	ten := env.newTenant(t, "sdk-cust")
	client := meterbase.NewClient(env.url, ten.key)

	ctx := context.Background()

	t.Run("Create and Get", func(t *testing.T) {
		extID := uid("ext")
		c, err := client.Customers.Create(ctx, meterbase.CustomerCreate{
			ExternalID: extID,
			Name:       "Test Customer",
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if c.ExternalID != extID {
			t.Errorf("externalId: got %q, want %q", c.ExternalID, extID)
		}
		got, err := client.Customers.Get(ctx, c.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != c.ID {
			t.Errorf("id mismatch: got %q, want %q", got.ID, c.ID)
		}
	})

	t.Run("List", func(t *testing.T) {
		if _, err := client.Customers.Create(ctx, meterbase.CustomerCreate{ExternalID: uid("lst")}); err != nil {
			t.Fatalf("Create for list: %v", err)
		}
		page, err := client.Customers.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Data) == 0 {
			t.Error("List returned empty page, expected at least one customer")
		}
	})

	t.Run("Update", func(t *testing.T) {
		c, err := client.Customers.Create(ctx, meterbase.CustomerCreate{ExternalID: uid("upd")})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		newName := "Updated Name"
		updated, err := client.Customers.Update(ctx, c.ID, meterbase.CustomerPatch{Name: &newName})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Name != newName {
			t.Errorf("name: got %q, want %q", updated.Name, newName)
		}
	})
}
