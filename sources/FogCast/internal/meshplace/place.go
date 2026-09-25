// Package meshplace chooses where one title plays.
//
// Place is host-local. It implements the unsigned Decision 7 strawman
// in docs/mesh-lan.md as read by docs/mesh-phase3.md. Deano has not
// locked that order. This package does not describe the order as a
// lock, does not store a preference, and does not name a default
// native_emu winner.
//
// The caller passes the projected catalog entry and the candidate nodes
// it already has. FPGA eligibility uses abis (id, major) from each
// node's GET /v1/mesh/content/node. Place does not perform that read,
// does not browse DNS-SD, and does not treat an empty discovery family
// list as "any RBF". Address, human name, and candidate order are not
// ranking keys.
//
// A selected result names Execute, and names DisplaySink and
// InputSource only when that same node advertises them. Picture and
// pad stay on that node. Unresolved and fail closed do not name an
// Execute node.
//
// OverrideNodeID is optional. Empty means unset and leaves automatic
// placement unchanged, including when several native_emu nodes can
// run the title. A set id selects that candidate when it can already
// run the title. A name that cannot run the title, fails the mesh
// major, or is not a candidate does not win, and Place does not
// substitute a different node.
package meshplace

import "github.com/DeanoC/FogCast/internal/meshcontent"

// Outcome is the host-local result of one placement. It is not a wire
// value and it is not sofa copy.
type Outcome string

const (
	// OutcomeSelected names one Execute node.
	OutcomeSelected Outcome = "selected"
	// OutcomeUnresolved means Place will not name an Execute node.
	// Several eligible nodes and no tie-break is one case. A set
	// override that does not name an eligible candidate is another:
	// Place does not substitute a different node.
	OutcomeUnresolved Outcome = "unresolved"
	// OutcomeFailClosed means do not launch.
	OutcomeFailClosed Outcome = "fail_closed"
)

// Reason classifies OutcomeFailClosed. Empty unless the outcome is
// fail closed. Codes are host-local.
type Reason string

const (
	// ReasonNotLaunchable means the entry is browse-only or invalid.
	ReasonNotLaunchable Reason = "not_launchable"
	// ReasonNoCandidate means no candidate can run the title.
	ReasonNoCandidate Reason = "no_candidate"
	// ReasonMeshMajor means every candidate that could run the title
	// has a mesh-major mismatch.
	ReasonMeshMajor Reason = "mesh_major"
	// ReasonMissingSlot means the caller reported a required
	// composition slot with no source.
	ReasonMissingSlot Reason = "missing_slot"
)

// Candidate is one node the caller already knows.
//
// Execute lists advertised kinds. ABIs are the id and major pairs from
// that node's content document, the shape stored after
// GET /v1/mesh/content/node. An empty ABI list does not match a
// package. DisplaySink and InputSource are advertisements, not a
// claim that the picture is up.
type Candidate struct {
	NodeID      string
	MeshMajorOK bool
	Execute     []string
	DisplaySink bool
	InputSource bool
	ABIs        []meshcontent.EligibleABI
}

// Options are caller facts Place does not discover.
//
// DisplayPreference, LastDisplaySink, and OverrideNodeID are node ids.
// Empty means unset. MissingRequiredSlot is a required composition
// slot with no source. Place does not open files, does not pull, and
// does not read a config file.
//
// OverrideNodeID selects that candidate when it can already run the
// title. Empty does not choose a winner.
type Options struct {
	DisplayPreference   string
	LastDisplaySink     string
	MissingRequiredSlot bool
	OverrideNodeID      string
}

// Choice names the selected node. DisplaySink and InputSource are set
// only when that same node advertises them. They are never a different
// node. The zero Choice means no Execute node.
type Choice struct {
	Execute     string
	DisplaySink string
	InputSource string
}

// Result is host-local. It has no JSON encoding of its own.
type Result struct {
	Outcome Outcome
	Reason  Reason
	Choice  Choice
}

// Place chooses Execute for one entry.
//
// One eligible fpga_native kit is selected. When several eligible kits
// exist, one of them is selected only when the household display
// preference, or otherwise the last play DisplaySink, names one that
// advertises DisplaySink and whose mesh major matches. Otherwise the
// FPGA result is unresolved. Eligible for that count means Execute
// fpga_native and meshcontent.ABIMatches against the caller's abis. A
// mesh-major mismatch does not rank those kits.
//
// native_emu is considered only when no FPGA candidate can run the
// title. Exactly one such candidate is selected. Several are
// unresolved. Preference, last sink, and an empty override do not
// pick among them.
//
// A non-empty OverrideNodeID selects that node when it is already
// one of those candidates and its mesh major matches. For
// fpga_native that includes meshcontent.ABIMatches against the
// node's abis. DisplaySink is not required. DisplaySink and
// InputSource are still copied only when that node advertises them.
// When the id is set and is not such a candidate, the result is fail
// closed or unresolved and does not name a different Execute node.
func Place(entry meshcontent.Entry, candidates []Candidate, opts Options) Result {
	if err := entry.Validate(); err != nil || !entry.Launchable {
		return fail(ReasonNotLaunchable)
	}
	if opts.MissingRequiredSlot {
		return fail(ReasonMissingSlot)
	}
	fpga := runnableFPGA(entry, candidates)
	if opts.OverrideNodeID != "" {
		return placeOverride(entry, fpga, candidates, opts.OverrideNodeID)
	}
	if len(fpga) > 0 {
		return finishFPGA(fpga, opts)
	}
	if entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu {
		return fail(ReasonNoCandidate)
	}
	return finishNative(runnableNative(candidates))
}

