package types

import (
	"fmt"
	"strconv"
)

// ParseFloatField parses a numeric string field from an exchange API response.
// Empty strings are treated as zero, while malformed non-empty values return a
// field-specific error instead of silently becoming zero.
func ParseFloatField(field, value string) (float64, error) {
	if value == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s value %q: %w", field, value, err)
	}
	return v, nil
}
