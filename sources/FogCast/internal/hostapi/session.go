package hostapi

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/google/uuid"
)

type sessionService interface {
	Game(context.Context, string) (catalog.Game, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
	Status(context.Context) (protocol.Status, error)
}

type sessionLaunchOnService interface {
	LaunchOn(context.Context, string, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
}

type sessionCoreService interface {
	LoadCore(context.Context, int64, io.Reader) (protocol.Status, error)
}

// MediaSession is the session-owned seam for opt-in media transport.
type MediaSession interface {
	Start(context.Context, string) (MediaHandle, error)
}

type MediaHandle interface {
	Stop(context.Context) error
}

type sessionExecutionService interface {
	SessionExecution(context.Context, string) (string, error)
}

type sessionDevelopmentService interface {
	DevelopmentActive(context.Context) (bool, error)
}

type sessionDevelopmentStateService interface {
	DevelopmentSessionState(context.Context) (bool, string, error)
}

type sessionPackageOwnerService interface {
	ActivePackageOwned() bool
}

type sessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type sessionResult struct {
	ID                 string                         `json:"id"`
	Target             string                         `json:"target,omitempty"`
	TargetID           string                         `json:"target_id,omitempty"`
	Connection         *fogcast.TargetConnection      `json:"connection,omitempty"`
	State              protocol.State                 `json:"state"`
	GameID             *string                        `json:"game_id,omitempty"`
	System             *protocol.System               `json:"system,omitempty"`
	Execution          string                         `json:"execution,omitempty"`
	Media              string                         `json:"media,omitempty"`
	MediaName          string                         `json:"media_name,omitempty"`
	MediaEjected       bool                           `json:"media_ejected,omitempty"`
	Progress           *sessionProgress               `json:"progress,omitempty"`
	Input              *host.RemoteInputStatus        `json:"input,omitempty"`
	CorePackage        *protocol.CorePackageStatus    `json:"core_package,omitempty"`
	RuntimeInput       *protocol.RuntimeInputIdentity `json:"runtime_input,omitempty"`
	InputAgentInstance string                         `json:"input_agent_instance,omitempty"`
	FlightID           string                         `json:"flight_id,omitempty"`
}

// sessionEvent is one public host session event. Protocol 1 fields are
// unchanged; flight_id is an additive UUID for one launch, stop, or
// development-RBF / described-package mutation and the related events in
// that flight.
type sessionEvent struct {
	Sequence     uint64                  `json:"sequence"`
	FlightID     string                  `json:"flight_id,omitempty"`
	TSUTC        string                  `json:"ts_utc"`
	MonoMS       int64                   `json:"mono_ms"`
	ClientTSUTC  string                  `json:"client_ts_utc,omitempty"`
	ClientMonoMS *int64                  `json:"client_mono_ms,omitempty"`
	Event        string                  `json:"event"`
	State        protocol.State          `json:"state"`
	GameID       *string                 `json:"game_id,omitempty"`
	System       *protocol.System        `json:"system,omitempty"`
	Media        string                  `json:"media,omitempty"`
	Progress     *sessionProgress        `json:"progress,omitempty"`
	Input        *host.RemoteInputStatus `json:"input,omitempty"`
}

type sessionCoordinator struct {
	id                string
	service           sessionService
	remoteInput       host.RemoteInputController
	media             MediaSession
	mediaHandle       MediaHandle
	mediaGeneration   uint64
	execution         string
	executionPackage  sessionExecutionPackage
	packageOwned      bool
	nativeStoppedIdle bool // Soft-stopped non-package fpga_native label left set after idle cleanup
	mediaState        string
	liveMediaName     string
	liveMediaBinding  protocol.DevelopmentMediaBinding
	liveMediaFlightID string
	liveMediaEjected  bool
	inputBinding      sessionInputBinding
	mu                sync.Mutex
	observationMu     sync.Mutex
	busy              bool
	sequence          uint64
	flightID          string
	started           time.Time
	events            []sessionEvent
	// root owns the target coordinator registry; child coordinators carry all
	// session lifecycle resources for exactly one target.
	root               *sessionCoordinator
	target             string
	targetSessions     map[string]*sessionCoordinator
	targetSessionsMu   sync.Mutex
	remoteInputFactory RemoteInputFactory
	// remoteInputErr is why a target child has no remoteInput although the
	// host has a remote-input factory. Nil with a nil remoteInput means remote
	// input is not enabled on this host.
	remoteInputErr error
}

type sessionInputBinding struct {
	core        string
	packageID   string
	generation  uint64
	keyboard    bool
	keyboardHID bool
	gamepad     bool
	keypad      bool
	mouse       bool
}

// sessionExecutionPackage records the host-observable identity whose execution
// classification may differ from the target's development-load label. Runtime
// generations can repeat after an unobserved daemon restart; these fields do not
// identify the runtime incarnation.
type sessionExecutionPackage struct {
	packageID  string
	generation uint64
	flightID   string
	gameID     string
	system     protocol.System
	hasGame    bool
	hasSystem  bool
}

type adoptionSnapshot struct {
	execution         string
	executionPackage  sessionExecutionPackage
	packageOwned      bool
	nativeStoppedIdle bool
	mediaState        string
	mediaHandle       MediaHandle
	mediaGeneration   uint64
	inputBinding      sessionInputBinding
	flightID          string
}

type remoteInputCapabilityAttacher interface {
	AttachWithCapabilities(context.Context, string, bool) error
}

func newSessionCoordinator(service sessionService, remoteInput host.RemoteInputController, media MediaSession) *sessionCoordinator {
	return &sessionCoordinator{id: uuid.NewString(), service: service, remoteInput: remoteInput, media: media, started: time.Now(), targetSessions: make(map[string]*sessionCoordinator)}
}

func (s *sessionCoordinator) forTarget(target string) *sessionCoordinator {
	if s == nil || target == "" {
		return s
	}
	root := s
	if s.root != nil {
		root = s.root
	}
	root.targetSessionsMu.Lock()
	defer root.targetSessionsMu.Unlock()
	if child := root.targetSessions[target]; child != nil {
		return child
	}
	// A target child must never detach a different target's singleton bridge.
	// Deployments that support multiple live kits supply a factory below.
	var input host.RemoteInputController
	var inputErr error
	if root.remoteInputFactory != nil {
		input, inputErr = root.remoteInputFactory(target)
		if inputErr == nil && input == nil {
			inputErr = errors.New("remote input factory returned no bridge")
		}
		if inputErr != nil {
			input = nil
		}
	}
	child := newSessionCoordinator(root.service, input, root.media)
	child.remoteInputErr = inputErr
	child.root = root
	child.target = target
	root.targetSessions[target] = child
	return child
}

func (s *sessionCoordinator) forSessionID(id string) *sessionCoordinator {
	root := s
	if s != nil && s.root != nil {
		root = s.root
	}
	if root == nil || root.id == id {
		return root
	}
	root.targetSessionsMu.Lock()
	defer root.targetSessionsMu.Unlock()
	for _, child := range root.targetSessions {
		if child.id == id {
			return child
		}
	}
	return root
}

func (s *sessionCoordinator) scoped(ctx context.Context) context.Context {
	if s != nil && s.target != "" {
		return fogcast.WithSessionTarget(ctx, s.target)
	}
	return ctx
}

func (s *sessionCoordinator) beginFlight() {
	id := uuid.NewString()
	s.mu.Lock()
	s.flightID = id
	s.mu.Unlock()
}

func (s *sessionCoordinator) restoreFlight(id string) {
	s.mu.Lock()
	s.flightID = id
	s.mu.Unlock()
}

func (s *sessionCoordinator) ensureFlight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flightID == "" {
		s.flightID = uuid.NewString()
		// A package reconstructed by the first status observation has no
		// flight yet. Associate that same binding with its first flight.
		if s.executionPackage.flightID == "" {
			s.executionPackage.flightID = s.flightID
		}
	}
}

func (s *sessionCoordinator) record(event string, result sessionResult, progress *sessionProgress) {
	s.recordStamp(event, result, progress, clientStamp{})
}

func (s *sessionCoordinator) recordStamp(event string, result sessionResult, progress *sessionProgress, stamp clientStamp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started.IsZero() {
		s.started = time.Now()
	}
	s.sequence++
	item := sessionEvent{
		Sequence: s.sequence,
		FlightID: s.flightID,
		TSUTC:    time.Now().UTC().Format(time.RFC3339Nano),
		MonoMS:   time.Since(s.started).Milliseconds(),
		Event:    event,
		State:    result.State,
		GameID:   result.GameID,
		System:   result.System,
		Media:    result.Media,
		Progress: progress,
		Input:    cloneRemoteInputStatus(result.Input),
	}
	if stamp.TsUTC != "" {
		item.ClientTSUTC = stamp.TsUTC
	}
	if stamp.MonoSet {
		mono := stamp.MonoMS
		item.ClientMonoMS = &mono
	}
	s.events = append(s.events, item)
	if len(s.events) > 256 {
		s.events = s.events[len(s.events)-256:]
	}
}

func (s *sessionCoordinator) eventsAfter(after uint64) []sessionEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]sessionEvent, 0)
	for _, event := range s.events {
		if event.Sequence > after {
			result = append(result, event)
		}
	}
	return result
}