// placeOverride selects nodeID when it can already run the title.
// A miss does not fall through to preference, last sink, or a
// single other node.
func placeOverride(entry meshcontent.Entry, fpga []Candidate, candidates []Candidate, nodeID string) Result {
	could := fpga
	if len(could) == 0 {
		if entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu {
			return fail(ReasonNoCandidate)
		}
		could = runnableNative(candidates)
	}
	rows := oneRowPerNode(could)
	if len(rows) == 0 {
		return fail(ReasonNoCandidate)
	}
	if !anyMeshMajorOK(rows) {
		return fail(ReasonMeshMajor)
	}
	for _, candidate := range rows {
		if candidate.NodeID == nodeID && candidate.MeshMajorOK {
			return selected(candidate)
		}
	}
	return Result{Outcome: OutcomeUnresolved}
}

func finishFPGA(could []Candidate, opts Options) Result {
	rows, done, stop := gate(could)
	if stop {
		return done
	}
	if c, ok := preferred(rows, opts.DisplayPreference); ok {
		return selected(c)
	}
	if c, ok := preferred(rows, opts.LastDisplaySink); ok {
		return selected(c)
	}
	return Result{Outcome: OutcomeUnresolved}
}

func finishNative(could []Candidate) Result {
	_, done, stop := gate(could)
	if stop {
		return done
	}
	// Several native_emu nodes stay unresolved. Preference, last sink,
	// an empty override, and mesh-major OK are not a tie-break. That
	// choice is parked.
	return Result{Outcome: OutcomeUnresolved}
}

// gate applies the shared fail-closed checks. stop is true when the
// result is already final: no row, every row mesh-major mismatched, or
// exactly one selectable node. Several rows that include at least one
// mesh-major match return stop false so the caller can apply its own
// tie-break. A mismatched peer stays in rows. It is not deleted to
// manufacture a single winner.
func gate(could []Candidate) ([]Candidate, Result, bool) {
	rows := oneRowPerNode(could)
	if len(rows) == 0 {
		return nil, fail(ReasonNoCandidate), true
	}
	if !anyMeshMajorOK(rows) {
		return nil, fail(ReasonMeshMajor), true
	}
	if len(rows) == 1 {
		return nil, selected(rows[0]), true
	}
	return rows, Result{}, false
}

func runnableFPGA(entry meshcontent.Entry, candidates []Candidate) []Candidate {
	if entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
		return nil
	}
	pkg, ok := entryPackage(entry)
	if !ok {
		return nil
	}
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.NodeID == "" || !hasKind(candidate, meshcontent.ExecuteFPGANative) {
			continue
		}
		if !meshcontent.ABIMatches(pkg, candidate.ABIs) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func runnableNative(candidates []Candidate) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.NodeID == "" || !hasKind(candidate, meshcontent.ExecuteNativeEmu) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// preferred reports the named node when it is one of the eligible rows,
// advertises DisplaySink, and matches the mesh major. An empty id is
// unset. A menu shell that cannot run the title is not in rows, so it
// cannot win.
func preferred(rows []Candidate, nodeID string) (Candidate, bool) {
	if nodeID == "" {
		return Candidate{}, false
	}
	for _, candidate := range rows {
		if candidate.NodeID != nodeID || !candidate.MeshMajorOK || !candidate.DisplaySink {
			continue
		}
		return candidate, true
	}
	return Candidate{}, false
}

func selected(candidate Candidate) Result {
	choice := Choice{Execute: candidate.NodeID}
	if candidate.DisplaySink {
		choice.DisplaySink = candidate.NodeID
	}
	if candidate.InputSource {
		choice.InputSource = candidate.NodeID
	}
	return Result{Outcome: OutcomeSelected, Choice: choice}
}

func fail(reason Reason) Result {
	return Result{Outcome: OutcomeFailClosed, Reason: reason}
}

func anyMeshMajorOK(rows []Candidate) bool {
	for _, candidate := range rows {
		if candidate.MeshMajorOK {
			return true
		}
	}
	return false
}

func hasKind(candidate Candidate, kind string) bool {
	for _, got := range candidate.Execute {
		if got == kind {
			return true
		}
	}
	return false
}

func entryPackage(entry meshcontent.Entry) (meshcontent.PackageABI, bool) {
	for _, slot := range entry.Slots {
		if slot.Kind == meshcontent.SlotPackageABI && slot.Package != nil {
			return *slot.Package, true
		}
	}
	return meshcontent.PackageABI{}, false
}

// oneRowPerNode keeps the first row for each node id. Callers pass one
// record per node. An empty id is not a node. Order is preserved and
// is not a rank.
func oneRowPerNode(in []Candidate) []Candidate {
	seen := make(map[string]struct{}, len(in))
	out := make([]Candidate, 0, len(in))
	for _, candidate := range in {
		if candidate.NodeID == "" {
			continue
		}
		if _, ok := seen[candidate.NodeID]; ok {
			continue
		}
		seen[candidate.NodeID] = struct{}{}
		out = append(out, candidate)
	}
	return out
}
