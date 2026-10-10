package protocol

import (
	"github.com/DeanoC/FogCast/protocol/internal/generated"
	"net/http"
	"regexp"
)

const MediaGameHeader = "X-FogCast-Media-Game"
const MediaBaseHeader = "X-FogCast-Media-Base"

var mediaGameID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// MediaDataStatus is the explicit library binding owned by the runtime.
// The immutable imported base image is never overwritten.
type MediaDataStatus struct {
	Mode        string `json:"mode"`
	GameID      string `json:"game_id"`
	BaseMediaID string `json:"base_media_id"`
	Revision    string `json:"revision"`
}

func (s MediaDataStatus) Valid() bool {
	return s.Mode == "persistent" && ValidMediaGameID(s.GameID) && ValidateDigest(s.BaseMediaID) == nil && ValidCoreDataRevision(s.Revision)
}
func ValidMediaGameID(id string) bool { return len(id) <= 256 && mediaGameID.MatchString(id) }
func AtariStFloppyWriteInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaAtariStFloppyWriteID, Major: generated.FesComputerInterfaceMediaAtariStFloppyWriteMajor, Minor: generated.FesComputerInterfaceMediaAtariStFloppyWriteMinor}
}
func MediaWriteCapable(p *CorePackageStatus) bool {
	return activeComputer(p) && activeInterface(p, AtariStFloppyInterface()) && activeInterface(p, AtariStFloppyWriteInterface())
}
func MediaDataBound(p *CorePackageStatus) bool {
	if !MediaWriteCapable(p) {
		return false
	}
	for _, u := range p.MediaUnits {
		if _, ok := MediaUnit(p, u.Unit); ok && u.Persistence != nil {
			return true
		}
	}
	return false
}

// MatchesForSave admits only a ready durable ST disk in the same active
// generation. A retained save failure permits one explicit Save; other media
// and input operations continue to use the error-free Matches predicate.
func (b MediaUnitBinding) MatchesForSave(s Status) bool {
	if s.LastError != nil && (s.LastError.Code != CodeSaveFailed || s.LastError.Phase != "save") {
		return false
	}
	s.LastError = nil // Identity validation uses a copy, never clears published status.
	if !b.Matches(s) || b.Unit != AtariStFloppyUnit || !MediaWriteCapable(s.CorePackage) || s.CorePackage.PersistenceMode != "persistent" {
		return false
	}
	u, ok := MediaUnit(s.CorePackage, b.Unit)
	return ok && u.State == MediaUnitReady && u.Persistence != nil && u.Persistence.Valid()
}

// MatchesSaveResult requires a confirmed, error-free checkpoint for the same
// durable game/base binding. Only its valid revision may change.
func (b MediaUnitBinding) MatchesSaveResult(before, after Status) bool {
	if !b.MatchesForSave(before) || after.LastError != nil || !b.MatchesForSave(after) {
		return false
	}
	old, _ := MediaUnit(before.CorePackage, b.Unit)
	current, _ := MediaUnit(after.CorePackage, b.Unit)
	return old.Interface == current.Interface && old.MinBytes == current.MinBytes && old.MaxBytes == current.MaxBytes && ValidateDigest(current.Persistence.Revision) == nil && old.Persistence.GameID == current.Persistence.GameID && old.Persistence.BaseMediaID == current.Persistence.BaseMediaID
}

// LibraryMediaBinding names an immutable base and a stable library entry.
// Development InsertMedia deliberately carries no durable binding.
type LibraryMediaBinding struct {
	MediaUnitBinding
	GameID      string
	BaseMediaID string
}

func (b LibraryMediaBinding) Valid() bool {
	return b.MediaUnitBinding.Valid() && b.Unit == AtariStFloppyUnit && ValidMediaGameID(b.GameID) && ValidateDigest(b.BaseMediaID) == nil
}
func (b LibraryMediaBinding) SetHeaders(h http.Header) {
	b.MediaUnitBinding.SetHeaders(h)
	h.Set(MediaGameHeader, b.GameID)
	h.Set(MediaBaseHeader, b.BaseMediaID)
}
func LibraryMediaHeaders(h http.Header) (LibraryMediaBinding, bool) {
	unit, ok := MediaUnitHeaders(h)
	games, bases := h.Values(MediaGameHeader), h.Values(MediaBaseHeader)
	if !ok || len(games) != 1 || len(bases) != 1 {
		return LibraryMediaBinding{}, false
	}
	b := LibraryMediaBinding{MediaUnitBinding: unit, GameID: games[0], BaseMediaID: bases[0]}
	return b, b.Valid()
}

// CloneMediaUnits isolates the optional durable metadata from status owners.
func CloneMediaUnits(units []MediaUnitStatus) []MediaUnitStatus {
	if len(units) == 0 {
		return nil
	}
	out := append([]MediaUnitStatus(nil), units...)
	for i := range out {
		if p := out[i].Persistence; p != nil {
			copy := *p
			out[i].Persistence = &copy
		}
	}
	return out
}