func (s *sessionCoordinator) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *sessionCoordinator) beginWait(ctx context.Context, max time.Duration) bool {
	deadline := time.NewTimer(max)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.begin() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func (s *sessionCoordinator) end() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *sessionCoordinator) publicSession(st protocol.Status, progress *sessionProgress) sessionResult {
	result := publicSession(st, progress)
	s.mu.Lock()
	result.ID = s.id
	result.FlightID = s.flightID
	name, binding, mediaFlightID, mediaEjected := s.liveMediaName, s.liveMediaBinding, s.liveMediaFlightID, s.liveMediaEjected
	s.mu.Unlock()
	if s.target != "" {
		result.Target = s.target
		if binder, ok := s.service.(interface{ TargetIDForName(string) string }); ok {
			result.TargetID = binder.TargetIDForName(s.target)
		}
	} else if binder, ok := s.service.(interface{ SessionTarget() (string, string) }); ok {
		result.Target, result.TargetID = binder.SessionTarget()
	}
	mediaBindingMatches := result.FlightID == mediaFlightID &&
		result.Target == binding.Target && result.TargetID == binding.TargetID &&
		st.State == protocol.StateActive && st.CorePackage != nil &&
		st.CorePackage.PackageID == binding.PackageID && st.CorePackage.Generation == binding.Generation
	if mediaBindingMatches {
		unit, observed := protocol.MediaUnit(st.CorePackage, 0)
		if !observed {
			result.MediaName = name
			result.MediaEjected = mediaEjected
		} else if unit.State == protocol.MediaUnitReady {
			result.MediaName = name
		}
	} else if st.State != "" {
		// A confirmed filename belongs only to the observed session flight and
		// package binding, so a relaunch that reuses a generation cannot revive it.
		s.mu.Lock()
		if s.liveMediaName == name && s.liveMediaBinding == binding && s.liveMediaFlightID == mediaFlightID {
			s.clearLiveMediaNameLocked()
		}
		s.mu.Unlock()
	}
	if s.remoteInput != nil {
		input := s.remoteInput.Status()
		result.Input = &input
	}
	return result
}

