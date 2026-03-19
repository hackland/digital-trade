package modules

import "math"

// tanhScore maps a value to [-1, +1] using tanh for smooth, non-linear scoring.
// center: the neutral point (score=0)
// scale: controls sensitivity (smaller = more sensitive near center)
func tanhScore(value, center, scale float64) float64 {
	if scale == 0 {
		return 0
	}
	return math.Tanh((value - center) / scale)
}

// clamp constrains v to [min, max].
func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// toInt converts config value to int.
func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

// toFloat converts config value to float64.
func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
