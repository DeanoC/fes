package rooms

import (
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/libraryuser"
)

// Availability is the play-availability family rooms, tenfoot, and the
// browser show. Play uses Checking, Ready, Needs a choice, and
// Unavailable. Missing is the Unavailable reason when no title matches;
// Confirm still opens the library.
type Availability string

const (
	AvailChecking    Availability = "checking"
	AvailMissing     Availability = "missing"
	AvailNeedsChoice Availability = "needs_choice"
	AvailUnavailable Availability = "unavailable"
	AvailReady       Availability = "ready"
)

// Label is the four-state name every surface shows. Missing is Unavailable.
func (a Availability) Label() string {
	switch a {
	case AvailReady:
		return "Ready"
	case AvailNeedsChoice:
		return "Needs a choice"
	case AvailUnavailable, AvailMissing:
		return "Unavailable"
	default:
		return "Checking"
	}
}

// ChoiceKind is why Needs a choice opened. Fail-closed rows never use it.
type ChoiceKind string

const (
	ChoiceNone    ChoiceKind = ""
	ChoiceEdition ChoiceKind = "edition"
	ChoiceBackend ChoiceKind = "backend"
)

// Kind is the selected location the compact info panel identifies.
type Kind string

const (
	KindGame       Kind = "game"
	KindRoom       Kind = "room"
	KindLibrary    Kind = "library"
	KindAction     Kind = "action"
	KindCore       Kind = "core"
	KindUnresolved Kind = "unresolved"
)

// ValidPackageID reports whether id is a lowercase SHA-256 hex package id,
// the same rule the local-control socket accepts.
func ValidPackageID(id string) bool {
	return localcores.SHA256Hex(id)
}

// validCoreID reports a safe manifest core id. Callers allow an empty id;
// a present id must match the room-id token rule.
func validCoreID(id string) bool {
	return id != "" && roomIDPattern.MatchString(id)
}

// launcherActions is the fixed set of launcher-internal operations a room
// may name. Ids are not commands: there is no shell, path, network, or
// parameter. Rooms cannot extend this set.
var launcherActions = map[string]struct{}{
	"settings": {},
}

// LauncherActionAllowed reports whether id is one of those operations.
func LauncherActionAllowed(id string) bool {
	_, ok := launcherActions[strings.TrimSpace(id)]
	return ok
}

// ConfirmIntent is what Confirm must do so it never silently no-ops.
type ConfirmIntent int

const (
	ConfirmNone ConfirmIntent = iota
	ConfirmWait
	ConfirmOpenLibrary
	ConfirmChoose
	ConfirmExplain
	ConfirmImportFirmware
	ConfirmLaunch
	ConfirmEnterRoom
	ConfirmOpenLibraryBrowse
	ConfirmLauncherAction
	ConfirmLaunchCore
	// ConfirmLaunchKit plays a present local cartridge through the kit
	// socket. It is not the host session launch.
	ConfirmLaunchKit
)

// Destination is the selected location a room publishes to the launcher.
type Destination struct {
	Kind   Kind
	Label  string
	System string
	GameID string
	RoomID string
	// LauncherAction is the allowlisted operation id when Kind is KindAction.
	// It is not the Action copy line.
	LauncherAction string
	// PackageID and CoreID identify an installed core when Kind is KindCore.
	// CoreLaunchable is false when the socket would refuse the package.
	// CoreBlock is that refusal, verbatim ("Needs a cartridge", "Needs firmware").
	PackageID      string
	CoreID         string
	CoreLaunchable bool
	CoreBlock      string
	Availability   Availability
	Status         string
	Action         string
	Note           string
	NoteBy         string
	Query          string
	Platform       string
	Matches        []hostclient.Game
	History        History
	// LeaseHeld is a foreign kit lease. The same shell's Soft-stop retained
	// grant leaves this false so that shell stays Ready.
	LeaseHeld bool
	// KitDirect is a present local cartridge the kit plays through the
	// local-control socket. Confirm returns ConfirmLaunchKit and does not
	// post the host session. Host-eligible rows leave this false.
	KitDirect bool
	// ReadyBlock and NextAction are ReadyHere when the mesh seam is on.
	// Empty when that seam is off.
	ReadyBlock string
	NextAction string
	// Choice is edition or backend when Availability is Needs a choice.
	Choice ChoiceKind
}

