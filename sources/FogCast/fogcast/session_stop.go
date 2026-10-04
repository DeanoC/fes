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

// Caller holds lifecycle admission, so Status cannot change this kit's play
// between the binding check and stopLocked's target selection.
func (s *Service) matchesStopBinding(expected SessionStopBinding) bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	play := s.plays[expected.Target]
	return expected.Target != "" && expected.PackageID != "" && expected.Generation != 0 &&
		targetByName(s.targets, expected.Target).TargetID == expected.TargetID &&
		play.gameID == expected.GameID && play.packageID == expected.PackageID && play.packageGeneration == expected.Generation
}
