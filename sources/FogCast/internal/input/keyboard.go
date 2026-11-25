package input

import (
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"sync"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

var errMatrixUnconfirmed = errors.New("keyboard matrix source is unconfirmed")
var matrixInstancePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func validObservedMatrixInstance(instance string) bool {
	return matrixInstancePattern.MatchString(instance) && instance != "00000000000000000000000000000000"
}

func matrixObservationRetires(obs CoreObservation, instance, packageID string, generation uint64) bool {
	// Production Runtime cannot publish active/idle status for a fresh
	// instance until Start has successfully programmed MENU or idle. Thus a
	// validated new instance also retires the prior physical core. Unknown or
	// failed startup must not be projected as Active or IdleConfirmed.
	idle := obs.IdleConfirmed && !obs.Active && obs.Generation == 0 && obs.PackageID == ""
	active := obs.Active && !obs.IdleConfirmed && obs.Generation != 0
	return validObservedMatrixInstance(obs.RuntimeInstance) && (idle || active &&
		(obs.RuntimeInstance != instance || obs.Generation != generation || obs.PackageID != packageID))
}

type MatrixBinding struct {
	RuntimeInstance string
	PackageID       string
	Generation      uint64
}

type MatrixPoster func(context.Context, localhid.MatrixPost, uint64) error
type MatrixRetirer func(context.Context, localhid.MatrixRetirement) error

// KeyboardSink serializes source union mutations and their physical posts.
// An ambiguous post fences the union until that exact source is neutralized.
type KeyboardSink struct {
	mu            sync.Mutex
	pressed       [sourceCount]map[remoteinput.Code]bool
	blocked       [sourceCount]map[remoteinput.Code]bool
	focused       bool
	poster        MatrixPoster
	retirer       MatrixRetirer
	binding       *MatrixBinding
	sinkID        [16]byte
	nextSequence  uint64
	uncertain     *localhid.MatrixWitness
	retiredNoPost bool
}

type matrixIdleCleanupKey struct{}

func NewKeyboardSink() *KeyboardSink {
	sink := &KeyboardSink{}
	if _, err := rand.Read(sink.sinkID[:]); err != nil {
		sink.sinkID = [16]byte{}
	}
	return sink
}

func (s *KeyboardSink) SetPoster(poster MatrixPoster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poster = poster
}

func (s *KeyboardSink) SetRetirer(retirer MatrixRetirer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retirer = retirer
}

// neutralMediaLocked holds the source ledger stable from witness capture
// through the adapter's confirmed all-source neutral. It preserves held-key
// suppression on every failure, including proven pre-dispatch rejection.
func (s *KeyboardSink) neutralMedia(ctx context.Context, query func(context.Context) (localhid.MatrixMediaWitness, bool, error), neutral func(context.Context, localhid.MatrixMediaNeutral) error, validate func(context.Context) error) (bool, error) {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return false, err
	}
	defer s.mu.Unlock()
	if query == nil || neutral == nil || validate == nil {
		return false, nil
	}
	witness, pending, err := query(ctx)
	if err != nil || !pending {
		return pending, err
	}
	if s.binding == nil || witness.RuntimeInstance != s.binding.RuntimeInstance || witness.PackageID != s.binding.PackageID || witness.Generation != s.binding.Generation {
		return true, errMatrixUnconfirmed
	}
	if err := validate(ctx); err != nil {
		return true, err
	}
	for source := inputSource(0); source < sourceCount; source++ {
		s.suppressSourceLocked(source)
	}
	request := localhid.MatrixMediaNeutral{MediaOf: witness, Validate: validate}
	if s.uncertain != nil {
		copy := *s.uncertain
		request.SourceOf = &copy
	}
	if err := neutral(ctx, request); err != nil {
		return true, err
	}
	s.uncertain = nil
	return true, nil
}

