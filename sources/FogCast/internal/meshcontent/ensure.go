package meshcontent

import (
	"context"
	"errors"
	"time"
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
	// ErrLeaseNotFree refuses Ensure before any pull when the session
	// is not an owned binding or the node is already in use.
	ErrLeaseNotFree = errors.New("mesh ensure refused: session is not owned or the node is in use")
	// ErrCheckingTimeout is the host deadline for a slot that stayed
	// Checking. It is not ErrExecuteBlocked and it is not a down target.
	ErrCheckingTimeout = errors.New("mesh content stayed checking until the host timeout")
	// ErrContentPullFailed is a copy that was advertised and still did
	// not leave the content-id Present. It is not the no-source class
	// and it is not a down target. Callers must not treat partial bytes
	// as Present.
	ErrContentPullFailed = errors.New("mesh content pull failed")
	// ErrContentLinkFailed is a link that did not record the expansion
	// slot. It is not a failed copy.
	ErrContentLinkFailed = errors.New("mesh content link failed")
	// ErrContentUnreachable is a slot or source read that failed in
	// transport. It is not StateMissing and it is not a missing source.
	ErrContentUnreachable = errors.New("mesh content could not be read")
	errSlotState          = errors.New("meshcontent: executor slot state is invalid")
)

// LeaseDeniedError is a kit lease rejection of a mesh pull or link.
// Code is the kit status (KIT_LEASE_REQUIRED, KIT_LEASE_BUSY, and the
// rest). Host launch maps it to KIT_LEASE_DENIED. It is not a failed copy.
type LeaseDeniedError struct {
	Code string
}

func (e *LeaseDeniedError) Error() string {
	if e == nil || e.Code == "" {
		return "kit lease denied"
	}
	return "kit lease denied: " + e.Code
}

// MaxCheckingTimeout is the upper bound on a Checking wait. Callers
// that pass a longer EnsureOption.CheckingTimeout are clamped.
const MaxCheckingTimeout = 2 * time.Minute

const checkingPollInterval = 20 * time.Millisecond

// EnsureOption is the launch admission Ensure enforces itself.
// LeaseFree and InUse are the caller's session facts. Ensure does not
// discover them. CheckingTimeout bounds a slot that is already
// Checking; zero does not wait.
type EnsureOption struct {
	LeaseFree       bool
	InUse           bool
	CheckingTimeout time.Duration
}

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
// Ensure does not pick another node and does not release or change a
// lease. Pull copies bytes onto this executor. The kit content store
// on the target agent implements this for the bound node. Tests may
// pass a fake.
//
// LinkExpansion is idempotent for the same expansion name and
// content-id: repeating that pair leaves one link. Ensure does not
// roll back links that already succeeded when a later LinkExpansion
// returns an error. A caller must not program the FPGA while any
// required slot is Checking, and must not link until every required
// sibling is Present and the package ABI is eligible.
type Executor interface {
	// NodeID is this executor. Ensure refuses the call when it is not
	// the session's bound node.
	NodeID() string
	// Slot reports id on this executor: Present, Checking, or Missing.
	// Checking means a pull is already in progress. Partial bytes are
	// not Present.
	Slot(id ContentID) SlotState
	// SourceAdvertises reports whether some content source can serve id.
	// The source may be another node. The pull still lands on this one.
	SourceAdvertises(id ContentID) bool
	// Pull copies id from a source onto this executor.
	// Checking means the copy is still running. Present means it finished.
	// A canceled or failed pull must not leave a new pull running and
	// must not leave partial bytes visible as Present.
	Pull(ctx context.Context, id ContentID) (SlotState, error)
	// LinkExpansion links one expansion's slot bytes on this executor.
	// id is that slot's content-id. The host does not pre-link those
	// bytes with primary media or with any other slot. The same name
	// and content-id may be linked again; that second call is a no-op
	// success. Ensure does not remove an earlier link if a later one fails.
	LinkExpansion(name string, id ContentID) error
	// EligibleABIs are the package ABI id and major pairs this executor
	// can run. An empty list is not eligibility.
	EligibleABIs() []EligibleABI
}

// SlotFact is one content-id on an executor. Advertises is meaningful
// when State is Missing: some source can serve the bytes. A snapshot
// or read error is a failed read, not Missing.
type SlotFact struct {
	State      SlotState
	Advertises bool
}

// ContentSnapshot reads many content-ids in one call. ReadyHere uses it
// so a kit client does not issue one HTTP request per title. A successful
// snapshot may be reused for a short time by the client. Ensure does not
// use that cache.
type ContentSnapshot interface {
	Snapshot(ctx context.Context, ids []ContentID) (map[string]SlotFact, error)
}

// SlotReader reports one id. A transport failure returns an error and
// is not StateMissing.
type SlotReader interface {
	ReadSlot(ctx context.Context, id ContentID) (SlotState, error)
}

