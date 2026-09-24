package meshcontent

import (
	"errors"
)

// SlotState is presence of one required content-id on the bound executor.
// StateChecking is a slot mid-pull. Sofa copy for that state is Checking.
// It is not Present, and it does not allow execute.
type SlotState string

const (
	StatePresent  SlotState = "present"
	StateChecking SlotState = "checking"
	StateMissing  SlotState = "missing"
)

var (
	// ErrContentMissingNoSource is the failure class for a required
	// content-id that is missing on the bound executor and that no
	// content source advertises. Callers fail closed. It is not a
	// generic error.
	ErrContentMissingNoSource = errors.New("content missing, no source")
	// ErrUnboundNode refuses a pull onto a node the session did not
	// bind. Ensure does not choose a different node.
	ErrUnboundNode = errors.New("pull refused for a node the session did not bind")
	// ErrExecuteBlocked means Ensure finished without every required
	// slot Present, or the executor cannot run the package ABI.
	// Callers must not program the FPGA or start execution.
	ErrExecuteBlocked = errors.New("mesh execute blocked")
	errSlotState      = errors.New("meshcontent: executor slot state is invalid")
	errPullMissed     = errors.New("meshcontent: pull left the content-id missing")
)

// ContentMissingError is ErrContentMissingNoSource for one slot.
type ContentMissingError struct {
	Kind string
	Name string
	ID   ContentID
}

func (e *ContentMissingError) Error() string {
	label := e.Kind
	if e.Name != "" {
		label += " " + e.Name
	}
	return "content missing, no source: " + label + " " + e.ID.String()
}

func (e *ContentMissingError) Unwrap() error { return ErrContentMissingNoSource }

// ExecuteBlockedError is ErrExecuteBlocked with the ReadyHere block that
// explains it. Checking (a slot mid-pull) uses BlockEnsureProgress.
type ExecuteBlockedError struct {
	Block Block
}

func (e *ExecuteBlockedError) Error() string {
	if e == nil || e.Block == "" {
		return ErrExecuteBlocked.Error()
	}
	return ErrExecuteBlocked.Error() + ": " + string(e.Block)
}

func (e *ExecuteBlockedError) Unwrap() error { return ErrExecuteBlocked }

// Executor is the node this session is already bound to.
// Ensure does not pick another node, does not release or change a
// lease, and does not transfer bytes. A kit store behind the target
// agent is a later slice. Tests pass a fake.
type Executor interface {
	// NodeID is this executor. Ensure refuses the call when it is not
	// the session's bound node.
	NodeID() string
	// Slot reports id on this executor: Present, Checking, or Missing.
	// Checking means a pull is already in progress.
	Slot(id ContentID) SlotState
	// SourceAdvertises reports whether some content source can serve id.
	// The source may be another node. The pull still lands on this one.
	SourceAdvertises(id ContentID) bool
	// Pull copies id from a source onto this executor.
	// Checking means the copy is still running. Present means it finished.
	Pull(id ContentID) (SlotState, error)
	// LinkExpansion links one expansion's slot bytes on this executor.
	// id is that slot's content-id. The host does not pre-link those
	// bytes with primary media or with any other slot.
	LinkExpansion(name string, id ContentID) error
	// EligibleABIs are the package ABI id and major pairs this executor
	// can run. An empty list is not eligibility.
	EligibleABIs() []EligibleABI
}

// SlotStatus is one required content slot after Ensure. Package / ABI
// is not a content slot; eligibility is Result.Block.
type SlotStatus struct {
	Kind    string
	Name    string
	Content ContentID
	State   SlotState
}

// Result is the per-slot outcome for one bound executor.
// Execute is true only when every required content slot is Present and,
// for package-backed entries, the executor's ABI id and major are
// eligible. A Checking slot keeps Execute false. Ensure does not
// program the FPGA.
type Result struct {
	Node    string
	Slots   []SlotStatus
	Execute bool
	Block   Block
}

