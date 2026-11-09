package tenfoot

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// PreferenceStore supplies the tenfoot JSON settings record without requiring
// an operating-system path. WritePreferences must report storage and flush
// errors; returning nil means the record has reached the store's durability
// boundary. A queued store returns ErrPreferencesPending, serves the latest
// cached value and exposes PreferenceStatus until its platform checkpoint
// succeeds or fails. An App serializes its calls to this store.
type PreferenceStore interface {
	ReadPreferences() ([]byte, error)
	WritePreferences([]byte) error
}

// ErrPreferencesPending means the new settings are cached but have not reached
// persistent storage. It must not be reported as a completed durable write.
var ErrPreferencesPending = errors.New("settings are waiting to be saved")

type PreferenceSaveState struct{ Pending, Failed bool }

// PreferenceStatus is optional, nonblocking cached state. It must perform no IO
// or call back into the App; settings rendering can query it under the App lock.
type PreferenceStatus interface{ PreferenceSaveState() PreferenceSaveState }

// SetPreferenceStore loads and applies the store's settings before Start.
// Read or decode failure leaves the current settings and store unchanged.
// Call the ordinary presentation setters afterwards for explicit overrides.
// Once supplied, this store takes precedence over SetPrefsPath.
func (a *App) SetPreferenceStore(store PreferenceStore) error {
	if a == nil || store == nil {
		return fmt.Errorf("tenfoot preference store is nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return fmt.Errorf("tenfoot preference store must be set before Start")
	}
	prefs, err := readStoredPreferences(store)
	if err != nil {
		return fmt.Errorf("load tenfoot preferences: %w", err)
	}
	a.preferenceStore = store
	a.prefsWriteWarned = false
	a.setSafeAreaPctLocked(prefs.SafeAreaPct, false)
	a.setLayoutLocked(parseLayout(prefs.Layout), false)
	a.attractPrefEnabled = prefsAttractEnabled(prefs)
	a.attractDisabled = !a.attractPrefEnabled || a.attractForcedOff
	if a.attractDisabled {
		a.hideAttractLocked()
	}
	a.homeRooms, _ = parseHomePref(prefs.Home)
	a.homeRoom = strings.TrimSpace(prefs.HomeRoom)
	a.pinnedRooms = normalizePinnedRooms(prefs.PinnedRooms)
	status := a.status
	a.setReducedMotionLocked(prefs.ReducedMotion, false)
	a.status = status
	a.setSurroundLocked(prefs.Surround == nil || *prefs.Surround, false)
	return nil
}

func (a *App) readPreferencesLocked() (tenfootPrefs, error) {
	if a.preferenceStore == nil {
		return loadTenfootPrefs(a.prefsPath)
	}
	return readStoredPreferences(a.preferenceStore)
}

func readStoredPreferences(store PreferenceStore) (tenfootPrefs, error) {
	data, err := store.ReadPreferences()
	if err != nil {
		return tenfootPrefs{}, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return tenfootPrefs{}, fmt.Errorf("preferences must be a JSON object")
	}
	return decodeTenfootPrefs(data)
}

func (a *App) preferenceSourceLocked() string {
	if a.preferenceStore != nil {
		return "preference store"
	}
	return a.prefsPath
}

func (a *App) warnPreferenceWriteLocked(err error) {
	if err == nil || errors.Is(err, ErrPreferencesPending) || a.prefsWriteWarned {
		return
	}
	a.prefsWriteWarned = true
	stateWarn("tenfoot: could not save settings in preference store: %v\n", err)
}
