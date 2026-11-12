//go:build !linux

package rooms

// readHostContention is nil without /proc, so a deadline miss reports no
// contention summary.
var readHostContention func() (contentionSnapshot, bool)

// lowerContentionPriority has nothing to do: there is no background read.
func lowerContentionPriority() {}