// retireObserved samples after the uncertain post drained, while the sink
// remains locked. The caller holds the lifecycle fence through the adapter's
// validation and exact-witness retirement; neither path posts a matrix.
func (s *KeyboardSink) retireObserved(ctx context.Context, observe func(context.Context) (CoreObservation, error), validate func(context.Context) error) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.uncertain == nil {
		return nil
	}
	if observe == nil || validate == nil || s.retirer == nil {
		return errMatrixUnconfirmed
	}
	if err := validate(ctx); err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	obs, err := observe(probe)
	if err == nil {
		err = probe.Err()
	}
	cancel()
	if err != nil {
		return err
	}
	old := *s.uncertain
	if !matrixObservationRetires(obs, old.RuntimeInstance, old.PackageID, old.Generation) {
		return errMatrixUnconfirmed
	}
	proof := localhid.MatrixRetirement{RetireOf: old, ObservedRuntimeInstance: obs.RuntimeInstance,
		ObservedPackageID: obs.PackageID, ObservedGeneration: obs.Generation, Idle: obs.IdleConfirmed, Validate: validate}
	if err := s.retirer(ctx, proof); err != nil {
		return err
	}
	s.uncertain = nil
	s.binding = nil
	s.retiredNoPost = true
	for source := inputSource(0); source < sourceCount; source++ {
		s.suppressSourceLocked(source)
	}
	return nil
}

// hasActiveStateContext reports whether this matrix sink currently holds a
// binding, uncertain write, or any pressed key that still requires release.
func (s *KeyboardSink) hasActiveStateContext(ctx context.Context) (bool, error) {
	if s == nil {
		return false, nil
	}
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return false, err
	}
	defer s.mu.Unlock()
	if s.binding != nil || s.uncertain != nil {
		return true, nil
	}
	for _, pressed := range s.pressed {
		if len(pressed) != 0 {
			return true, nil
		}
	}
	return false, nil
}

// retireUnboundIdle avoids inventing a matrix binding for a fresh sink. A
// missing binding is not evidence of neutral hardware: an unbound, empty
// matrix sink may retire only after a fresh explicit observation with a valid
// runtime instance proving either idle or an active packaged core without
// fes.keyboard (raw development RBFs and active matrix cores remain rejected).
func (s *KeyboardSink) retireUnboundIdle(ctx context.Context, observe func(context.Context) (CoreObservation, error), validate func(context.Context) error) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.binding != nil || s.uncertain != nil || observe == nil || validate == nil {
		return errMatrixUnconfirmed
	}
	for _, pressed := range s.pressed {
		if len(pressed) != 0 {
			return errMatrixUnconfirmed
		}
	}
	if err := validate(ctx); err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	obs, err := observe(probe)
	if err == nil {
		err = probe.Err()
	}
	cancel()
	if err != nil {
		return err
	}
	idle := obs.IdleConfirmed && !obs.Active && obs.PackageID == "" && obs.Generation == 0 && validObservedMatrixInstance(obs.RuntimeInstance)
	nonMatrixPackaged := obs.Active && !obs.IdleConfirmed && !obs.Keyboard && obs.PackageID != "" && obs.Generation != 0 && validObservedMatrixInstance(obs.RuntimeInstance)
	if !idle && !nonMatrixPackaged {
		return errMatrixUnconfirmed
	}
	if err := validate(ctx); err != nil {
		return err
	}
	return nil
}