// ClassifyGames maps a library result set onto Checking, Missing, Needs a
// choice, Unavailable, or Ready. The caller reports Checking while the
// query is still in flight. query, when set, filters titles; an exact
// title match wins over looser contains-matches.
// Ready is Phase 0 composition when the game has no ReadyHere result.
// When ReadyHere is set, that result is Ready: a false result is
// Unavailable, and a slot mid-pull (ensure_in_progress) is Checking.
// An empty or unknown ready block is Unavailable, not Ready.
// A selected placement leaves that Ready path in place, so Play does not
// ask which machine.
// Unresolved and fail closed are not Ready. A mesh Execute
// advertisement is not an input and cannot change the result.
// Needs a choice is only the viable play options: two Ready backends or
// two Ready editions. One viable option is Ready with no prompt.
// Skew, in use, not installed, and other fail-closed rows are
// Unavailable with a short reason. They are never a choice.
func ClassifyGames(games []hostclient.Game, query string) (Availability, []hostclient.Game) {
	matches := matchingGames(games, query)
	if len(matches) == 0 {
		return AvailMissing, nil
	}
	if exact := exactTitleMatches(matches, query); len(exact) > 0 {
		matches = exact
	}
	viable, blocked, checking := partitionPlayOptions(matches)
	if len(viable) > 1 {
		return AvailNeedsChoice, viable
	}
	if len(viable) == 1 {
		return AvailReady, viable
	}
	if checking {
		return AvailChecking, matches
	}
	if len(blocked) == 0 {
		return AvailMissing, nil
	}
	return AvailUnavailable, blocked
}

func partitionPlayOptions(matches []hostclient.Game) (viable, blocked []hostclient.Game, checking bool) {
	for _, game := range matches {
		if game.ReadyHere != nil && !*game.ReadyHere && game.LaunchBlock() == hostclient.LaunchEnsureProgress {
			checking = true
			continue
		}
		if game.LaunchEligible() {
			viable = append(viable, game)
			continue
		}
		blocked = append(blocked, game)
	}
	return viable, blocked, checking
}

// PlayChoiceKind is backend when the viable rows use more than one
// execute kind (FPGA versus emulator). Same-kind rows are editions.
func PlayChoiceKind(matches []hostclient.Game) ChoiceKind {
	if len(matches) < 2 {
		return ChoiceNone
	}
	seen := ""
	for _, game := range matches {
		exec := playExecution(game)
		if exec == "" {
			continue
		}
		if seen == "" {
			seen = exec
			continue
		}
		if exec != seen {
			return ChoiceBackend
		}
	}
	return ChoiceEdition
}

func playExecution(game hostclient.Game) string {
	exec := strings.TrimSpace(game.Execution)
	switch exec {
	case hostclient.ExecutionHostOnly, "native_emu":
		return "native_emu"
	case "fpga_native", "fpga_development":
		return "fpga_native"
	default:
		return exec
	}
}

// BackendLabel is the sofa name for one play option.
func BackendLabel(game hostclient.Game) string {
	switch playExecution(game) {
	case "native_emu":
		return "Emulator"
	case "fpga_native":
		return "FPGA"
	default:
		sys := strings.TrimSpace(game.System)
		if sys == "" {
			return game.Title
		}
		return strings.ToUpper(sys)
	}
}

