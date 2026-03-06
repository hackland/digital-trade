package trend

// Helper functions for config parsing, shared across all trend strategies.

// toInt converts an interface{} (from YAML/JSON config) to int.
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

// toFloat converts an interface{} to float64.
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

// toBool converts an interface{} to bool.
func toBool(v interface{}) bool {
	if val, ok := v.(bool); ok {
		return val
	}
	return false
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