// retireBindingObserved is used by a source token whose entire old core has
// retired. It samples after acquiring the sink lock and never changes a
// successor binding, even when the package and generation numbers repeat.
func (s *KeyboardSink) retireBindingObserved(ctx context.Context, expected MatrixBinding, observe func(context.Context) (CoreObservation, error), validate func(context.Context) error) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.binding == nil || *s.binding != expected || observe == nil || validate == nil {
		return errMatrixUnconfirmed
	}
	if err := validate(ctx); err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	obs, err := observe(probe)
	if err == nil {
		err = probe.Err()
	}
	cancel()
	if err != nil {
		return err
	}
	if !matrixObservationRetires(obs, expected.RuntimeInstance, expected.PackageID, expected.Generation) {
		return errMatrixUnconfirmed
	}
	if s.uncertain != nil {
		old := *s.uncertain
		if old.RuntimeInstance != expected.RuntimeInstance || old.PackageID != expected.PackageID || old.Generation != expected.Generation || s.retirer == nil {
			return errMatrixUnconfirmed
		}
		proof := localhid.MatrixRetirement{RetireOf: old, ObservedRuntimeInstance: obs.RuntimeInstance, ObservedPackageID: obs.PackageID, ObservedGeneration: obs.Generation, Idle: obs.IdleConfirmed, Validate: validate}
		if err := s.retirer(ctx, proof); err != nil {
			return err
		}
		s.uncertain = nil
	} else {
		if err := validate(ctx); err != nil {
			return err
		}
	}
	for source := inputSource(0); source < sourceCount; source++ {
		s.suppressSourceLocked(source)
	}
	s.binding = nil
	s.retiredNoPost = true
	return nil
}

// BindInitial installs the first observed authority. Replacement requires the
// controller's lifecycle proof and cannot be asserted by this method.
func (s *KeyboardSink) BindInitial(binding MatrixBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding != nil {
		if *s.binding == binding {
			return nil
		}
		return errMatrixUnconfirmed
	}
	if binding.RuntimeInstance == "" || binding.Generation == 0 || s.uncertain != nil {
		return errMatrixUnconfirmed
	}
	copy := binding
	s.binding = &copy
	s.retiredNoPost = false
	return nil
}

// bind replaces a matrix authority only after the caller has positively proven
// the old generation retired. It never posts an old union into a successor.
func (s *KeyboardSink) bindContext(ctx context.Context, binding *MatrixBinding, oldRetired, updateBinding, forgetLocal bool) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	same := s.binding != nil && binding != nil && *s.binding == *binding
	// A status observation may have started before the failed post drained.
	// It cannot clear uncertainty merely because it differs from this binding.
	if updateBinding && !same {
		if s.uncertain != nil {
			return errMatrixUnconfirmed
		}
		s.binding = nil
		if binding != nil {
			copy := *binding
			s.binding = &copy
			s.retiredNoPost = false
		}
		if oldRetired {
			for source := inputSource(0); source < sourceCount; source++ {
				s.suppressSourceLocked(source)
			}
		}
	}
	if forgetLocal {
		s.suppressSourceLocked(sourceLocal)
		s.suppressSourceLocked(sourceLocalMatrix)
	}
	return nil
}

func (s *KeyboardSink) postLocked(ctx context.Context, source inputSource, neutral bool, validate func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.poster == nil {
		return nil
	}
	idleGuard, _ := ctx.Value(matrixIdleCleanupKey{}).(func(context.Context) error)
	if s.binding == nil && s.uncertain == nil && (s.retiredNoPost || idleGuard != nil) {
		if idleGuard != nil {
			if err := idleGuard(ctx); err != nil {
				return err
			}
		}
		if len(s.unionLocked()) == 0 {
			return nil
		}
		// Idle retirement certifies only an empty cleanup, never a press.
		// The caller must roll back the unbound provisional mutation.
		return errors.Join(localhid.ErrMatrixNotDispatched, errMatrixUnconfirmed)
	}
	if s.sinkID == ([16]byte{}) || s.nextSequence >= ^uint64(0)-1 {
		return errMatrixUnconfirmed
	}
	if s.uncertain != nil && (!neutral || s.uncertain.Source != uint8(source)) {
		return errMatrixUnconfirmed
	}
	s.nextSequence++
	witness := localhid.MatrixWitness{Sink: s.sinkID, Sequence: s.nextSequence, Source: uint8(source)}
	if s.binding != nil {
		witness.RuntimeInstance = s.binding.RuntimeInstance
		witness.PackageID = s.binding.PackageID
		witness.Generation = s.binding.Generation
	}
	if validate == nil {
		validate = func(ctx context.Context) error { return ctx.Err() }
	}
	post := localhid.MatrixPost{Witness: witness, Validate: validate}
	if s.uncertain != nil {
		old := *s.uncertain
		post.NeutralOf = &old
	}
	err := s.poster(ctx, post, zx81keys.Matrix(s.unionLocked()))
	if err != nil {
		if s.uncertain == nil && !errors.Is(err, localhid.ErrMatrixNotDispatched) {
			old := witness
			s.uncertain = &old
		}
		return err
	}
	if post.NeutralOf != nil {
		s.uncertain = nil
	}
	return nil
}

