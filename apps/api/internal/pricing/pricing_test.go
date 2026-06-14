package pricing

import (
	"encoding/json"
	"math"
	"testing"
)

const epsilon = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

// ── PAYG ─────────────────────────────────────────────────────────────────────

func TestPAYG(t *testing.T) {
	cases := []struct {
		name      string
		unitPrice float64
		usage     float64
		want      float64
	}{
		{"zero usage", 0.01, 0, 0},
		{"normal", 0.01, 100, 1.00},
		{"fractional", 0.002, 1500, 3.00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := json.Marshal(PAYGConfig{UnitPrice: tc.unitPrice})
			got, err := ComputeCost("PAYG", cfg, tc.usage)
			if err != nil {
				t.Fatalf("ComputeCost: %v", err)
			}
			if !approxEqual(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// ── FLAT_PLUS_OVERAGE ─────────────────────────────────────────────────────────

func TestFlatPlusOverage(t *testing.T) {
	cases := []struct {
		name         string
		flatFee      float64
		included     float64
		overagePrice float64
		usage        float64
		want         float64
	}{
		{"zero usage", 5, 100, 0.01, 0, 5.00},
		{"within included", 10, 1000, 0.01, 500, 10.00},
		{"at boundary", 10, 1000, 0.01, 1000, 10.00},
		{"overage", 10, 1000, 0.01, 1500, 15.00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := json.Marshal(FlatPlusOverageConfig{
				FlatFee: tc.flatFee, Included: tc.included, OveragePrice: tc.overagePrice,
			})
			got, err := ComputeCost("FLAT_PLUS_OVERAGE", cfg, tc.usage)
			if err != nil {
				t.Fatalf("ComputeCost: %v", err)
			}
			if !approxEqual(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// ── TIERED graduated ─────────────────────────────────────────────────────────

// Tiers: [{upTo:100, unitPrice:0.10}, {upTo:nil, unitPrice:0.05}]
func tieredCfg(mode string) json.RawMessage {
	upTo100 := float64(100)
	cfg, _ := json.Marshal(TieredConfig{
		Mode: mode,
		Tiers: []Tier{
			{UpTo: &upTo100, UnitPrice: 0.10},
			{UpTo: nil, UnitPrice: 0.05},
		},
	})
	return cfg
}

func TestTieredGraduated(t *testing.T) {
	cfg := tieredCfg("graduated")
	cases := []struct {
		name  string
		usage float64
		want  float64
	}{
		{"zero", 0, 0},
		{"within tier1", 50, 5.00},
		{"at tier1 cap", 100, 10.00},
		{"span two tiers", 150, 12.50},
		{"large", 1100, 60.00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeCost("TIERED", cfg, tc.usage)
			if err != nil {
				t.Fatalf("ComputeCost: %v", err)
			}
			if !approxEqual(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// ── TIERED volume ─────────────────────────────────────────────────────────────

func TestTieredVolume(t *testing.T) {
	cfg := tieredCfg("volume")
	cases := []struct {
		name  string
		usage float64
		want  float64
	}{
		{"zero", 0, 0},
		{"within tier1", 50, 5.00},
		{"at tier1 cap", 100, 10.00},
		{"just over tier1", 101, 5.05},
		{"large", 1000, 50.00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeCost("TIERED", cfg, tc.usage)
			if err != nil {
				t.Fatalf("ComputeCost: %v", err)
			}
			if !approxEqual(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// ── REPRODUCIBILITY ───────────────────────────────────────────────────────────

// ComputeCost must be deterministic: same inputs always yield the same output.
func TestReproducibility(t *testing.T) {
	upTo100 := float64(100)
	cases := []struct {
		model  string
		config any
		usage  float64
	}{
		{"PAYG", PAYGConfig{UnitPrice: 0.002}, 1500},
		{"FLAT_PLUS_OVERAGE", FlatPlusOverageConfig{FlatFee: 10, Included: 1000, OveragePrice: 0.01}, 1500},
		{"TIERED", TieredConfig{
			Mode:  "graduated",
			Tiers: []Tier{{UpTo: &upTo100, UnitPrice: 0.10}, {UpTo: nil, UnitPrice: 0.05}},
		}, 150},
		{"TIERED", TieredConfig{
			Mode:  "volume",
			Tiers: []Tier{{UpTo: &upTo100, UnitPrice: 0.10}, {UpTo: nil, UnitPrice: 0.05}},
		}, 150},
	}
	for _, tc := range cases {
		cfg, _ := json.Marshal(tc.config)
		first, err := ComputeCost(tc.model, cfg, tc.usage)
		if err != nil {
			t.Fatalf("%s ComputeCost first call: %v", tc.model, err)
		}
		second, err := ComputeCost(tc.model, cfg, tc.usage)
		if err != nil {
			t.Fatalf("%s ComputeCost second call: %v", tc.model, err)
		}
		if first != second {
			t.Errorf("%s: first=%v second=%v — not deterministic", tc.model, first, second)
		}
	}
}

// ── ERROR CASES ───────────────────────────────────────────────────────────────

func TestComputeCostErrors(t *testing.T) {
	paygCfg, _ := json.Marshal(PAYGConfig{UnitPrice: 0.01})

	if _, err := ComputeCost("UNKNOWN_MODEL", paygCfg, 100); err == nil {
		t.Error("expected error for unknown model")
	}
	if _, err := ComputeCost("PAYG", json.RawMessage(`not-json`), 100); err == nil {
		t.Error("expected error for malformed JSON config")
	}
	if _, err := ComputeCost("TIERED", json.RawMessage(`{"mode":"graduated","tiers":[]}`), 100); err == nil {
		t.Error("expected error for empty tiers")
	}
	if _, err := ComputeCost("TIERED", json.RawMessage(`{"mode":"bad","tiers":[{"unitPrice":0.1}]}`), 100); err == nil {
		t.Error("expected error for invalid tiered mode")
	}
}
