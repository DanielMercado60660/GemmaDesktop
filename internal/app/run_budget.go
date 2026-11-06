package app

import (
	"errors"
	"math"
	"time"
)

// Callers may narrow the selected ordinary/qualified budget, never widen it.
// Missing legacy fields retain the exact previously selected limit.
func narrowedRunTimeout(params Doc, admitted time.Duration) (time.Duration, error) {
	value, present := params["budgetSeconds"]
	if !present {
		return admitted, nil
	}
	seconds, ok := finiteNumber(value)
	if !ok || math.Trunc(seconds) != seconds || seconds < 30 || seconds > 1200 {
		return 0, errors.New("budgetSeconds must be an integer from 30 to 1200")
	}
	selected := time.Duration(seconds) * time.Second
	if selected > admitted {
		return 0, errors.New("budgetSeconds cannot exceed the admitted run budget")
	}
	return selected, nil
}