func (s *KeyboardSink) Apply(f protocol.InputFrame) error {
	return s.ApplyFrom(context.Background(), sourceRemote, f)
}

func (s *KeyboardSink) ApplyFrom(ctx context.Context, source inputSource, f protocol.InputFrame) error {
	return s.applyFromBound(ctx, source, f, nil, matrixGuardFrom(ctx))
}

// applyFromBound checks the immutable admission identity under the same lock
// as mutation/post; nil is for callers that have no external binding.
func (s *KeyboardSink) applyFromBound(ctx context.Context, source inputSource, f protocol.InputFrame, expected *MatrixBinding, validate func(context.Context) error) error {
	if f.Kind != uint8(remoteinput.KindKey) && f.Device != uint8(remoteinput.DeviceKeyboard) {
		return errors.New("unsupported input frame")
	}
	if source >= sourceCount {
		source = sourceRemote
	}
	code := remoteinput.Code(f.Code)
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if expected != nil && (s.binding == nil || *s.binding != *expected) {
		return errNoCore
	}
	if validate != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := validate(ctx); err != nil {
			return err
		}
	}
	if s.uncertain != nil {
		return errMatrixUnconfirmed
	}
	oldPressed := cloneMatrixKeys(s.pressed[source])
	oldBlocked := cloneMatrixKeys(s.blocked[source])
	if f.Action == uint8(remoteinput.ActionRelease) {
		delete(s.blocked[source], code)
	}
	if s.focused || s.blocked[source][code] {
		if s.focused && f.Action == uint8(remoteinput.ActionPress) {
			if s.blocked[source] == nil {
				s.blocked[source] = map[remoteinput.Code]bool{}
			}
			s.blocked[source][code] = true
		}
		delete(s.pressed[source], code)
		return nil
	}
	if s.pressed[source] == nil {
		s.pressed[source] = map[remoteinput.Code]bool{}
	}
	if f.Action == uint8(remoteinput.ActionPress) {
		s.pressed[source][code] = true
	} else if f.Action == uint8(remoteinput.ActionRelease) {
		delete(s.pressed[source], code)
	}
	err := s.postLocked(ctx, source, false, validate)
	if errors.Is(err, localhid.ErrMatrixNotDispatched) {
		s.pressed[source] = oldPressed
		s.blocked[source] = oldBlocked
	}
	return err
}

func cloneMatrixKeys(keys map[remoteinput.Code]bool) map[remoteinput.Code]bool {
	if keys == nil {
		return nil
	}
	copy := make(map[remoteinput.Code]bool, len(keys))
	for key, held := range keys {
		copy[key] = held
	}
	return copy
}

func (s *KeyboardSink) replaceSource(ctx context.Context, source inputSource, desired map[remoteinput.Code]bool) error {
	if s == nil {
		return nil
	}
	if source >= sourceCount {
		source = sourceRemote
	}
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.uncertain != nil {
		return errMatrixUnconfirmed
	}
	next := map[remoteinput.Code]bool{}
	if !s.focused {
		for code := range desired {
			if !s.blocked[source][code] {
				next[code] = true
			}
		}
	}
	old := s.pressed[source]
	s.pressed[source] = next
	err := s.postLocked(ctx, source, false, matrixGuardFrom(ctx))
	if errors.Is(err, localhid.ErrMatrixNotDispatched) {
		s.pressed[source] = old
	}
	return err
}

