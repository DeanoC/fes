// SPDX-License-Identifier: GPL-3.0-or-later
package arm64map

import "errors"

// WithStableMapping issues an operation once between identical successful
// mapping observations. A failure after issue returns no successful result;
// its caller must retire the operation and retain any uncertain DMA storage.
// This helper supplies no lifecycle, retries or memory reservation.
func WithStableMapping[T any](observe func() (Observation, error), issue func() T) (T, error) {
	var zero T
	if observe == nil || issue == nil {
		return zero, errors.New("explicit mapping guard and operation required")
	}
	before, err := observe()
	if err != nil {
		return zero, err
	}
	result := issue()
	after, err := observe()
	if err != nil {
		return zero, err
	}
	if before != after {
		return zero, errors.New("live mapping changed during operation")
	}
	return result, nil
}
