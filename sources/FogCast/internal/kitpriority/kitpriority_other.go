//go:build !linux

package kitpriority

// lowerThreads is a no-op off Linux; kits run Linux.
func lowerThreads(int) error { return nil }