// neutralUnconfirmedSource resolves only this source's ambiguous post. Held
// inputs remain suppressed until their physical release; no desired press is
// replayed while resolving the uncertainty.
func (s *KeyboardSink) neutralUnconfirmedSource(ctx context.Context, source inputSource, desired map[remoteinput.Code]bool) (bool, error) {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return false, err
	}
	defer s.mu.Unlock()
	if s.uncertain == nil {
		return false, nil
	}
	if source >= sourceCount || uint8(source) != s.uncertain.Source {
		return true, errMatrixUnconfirmed
	}
	s.suppressSourceLocked(source)
	for code := range desired {
		s.blocked[source][code] = true
	}
	if err := s.postLocked(ctx, source, true, matrixGuardFrom(ctx)); err != nil {
		return true, err
	}
	for code := range s.blocked[source] {
		if !desired[code] {
			delete(s.blocked[source], code)
		}
	}
	return true, nil
}

// suppressMappedKeys fences physically held pad contributors across a core
// transition. It changes bookkeeping only and never posts a guest matrix.
func (s *KeyboardSink) suppressMappedKeys(ctx context.Context, desired map[remoteinput.Code]bool) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.blocked[sourceLocalMatrix] == nil {
		s.blocked[sourceLocalMatrix] = make(map[remoteinput.Code]bool)
	}
	for code := range desired {
		s.blocked[sourceLocalMatrix][code] = true
	}
	return nil
}

// reconcileMappedReleases rearms a historical mapped key only when no
// continuous-reader contributor still maps to it on any supported core.
// A disconnected reader's orphaned keys require authoritative recovery.
func (s *KeyboardSink) reconcileMappedReleases(ctx context.Context, held, orphans map[remoteinput.Code]bool) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.uncertain != nil {
		return nil
	}
	for code := range s.blocked[sourceLocalMatrix] {
		if !held[code] && !orphans[code] {
			delete(s.blocked[sourceLocalMatrix], code)
		}
	}
	return nil
}

// quarantineMappedSubset removes only ambiguous mappings while preserving
// unrelated local and remote contributors in the confirmed union.
func (s *KeyboardSink) quarantineMappedSubset(ctx context.Context, mappings map[remoteinput.Code]bool, expected *localhid.MatrixWitness) (*localhid.MatrixWitness, error) {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return expected, err
	}
	defer s.mu.Unlock()
	if (s.uncertain == nil) != (expected == nil) || (s.uncertain != nil && *s.uncertain != *expected) {
		return expected, errMatrixUnconfirmed
	}
	if s.blocked[sourceLocalMatrix] == nil {
		s.blocked[sourceLocalMatrix] = make(map[remoteinput.Code]bool)
	}
	for code := range mappings {
		s.blocked[sourceLocalMatrix][code] = true
		delete(s.pressed[sourceLocalMatrix], code)
	}
	err := s.postLocked(ctx, sourceLocalMatrix, expected != nil, matrixGuardFrom(ctx))
	if err != nil && expected == nil && s.uncertain != nil {
		copy := *s.uncertain
		expected = &copy
	}
	return expected, err
}

func (s *KeyboardSink) unionLocked() map[remoteinput.Code]bool {
	union := make(map[remoteinput.Code]bool)
	for source := range s.pressed {
		for code := range s.pressed[source] {
			union[code] = true
		}
	}
	return union
}

// suppressSourceLocked preserves physical-held suppression even when a
// lifecycle neutral removes the contribution from the target union.
func (s *KeyboardSink) suppressSourceLocked(source inputSource) {
	if s.blocked[source] == nil {
		s.blocked[source] = map[remoteinput.Code]bool{}
	}
	for code := range s.pressed[source] {
		s.blocked[source][code] = true
	}
	s.pressed[source] = nil
}

