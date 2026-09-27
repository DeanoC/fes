package fogcast

import "errors"

// ErrSessionChanged rejects a host action captured for a different play. It is
// a host selection conflict, not a new target/runtime protocol error.
var ErrSessionChanged = errors.New("the running machine changed; refresh before trying again")

// SessionStopBinding names the play that was visible when Stop was chosen.
// The public host coordinator separately checks its ID and launch flight.
type SessionStopBinding struct {
	GameID     string `json:"game_id"`
	Target     string `json:"target"`
	TargetID   string `json:"target_id"`
	PackageID  string `json:"package_id"`
	Generation uint64 `json:"generation"`
}

// Caller holds lifecycle admission, so Status cannot promote another play
// between this check and stopLocked's target selection.
func (s *Service) matchesStopBinding(expected SessionStopBinding) bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	return expected.Target != "" && expected.PackageID != "" && expected.Generation != 0 &&
		s.activeTarget == expected.Target && targetByName(s.targets, s.activeTarget).TargetID == expected.TargetID &&
		s.activeGameID == expected.GameID && s.activePackageID == expected.PackageID && s.activePackageGeneration == expected.Generation
}
