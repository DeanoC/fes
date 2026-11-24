package localhid

import (
	"context"
	"errors"
)

// ErrMatrixNotDispatched denotes a proven rejection before any runtime write.
// Once the runtime call begins, failures must never use this sentinel.
var ErrMatrixNotDispatched = errors.New("matrix operation was not dispatched")

// MatrixWitness identifies one sink operation and the runtime authority that
// admitted it. Sink is freshly generated per sink lifetime; Sequence never
// wraps or repeats within that lifetime. An empty PackageID is an explicit
// contained-development binding, not a wildcard.
type MatrixWitness struct {
	Sink            [16]byte
	Sequence        uint64
	Source          uint8
	RuntimeInstance string
	PackageID       string
	Generation      uint64
}

// MatrixPost carries the exact previous uncertainty when the sink has removed
// that source from its union. Validate is a bounded, nonrecursive authority
// check performed after the physical post, while both the sink and adapter
// still serialize the operation. Neither this value nor its callback crosses
// the wire.
type MatrixPost struct {
	Witness   MatrixWitness
	NeutralOf *MatrixWitness
	Validate  func(context.Context) error
}

// MatrixRetirement is assembled only after the failed operation drained,
// while the sink and controller lifecycle are fenced. The observation is
// explicit idle or an exact different running authority; absence is not idle.
// Validate rechecks that the same lifecycle/lease fence is still held.
type MatrixRetirement struct {
	RetireOf                MatrixWitness
	ObservedRuntimeInstance string
	ObservedPackageID       string
	ObservedGeneration      uint64
	Idle                    bool
	Validate                func(context.Context) error
}

type MatrixMediaWitness struct {
	Operation       [16]byte
	RuntimeInstance string
	PackageID       string
	Generation      uint64
}

type MatrixMediaRetirement struct {
	RetireOf                MatrixMediaWitness
	ObservedRuntimeInstance string
	ObservedPackageID       string
	ObservedGeneration      uint64
	Idle                    bool
	Validate                func(context.Context) error
}

// MatrixMediaNeutral authorizes only an all-source neutral on the exact
// media binding. When SourceOf is present, both exact latches must be matched
// and cleared atomically only after the same confirmed neutral write.
type MatrixMediaNeutral struct {
	MediaOf  MatrixMediaWitness
	SourceOf *MatrixWitness
	Validate func(context.Context) error
}

func (w MatrixWitness) Valid() bool {
	var zero [16]byte
	return w.Sink != zero && validNumber(w.Sequence) && w.Source < 3 && nonzeroID(w.RuntimeInstance, true) &&
		(w.PackageID == "" || nonzeroID(w.PackageID, false)) && w.Generation != 0
}