// ApplyForeignLease applies a foreign kit lease to each launch-eligible
// candidate before viable choices are counted. foreign is false for the
// shell that holds the grant, including after Soft-stop. A host-only
// candidate stays viable, so an emulator beside a leased FPGA is the sole
// Ready option. A lone leased option is Unavailable with the in-use reason.
// Confirm then explains and does not launch or take the lease. An existing
// catalog block (firmware, skew) is left as that block. A core destination
// is left to tenfoot, which applies the local in-use copy and refuses
// Confirm. An Execute advertisement is not an input.
func ApplyForeignLease(d Destination, foreign bool) Destination {
	if !foreign || d.Kind == KindRoom || d.Kind == KindLibrary || d.Kind == KindAction || d.Kind == KindCore {
		return d
	}
	if len(d.Matches) == 0 {
		return d
	}
	next := make([]hostclient.Game, len(d.Matches))
	changed := false
	for i, g := range d.Matches {
		next[i] = g
		if g.HostOnly() || !g.LaunchEligible() {
			continue
		}
		next[i] = leaseHeldCandidate(g)
		changed = true
	}
	if !changed {
		return d
	}
	state, matches := ClassifyGames(next, d.Query)
	d.Availability = state
	d.Matches = matches
	d.Choice = ChoiceNone
	d.LeaseHeld = false
	d.ReadyBlock = ""
	d.NextAction = ""
	if state == AvailReady && len(matches) == 1 {
		d.GameID = matches[0].ID
		if title := strings.TrimSpace(matches[0].Title); title != "" {
			d.Label = title
		}
		if system := strings.TrimSpace(matches[0].System); system != "" {
			d.System = system
		}
	}
	d.applyMeshFacts()
	if state == AvailUnavailable && allLeaseHeld(matches) {
		d.LeaseHeld = true
		if len(matches) == 1 {
			d.ReadyBlock = matches[0].ReadyBlock
			d.NextAction = matches[0].NextAction
		}
	}
	d.FillCopy()
	d.FillHistory()
	return d
}

func leaseHeldCandidate(g hostclient.Game) hostclient.Game {
	ready := false
	g.ReadyHere = &ready
	g.ReadyBlock = string(hostclient.LaunchLeaseHeld)
	g.NextAction = "wait_for_lease"
	return g
}

func allLeaseHeld(matches []hostclient.Game) bool {
	if len(matches) == 0 {
		return false
	}
	for _, g := range matches {
		if g.LaunchBlock() != hostclient.LaunchLeaseHeld {
			return false
		}
	}
	return true
}

func matchingGames(games []hostclient.Game, query string) []hostclient.Game {
	q := normalizeTitle(query)
	out := make([]hostclient.Game, 0, len(games))
	for _, g := range games {
		if q == "" || strings.Contains(normalizeTitle(g.Title), q) {
			out = append(out, g)
		}
	}
	return out
}

func exactTitleMatches(games []hostclient.Game, query string) []hostclient.Game {
	q := normalizeTitle(query)
	if q == "" {
		return nil
	}
	var out []hostclient.Game
	for _, g := range games {
		if normalizeTitle(g.Title) == q {
			out = append(out, g)
		}
	}
	return out
}

func normalizeTitle(s string) string {
	return libraryuser.CanonicalEditionQuery(s)
}

// DestinationPreferenceKey is the household edition-preference key for a
// published location (query, else label, plus platform).
func DestinationPreferenceKey(d Destination) string {
	q := strings.TrimSpace(d.Query)
	if q == "" {
		q = strings.TrimSpace(d.Label)
	}
	return libraryuser.EditionKey(q, d.Platform)
}

// ApplyEditionPreference collapses Needs a choice onto the saved edition when
// that game is still in the current match set. Unknown or stale IDs leave the
// destination unchanged so Confirm still forces a choice.
func ApplyEditionPreference(d Destination, gameID string) Destination {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" || d.Availability != AvailNeedsChoice {
		return d
	}
	for _, g := range d.Matches {
		if g.ID != gameID {
			continue
		}
		d.GameID = g.ID
		d.Matches = []hostclient.Game{g}
		d.Label = g.Title
		d.System = g.System
		state, picked := ClassifyGames([]hostclient.Game{g}, "")
		d.Availability = state
		d.Choice = ChoiceNone
		if len(picked) == 1 {
			d.Matches = picked
		}
		d.FillCopy()
		d.FillHistory()
		return d
	}
	return d
}

// applyMeshFacts copies ReadyHere onto the destination and keeps a
// not-ready title from staying Ready. Phase 0 games leave this unchanged.
func (d *Destination) applyMeshFacts() {
	if d == nil {
		return
	}
	game, ok := d.Game()
	if !ok || game.ReadyHere == nil {
		return
	}
	d.ReadyBlock = game.ReadyBlock
	d.NextAction = game.NextAction
	if *game.ReadyHere {
		return
	}
	if game.ReadyBlock == string(hostclient.LaunchLeaseHeld) {
		d.LeaseHeld = true
	}
	if d.Availability != AvailReady && d.Availability != "" {
		return
	}
	if game.LaunchBlock() == hostclient.LaunchEnsureProgress {
		d.Availability = AvailChecking
		return
	}
	d.Availability = AvailUnavailable
}