type ensurePlan struct {
	slot  Slot
	state SlotState
	pull  bool
}

// Ensure makes every required content-id Present on the executor the
// session bound, or reports why execute must not start.
//
// Primary-media content-ids are the format-3 source digest (catalog
// MediaID, recorded on the executor as SourceSHA256). Expansion
// content-ids are the slot-bytes digest (MeshExpansion.Digest, the
// cart payload's SHA-256). Ensure does not read a post-link
// ProgrammedSHA256 and does not link expansion bytes on the host.
// A slot that is already Checking stays Checking and is not pulled
// again. A required id that is missing and has no source returns
// ErrContentMissingNoSource before any pull.
func Ensure(entry Entry, boundNode string, exec Executor) (Result, error) {
	if err := entry.Validate(); err != nil {
		return Result{}, err
	}
	if exec == nil || boundNode == "" || exec.NodeID() != boundNode {
		return Result{}, ErrUnboundNode
	}
	if !entry.Launchable {
		return Result{Node: boundNode, Execute: false, Block: BlockBrowseOnly}, nil
	}

	plans := make([]ensurePlan, 0, len(entry.Slots))
	for _, slot := range entry.Slots {
		if slot.Content == nil {
			continue
		}
		state, ok := normalizeState(exec.Slot(*slot.Content))
		if !ok {
			return Result{}, errSlotState
		}
		plan := ensurePlan{slot: slot, state: state}
		if state == StateMissing {
			if !exec.SourceAdvertises(*slot.Content) {
				return Result{}, &ContentMissingError{Kind: slot.Kind, Name: slot.Name, ID: *slot.Content}
			}
			plan.pull = true
		}
		plans = append(plans, plan)
	}
	for i := range plans {
		if !plans[i].pull {
			continue
		}
		id := *plans[i].slot.Content
		next, err := exec.Pull(id)
		if err != nil {
			return Result{}, err
		}
		state, ok := normalizeState(next)
		if !ok {
			return Result{}, errSlotState
		}
		if state == StateMissing {
			// A source advertised this id. A pull that still reports
			// Missing is a failed copy, not the no-source class.
			return Result{}, errPullMissed
		}
		plans[i].state = state
	}
	slots := make([]SlotStatus, 0, len(plans))
	sawChecking := false
	allPresent := true
	for _, plan := range plans {
		if plan.slot.Kind == SlotExpansion && plan.state == StatePresent {
			if err := exec.LinkExpansion(plan.slot.Name, *plan.slot.Content); err != nil {
				return Result{}, err
			}
		}
		if plan.state == StateChecking {
			sawChecking = true
		}
		if plan.state != StatePresent {
			allPresent = false
		}
		slots = append(slots, SlotStatus{
			Kind:    plan.slot.Kind,
			Name:    plan.slot.Name,
			Content: *plan.slot.Content,
			State:   plan.state,
		})
	}

	result := Result{Node: boundNode, Slots: slots, Execute: allPresent, Block: BlockNone}
	if sawChecking {
		result.Execute = false
		result.Block = BlockEnsureProgress
	}
	if packageBacked(entry.Execute[0].Kind) {
		if block := abiBlock(entry, exec.EligibleABIs()); block != BlockNone {
			result.Execute = false
			if result.Block == BlockNone {
				result.Block = block
			}
		}
	}
	if !result.Execute && result.Block == BlockNone {
		result.Block = BlockContentMissing
	}
	return result, nil
}

// Blocked returns ErrExecuteBlocked when result.Execute is false.
// A true result returns nil. Callers must not program while the error
// is non-nil.
func (r Result) Blocked() error {
	if r.Execute {
		return nil
	}
	return &ExecuteBlockedError{Block: r.Block}
}

func normalizeState(state SlotState) (SlotState, bool) {
	switch state {
	case StatePresent, StateChecking, StateMissing:
		return state, true
	default:
		return "", false
	}
}