func (s *KeyboardSink) ReleaseSource(source inputSource) error {
	return s.releaseSourceContext(context.Background(), source)
}
func (s *KeyboardSink) releaseSourceContext(ctx context.Context, source inputSource) error {
	return s.releaseSourcesContext(ctx, source)
}
func (s *KeyboardSink) releaseSourcesContext(ctx context.Context, sources ...inputSource) error {
	return s.releaseSourcesBound(ctx, nil, matrixGuardFrom(ctx), sources...)
}

func (s *KeyboardSink) releaseSourcesBound(ctx context.Context, expected *MatrixBinding, validate func(context.Context) error, sources ...inputSource) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if expected != nil && (s.binding == nil || *s.binding != *expected) {
		return errNoCore
	}
	if validate != nil {
		if err := validate(ctx); err != nil {
			return err
		}
	}
	neutralSource := sourceRemote
	neutral := false
	if s.uncertain != nil {
		for _, source := range sources {
			if uint8(source) == s.uncertain.Source {
				neutral = true
				neutralSource = source
			}
		}
		if !neutral {
			return errMatrixUnconfirmed
		}
	}
	for _, source := range sources {
		if source >= sourceCount {
			source = sourceRemote
		}
		s.suppressSourceLocked(source)
		if !neutral {
			neutralSource = source
		}
	}
	return s.postLocked(ctx, neutralSource, neutral, validate)
}

func (s *KeyboardSink) ReleaseAll() error { return s.releaseAllContext(context.Background()) }

func (s *KeyboardSink) releaseAllContext(ctx context.Context) error {
	if err := lockInputMutex(ctx, &s.mu); err != nil {
		return err
	}
	defer s.mu.Unlock()
	s.focused = false
	for source := inputSource(0); source < sourceCount; source++ {
		s.suppressSourceLocked(source)
	}
	source := sourceRemote
	neutral := s.uncertain != nil
	if neutral {
		source = inputSource(s.uncertain.Source)
	}
	return s.postLocked(ctx, source, neutral, nil)
}

func (s *KeyboardSink) setDisplayFocus(focused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if focused {
		for source, keys := range s.pressed {
			if s.blocked[source] == nil {
				s.blocked[source] = map[remoteinput.Code]bool{}
			}
			for code := range keys {
				s.blocked[source][code] = true
			}
			s.pressed[source] = map[remoteinput.Code]bool{}
		}
	}
	s.focused = focused
}

func (s *KeyboardSink) Close() error { return s.ReleaseAll() }

type muxSink struct {
	keys *KeyboardSink
	pads bridge.Sink
}

func (m muxSink) Apply(f protocol.InputFrame) error {
	if keyboardFrame(f) {
		if m.keys != nil && f.Code >= uint16(zx81keys.KeyShift) {
			return m.keys.Apply(f)
		}
		return nil
	}
	if m.pads != nil {
		return m.pads.Apply(f)
	}
	return nil
}

func keyboardFrame(f protocol.InputFrame) bool {
	return f.Device == uint8(remoteinput.DeviceKeyboard) || f.Kind == uint8(remoteinput.KindKey)
}

func (m muxSink) ReleaseAll() error {
	var keyErr, padErr error
	if m.keys != nil {
		keyErr = m.keys.ReleaseAll()
	}
	if m.pads != nil {
		padErr = m.pads.ReleaseAll()
	}
	if keyErr != nil {
		return keyErr
	}
	return padErr
}

func (m muxSink) Close() error {
	var keyErr, padErr error
	if m.keys != nil {
		keyErr = m.keys.Close()
	}
	if m.pads != nil {
		padErr = m.pads.Close()
	}
	if keyErr != nil {
		return keyErr
	}
	return padErr
}