const (
	// InUseStatus is the short label when someone else holds this machine.
	InUseStatus = "In use"
	// InUseDetail is the action line under InUseStatus.
	InUseDetail = "Someone else is playing on this machine. You can play when they're done."
	// CheckingStatus is the sofa sentence while play is still unresolved.
	CheckingStatus = "Still resolving whether this title can play here."
	// CheckingAction is Confirm on Checking: wait, never launch.
	CheckingAction = "Wait. Do not launch."
	// EditionChoiceStatus is Needs a choice among playable editions.
	EditionChoiceStatus = "Several editions match. Choose one."
	// EditionChoiceAction is Confirm on an edition choice.
	EditionChoiceAction = "Choose an edition."
	// BackendChoiceStatus is Needs a choice among playable backends.
	BackendChoiceStatus = "This title can play in more than one way. Choose one."
	// BackendChoiceAction is Confirm on a backend choice.
	BackendChoiceAction = "Choose how to play."
)

// InUseLine is the single-line form of the in-use copy.
func InUseLine() string { return InUseStatus + ". " + InUseDetail }

func meshUnavailableCopy(block string) (status, action string, ok bool) {
	switch hostclient.LaunchBlock(block) {
	case hostclient.LaunchDistant:
		return "This title is not on this machine.", "Bring it here before Play.", true
	case hostclient.LaunchVersionSkew:
		return "Can't play here yet.", "Do not launch.", true
	case hostclient.LaunchContentMissing:
		return "A required part of this title is missing.", "Supply the missing part.", true
	case hostclient.LaunchNoExecutor, hostclient.LaunchMeshInvalid:
		return "This title cannot play on the current setup.", "See why this title cannot play.", true
	default:
		return "", "", false
	}
}

// FillCopy sets distinct Status and Action strings for the compact panel.
func (d *Destination) FillCopy() {
	if d == nil {
		return
	}
	d.applyMeshFacts()
	if d.Availability != AvailNeedsChoice {
		d.Choice = ChoiceNone
	}
	switch d.Kind {
	case KindRoom:
		d.Status = "Enter room."
		d.Action = "Enter room."
		return
	case KindLibrary:
		d.Status = "Browse the full library."
		d.Action = "Open library."
		return
	case KindAction:
		switch d.LauncherAction {
		case "settings":
			d.Status = "Open settings."
			d.Action = "Open settings."
		default:
			d.Status = "This action is not available."
			d.Action = "This action is not available."
		}
		return
	case KindCore:
		if d.CoreLaunchable {
			d.Status = "Ready to play."
			d.Action = "Play"
			return
		}
		block := strings.TrimSpace(d.CoreBlock)
		if block == "" {
			block = "This core cannot launch."
		}
		d.Status = block
		d.Action = block
		return
	case KindUnresolved:
		if d.Availability == "" {
			d.Status = "Choose a title from this location."
			d.Action = "Open the title list."
			return
		}
	}
	switch d.Availability {
	case AvailChecking:
		d.Status = CheckingStatus
		d.Action = CheckingAction
	case AvailMissing:
		label := strings.TrimSpace(d.Query)
		if label == "" {
			label = strings.TrimSpace(d.Label)
		}
		if label != "" {
			d.Status = "Not in this household's library (" + label + ")."
		} else {
			d.Status = "Not in this household's library."
		}
		d.Action = "Open the library to add it."
	case AvailNeedsChoice:
		d.Choice = PlayChoiceKind(d.Matches)
		if d.Choice == ChoiceBackend {
			d.Status = BackendChoiceStatus
			d.Action = BackendChoiceAction
			break
		}
		d.Choice = ChoiceEdition
		d.Status = EditionChoiceStatus
		d.Action = EditionChoiceAction
	case AvailUnavailable:
		if d.LeaseHeld || d.ReadyBlock == string(hostclient.LaunchLeaseHeld) {
			d.Status = InUseStatus
			d.Action = InUseDetail
			break
		}
		if status, action, ok := meshUnavailableCopy(d.ReadyBlock); ok {
			d.Status = status
			d.Action = action
			break
		}
		d.Status = "This title cannot play on the current setup."
		d.Action = "See why this title cannot play."
		if len(d.Matches) > 0 {
			if reason := LaunchBlockCopy(d.Matches[0]); reason != "" {
				d.Status = reason
			}
			if d.Matches[0].LaunchBlock() == hostclient.LaunchMissingFirmware {
				d.Action = "Import Coleco BIOS."
			}
		}
	case AvailReady:
		d.Status = "Ready to play."
		d.Action = "Play"
	default:
		d.Status = CheckingStatus
		d.Action = CheckingAction
	}
}