// publicCurrentSession projects a successful operation on an existing session.
// A coordinator-owned native package may be reported as a development load.
// Override that target label only for the confirmed package generation and
// flight; another actor can replace a package without updating this coordinator.
func (s *sessionCoordinator) publicCurrentSession(st protocol.Status) sessionResult {
	result := s.publicSession(st, nil)
	s.mu.Lock()
	if st.State == protocol.StateActive && s.execution != "" &&
		(s.matchesExecutionPackageLocked(st) || s.matchesUnpackagedExecutionLocked(st)) {
		result.Execution = s.execution
	}
	s.mu.Unlock()
	return result
}

func (s *sessionCoordinator) matchesExecutionPackageLocked(st protocol.Status) bool {
	binding := s.executionPackage
	return st.CorePackage != nil && binding.packageID != "" && binding.generation != 0 &&
		binding.flightID == s.flightID && st.CorePackage.PackageID == binding.packageID &&
		st.CorePackage.Generation == binding.generation && matchesExecutionIdentity(binding, st)
}

func (s *sessionCoordinator) matchesUnpackagedExecutionLocked(st protocol.Status) bool {
	binding := s.executionPackage
	return s.execution == fogcast.ExecutionFPGANative &&
		binding.packageID == "" && !st.Development && st.CorePackage == nil &&
		binding.flightID == s.flightID && binding.hasGame && binding.hasSystem && matchesExecutionIdentity(binding, st)
}

func matchesExecutionIdentity(binding sessionExecutionPackage, st protocol.Status) bool {
	return (!binding.hasGame || st.GameID != nil && *st.GameID == binding.gameID) &&
		(!binding.hasSystem || st.System != nil && *st.System == binding.system)
}

