package pricing

import (
	"encoding/json"
	"fmt"
	"math"
)

// PAYGConfig is the config for the PAYG pricing model.
// JSON: { "unitPrice": 0.002 }
type PAYGConfig struct {
	UnitPrice float64 `json:"unitPrice"`
}

// FlatPlusOverageConfig is the config for the FLAT_PLUS_OVERAGE model.
// JSON: { "flatFee": 10, "included": 1000, "overagePrice": 0.01 }
type FlatPlusOverageConfig struct {
	FlatFee      float64 `json:"flatFee"`
	Included     float64 `json:"included"`
	OveragePrice float64 `json:"overagePrice"`
}

// Tier is one price band in the TIERED model. UpTo nil means infinity.
type Tier struct {
	UpTo      *float64 `json:"upTo"`
	UnitPrice float64  `json:"unitPrice"`
}

// TieredConfig is the config for the TIERED model.
// JSON: { "mode": "graduated"|"volume", "tiers": [ { "upTo": N, "unitPrice": P } ] }
// The last tier must have upTo = null (infinity).
type TieredConfig struct {
	Mode  string `json:"mode"`
	Tiers []Tier `json:"tiers"`
}

// ComputeCost returns the cost for a given pricing model, config, and usage amount.
// It is pure: no DB, no I/O, fully deterministic given the same inputs.
// model must be one of "PAYG", "FLAT_PLUS_OVERAGE", "TIERED".
func ComputeCost(model string, configJSON json.RawMessage, usage float64) (float64, error) {
	switch model {
	case "PAYG":
		return computePAYG(configJSON, usage)
	case "FLAT_PLUS_OVERAGE":
		return computeFlatPlusOverage(configJSON, usage)
	case "TIERED":
		return computeTiered(configJSON, usage)
	default:
		return 0, fmt.Errorf("unknown pricing model: %q", model)
	}
}

func computePAYG(configJSON json.RawMessage, usage float64) (float64, error) {
	var cfg PAYGConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return 0, fmt.Errorf("PAYG config: %w", err)
	}
	return usage * cfg.UnitPrice, nil
}

func computeFlatPlusOverage(configJSON json.RawMessage, usage float64) (float64, error) {
	var cfg FlatPlusOverageConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return 0, fmt.Errorf("FLAT_PLUS_OVERAGE config: %w", err)
	}
	overage := math.Max(0, usage-cfg.Included)
	return cfg.FlatFee + overage*cfg.OveragePrice, nil
}

func computeTiered(configJSON json.RawMessage, usage float64) (float64, error) {
	var cfg TieredConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return 0, fmt.Errorf("TIERED config: %w", err)
	}
	if len(cfg.Tiers) == 0 {
		return 0, fmt.Errorf("TIERED config: tiers must not be empty")
	}
	switch cfg.Mode {
	case "graduated":
		return computeGraduated(cfg.Tiers, usage)
	case "volume":
		return computeVolume(cfg.Tiers, usage)
	default:
		return 0, fmt.Errorf("TIERED config: unknown mode %q (want graduated or volume)", cfg.Mode)
	}
}

// computeGraduated prices each usage band at its tier rate and sums them.
func computeGraduated(tiers []Tier, usage float64) (float64, error) {
	if usage == 0 {
		return 0, nil
	}
	var total float64
	prevCap := float64(0)
	for _, tier := range tiers {
		cap := math.Inf(1)
		if tier.UpTo != nil {
			cap = *tier.UpTo
		}
		usageInBand := math.Max(0, math.Min(usage, cap)-prevCap)
		total += usageInBand * tier.UnitPrice
		prevCap = cap
		if usage <= cap {
			break
		}
	}
	return total, nil
}

// computeVolume prices all usage at the rate of the tier that usage falls into.
func computeVolume(tiers []Tier, usage float64) (float64, error) {
	if usage == 0 {
		return 0, nil
	}
	for _, tier := range tiers {
		if tier.UpTo == nil || usage <= *tier.UpTo {
			return usage * tier.UnitPrice, nil
		}
	}
	// Usage exceeds all finite tiers — apply the last tier (which should have UpTo=nil).
	// This is a fallback; well-formed configs always have a nil-upTo last tier.
	last := tiers[len(tiers)-1]
	return usage * last.UnitPrice, nil
}