// Confirm reports the required Confirm behaviour for this destination.
func (d Destination) Confirm() ConfirmIntent {
	switch d.Kind {
	case KindRoom:
		return ConfirmEnterRoom
	case KindLibrary:
		return ConfirmOpenLibraryBrowse
	case KindAction:
		return ConfirmLauncherAction
	case KindCore:
		if d.LeaseHeld {
			return ConfirmExplain
		}
		if d.CoreLaunchable {
			return ConfirmLaunchCore
		}
		return ConfirmExplain
	}
	switch d.Availability {
	case AvailChecking:
		return ConfirmWait
	case AvailMissing:
		return ConfirmOpenLibrary
	case AvailNeedsChoice:
		return ConfirmChoose
	case AvailUnavailable:
		if d.LeaseHeld {
			return ConfirmExplain
		}
		if len(d.Matches) > 0 && d.Matches[0].LaunchBlock() == hostclient.LaunchMissingFirmware {
			return ConfirmImportFirmware
		}
		return ConfirmExplain
	case AvailReady:
		// A selected placement stays on this launch. Confirm does not
		// ask which machine. A kit-direct cartridge never posts the host.
		if d.KitDirect {
			return ConfirmLaunchKit
		}
		return ConfirmLaunch
	default:
		if d.Kind == KindUnresolved && d.Availability != "" {
			return ConfirmWait
		}
		return ConfirmNone
	}
}

// LaunchBlockCopy is sofa copy for a catalog-side launch block.
func LaunchBlockCopy(game hostclient.Game) string {
	switch game.LaunchBlock() {
	case hostclient.LaunchBrowseOnly:
		return "This platform is browse-only on this host."
	case hostclient.LaunchSourceOffline:
		return "This game's source is offline."
	case hostclient.LaunchUnreadable:
		return "This ROM can't be read."
	case hostclient.LaunchNotReady:
		return "This game isn't ready to launch."
	case hostclient.LaunchMissingFirmware:
		return "Coleco BIOS required. Import household firmware before Play."
	case hostclient.LaunchMissingROM:
		return "Needs a cartridge"
	case hostclient.LaunchDistant:
		return "This title is not on this machine."
	case hostclient.LaunchLeaseHeld:
		return InUseStatus
	case hostclient.LaunchVersionSkew:
		return "Can't play here yet."
	case hostclient.LaunchContentMissing:
		return "A required part of this title is missing."
	case hostclient.LaunchNoExecutor, hostclient.LaunchMeshInvalid:
		return "This title cannot play on the current setup."
	case hostclient.LaunchEnsureProgress:
		return CheckingStatus
	case hostclient.LaunchPlacementUnresolved, hostclient.LaunchPlacementFailClosed:
		return "This title cannot play on the current setup."
	case "":
		return ""
	default:
		return "This game isn't ready to launch."
	}
}

// CuratorAttribution is the Details-panel credit for a room-authored note.
func (d Destination) CuratorAttribution() string {
	note := strings.TrimSpace(d.Note)
	if note == "" {
		return ""
	}
	by := strings.TrimSpace(d.NoteBy)
	if by == "" {
		by = "this room"
	}
	return "Note from " + by
}

// Game returns the single matched catalog row when one is selected.
func (d Destination) Game() (hostclient.Game, bool) {
	if id := strings.TrimSpace(d.GameID); id != "" {
		for _, g := range d.Matches {
			if g.ID == id {
				return g, true
			}
		}
	}
	if len(d.Matches) == 1 {
		return d.Matches[0], true
	}
	return hostclient.Game{}, false
}

// Set reports whether the room has published a selected location.
func (d Destination) Set() bool {
	return d.Kind != "" || d.Availability != "" || strings.TrimSpace(d.Label) != "" || strings.TrimSpace(d.GameID) != "" || strings.TrimSpace(d.RoomID) != "" || strings.TrimSpace(d.LauncherAction) != "" || strings.TrimSpace(d.PackageID) != "" || strings.TrimSpace(d.CoreID) != ""
}
