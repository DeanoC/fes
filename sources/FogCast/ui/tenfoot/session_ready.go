package tenfoot

import (
	"encoding/json"
	"os"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// This marker proves a responsive paused launcher adopted a fresh host session.
// It is separate from frame readiness and is never a software activation gate.
func (a *App) clearSessionReadyLocked() {
	if path := os.Getenv("FES_SOFTWARE_READY_FILE"); path != "" {
		_ = os.Remove(path + ".session")
	}
	a.sessionReadyWrittenAt = time.Time{}
}

func (a *App) reportSessionReady(paused bool, observation uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	path, bundle := os.Getenv("FES_SOFTWARE_READY_FILE"), os.Getenv("FES_SOFTWARE_BUNDLE_ID")
	if path == "" || bundle == "" {
		return
	}
	now, s := time.Now(), a.session
	if !paused || !a.kitHost || observation != a.sessionObservation || a.hostAwaitStatus || a.kitHostPreparing || a.sessionDisplayBusy || a.sessionDisplayUncertain || a.sessionDisplayLocked().Visible || a.launch.Phase == "launching" || a.stopPhase == "stopping" || s.State != "active" || s.ID == "" || s.TargetID == "" || s.ErrorCode != "" || s.CorePackage == nil || s.CorePackage.Generation == 0 || protocol.ValidateDigest(s.CorePackage.PackageID) != nil || a.sessionObservedAt.IsZero() || now.Sub(a.sessionObservedAt) > 5*time.Second {
		a.clearSessionReadyLocked()
		return
	}
	if now.Sub(a.sessionReadyWrittenAt) < 500*time.Millisecond {
		return
	}
	data, err := json.Marshal(struct {
		BundleID   string    `json:"bundle_id"`
		PID        int       `json:"pid"`
		SessionID  string    `json:"session_id"`
		TargetID   string    `json:"target_id"`
		PackageID  string    `json:"package_id"`
		Generation uint64    `json:"generation"`
		ObservedAt time.Time `json:"observed_at"`
		UpdatedAt  time.Time `json:"updated_at"`
	}{bundle, os.Getpid(), s.ID, s.TargetID, s.CorePackage.PackageID, s.CorePackage.Generation, a.sessionObservedAt, now})
	if err != nil {
		a.clearSessionReadyLocked()
		return
	}
	temp := path + ".session.tmp"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		a.clearSessionReadyLocked()
		return
	}
	if err := os.Rename(temp, path+".session"); err != nil {
		a.clearSessionReadyLocked()
		return
	}
	a.sessionReadyWrittenAt = now
}