// bindExecutionPackageLocked records only an explicitly observed package. The
// caller holds s.mu and must have established the session's execution owner.
func (s *sessionCoordinator) bindExecutionPackageLocked(st protocol.Status) {
	s.executionPackage = sessionExecutionPackage{}
	if st.State != protocol.StateActive {
		return
	}
	s.executionPackage.flightID = s.flightID
	if st.GameID != nil {
		s.executionPackage.gameID, s.executionPackage.hasGame = *st.GameID, true
	}
	if st.System != nil {
		s.executionPackage.system, s.executionPackage.hasSystem = *st.System, true
	}
	if st.CorePackage != nil && st.CorePackage.PackageID != "" && st.CorePackage.Generation != 0 {
		s.executionPackage.packageID, s.executionPackage.generation = st.CorePackage.PackageID, st.CorePackage.Generation
	}
}

// clearLiveMediaNameLocked retires the complete identity of a cached filename.
// The caller must hold s.mu.
func (s *sessionCoordinator) clearLiveMediaNameLocked() {
	s.liveMediaName = ""
	s.liveMediaBinding = protocol.DevelopmentMediaBinding{}
	s.liveMediaFlightID = ""
	s.liveMediaEjected = false
}

func busyError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
}

func developmentMustStopError() error {
	return &protocol.APIError{Code: protocol.CodeBusy, Message: "stop the development RBF before launching a game"}
}

func sessionCore(st protocol.Status) string {
	if st.ObservedCore != nil {
		return *st.ObservedCore
	}
	if st.ExpectedCore != nil {
		return *st.ExpectedCore
	}
	return ""
}

func cloneRemoteInputStatus(status *host.RemoteInputStatus) *host.RemoteInputStatus {
	if status == nil {
		return nil
	}
	copy := *status
	return &copy
}

func publicSession(st protocol.Status, progress *sessionProgress) sessionResult {
	gameID := st.GameID
	system := st.System
	if st.State != protocol.StateActive {
		gameID = nil
		system = nil
	}
	result := sessionResult{State: st.State, GameID: gameID, System: system, Progress: progress,
		CorePackage: cloneCorePackageStatus(st.CorePackage)}
	if st.State == protocol.StateActive && st.RuntimeInput != nil && st.RuntimeInput.Validate() == nil {
		identity := *st.RuntimeInput
		if (identity.Kind == protocol.RuntimeInputDescribed && st.CorePackage != nil && st.CorePackage.PackageID == identity.PackageID && st.CorePackage.Generation == identity.Generation) ||
			(identity.Kind == protocol.RuntimeInputDevelopment && st.Development && st.CorePackage == nil) {
			result.RuntimeInput = &identity
		}
	}
	if st.State == protocol.StateActive && protocol.ValidInputAgentInstance(st.InputAgentInstance) {
		result.InputAgentInstance = st.InputAgentInstance
	}
	if st.State == protocol.StateActive && st.Development {
		result.Execution = fogcast.ExecutionFPGADevelopment
	} else if system != nil {
		result.Execution = fogcast.ExecutionFPGANative
	}
	return result
}

func cloneCorePackageStatus(value *protocol.CorePackageStatus) *protocol.CorePackageStatus {
	if value == nil {
		return nil
	}
	copy := *value
	if value.StaticComposition != nil {
		static := *value.StaticComposition
		copy.StaticComposition = &static
	}
	copy.ActiveInterfaces = append([]protocol.RuntimeInterface(nil), value.ActiveInterfaces...)
	if value.MediaStream != nil {
		stream := *value.MediaStream
		copy.MediaStream = &stream
	}
	if value.SlotComposition != nil {
		slots := *value.SlotComposition
		slots.Expansions = append(slots.Expansions[:0:0], value.SlotComposition.Expansions...)
		copy.SlotComposition = &slots
	}
	if value.MediaUnits != nil {
		copy.MediaUnits = protocol.CloneMediaUnits(value.MediaUnits)
	}
	return &copy
}

func publicEvents(events []sessionEvent) []sessionEvent {
	result := make([]sessionEvent, len(events))
	copy(result, events)
	return result
}

type sessionEventsResult struct {
	Events []sessionEvent `json:"events"`
}

func resumedCoreDataStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.Recovery == "" && status.CorePackage != nil && status.CorePackage.Generation != 0 && status.CorePackage.PersistenceMode == "persistent" &&
		(status.LastError == nil || (status.LastError.Code == protocol.CodeSaveFailed && status.LastError.Phase == "save"))
}
