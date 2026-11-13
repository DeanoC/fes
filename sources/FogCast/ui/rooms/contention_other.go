//go:build !linux

package rooms

// readHostContention is nil without /proc, so a deadline miss reports no
// contention summary.
var readHostContention func() (contentionSnapshot, bool)