// SourceReader reports whether a source advertises id. A transport
// failure returns an error and is not "no source".
type SourceReader interface {
	ReadSource(ctx context.Context, id ContentID) (bool, error)
}

// MaxSlotBatch is the most content-ids one snapshot read accepts.
const MaxSlotBatch = 128

// PackageHolder lists described package ids present on this executor.
// ReadyHere uses the list for package-backed entries. An executor that
// does not implement PackageHolder has no package ids, so a
// package-backed title is not Ready here. This is not a pull.
type PackageHolder interface {
	Packages() []string
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
// again. LinkExpansion runs only when every required slot is Present
// and, for a package-backed entry, the executor ABI is eligible. A
// sibling that is Checking or Missing is not linked. A required id
// that is missing and has no source returns ErrContentMissingNoSource
// before any pull. A pull that finishes Missing returns
// ErrContentPullFailed. A canceled context or a lease that is not
// free returns before any pull. Ensure does not program the FPGA.
func Ensure(ctx context.Context, entry Entry, boundNode string, exec Executor, opt EnsureOption) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := entry.Validate(); err != nil {
		return Result{}, err
	}
	if exec == nil || boundNode == "" || exec.NodeID() != boundNode {
		return Result{}, ErrUnboundNode
	}
	if !opt.LeaseFree || opt.InUse {
		return Result{}, ErrLeaseNotFree
	}
	if !entry.Launchable {
		return Result{Node: boundNode, Execute: false, Block: BlockBrowseOnly}, nil
	}

	plans := make([]ensurePlan, 0, len(entry.Slots))
	for _, slot := range entry.Slots {
		if slot.Content == nil {
			continue
		}
		state, err := readSlot(ctx, exec, *slot.Content)
		if err != nil {
			return Result{}, err
		}
		plan := ensurePlan{slot: slot, state: state}
		if state == StateMissing {
			advertises, err := readSource(ctx, exec, *slot.Content)
			if err != nil {
				return Result{}, err
			}
			if !advertises {
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
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		id := *plans[i].slot.Content
		next, err := exec.Pull(ctx, id)
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
			return Result{}, ErrContentPullFailed
		}
		plans[i].state = state
	}
	if err := settleChecking(ctx, exec, plans, opt.CheckingTimeout); err != nil {
		return Result{}, err
	}
	slots := make([]SlotStatus, 0, len(plans))
	sawChecking := false
	allPresent := true
	for _, plan := range plans {
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
	// Link only when execute is allowed: every required slot is Present
	// and the ABI is eligible. A missing or Checking sibling, and an
	// ineligible ABI, must not observe a link. LinkExpansion is
	// idempotent for one name and content-id. A mid-loop failure leaves
	// earlier links in place; Ensure does not roll them back.
	if !result.Execute {
		return result, nil
	}
	for _, plan := range plans {
		if plan.slot.Kind != SlotExpansion {
			continue
		}
		if err := exec.LinkExpansion(plan.slot.Name, *plan.slot.Content); err != nil {
			return Result{}, err
		}
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

func settleChecking(ctx context.Context, exec Executor, plans []ensurePlan, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	if timeout > MaxCheckingTimeout {
		timeout = MaxCheckingTimeout
	}
	deadline := time.Now().Add(timeout)
	for i := range plans {
		if plans[i].state != StateChecking || plans[i].slot.Content == nil {
			continue
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ErrCheckingTimeout
		}
		state, err := waitChecking(ctx, exec, *plans[i].slot.Content, remaining)
		if err != nil {
			return err
		}
		plans[i].state = state
	}
	return nil
}

func waitChecking(ctx context.Context, exec Executor, id ContentID, timeout time.Duration) (SlotState, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(checkingPollInterval)
	defer ticker.Stop()
	for {
		state, err := readSlot(ctx, exec, id)
		if err != nil {
			return "", err
		}
		if state != StateChecking {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", ErrCheckingTimeout
		case <-ticker.C:
		}
	}
}

func normalizeState(state SlotState) (SlotState, bool) {
	switch state {
	case StatePresent, StateChecking, StateMissing:
		return state, true
	default:
		return "", false
	}
}

func readSlot(ctx context.Context, exec Executor, id ContentID) (SlotState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if reader, ok := exec.(SlotReader); ok && reader != nil {
		state, err := reader.ReadSlot(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", err
		}
		normalized, ok := normalizeState(state)
		if !ok {
			return "", errSlotState
		}
		return normalized, nil
	}
	state, ok := normalizeState(exec.Slot(id))
	if !ok {
		return "", errSlotState
	}
	return state, nil
}

func readSource(ctx context.Context, exec Executor, id ContentID) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if reader, ok := exec.(SourceReader); ok && reader != nil {
		advertises, err := reader.ReadSource(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, err
		}
		return advertises, nil
	}
	return exec.SourceAdvertises(id), nil
}
