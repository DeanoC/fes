package hostclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/protocol"
)

// What a kit holds for a shelf disk beside its imported image.
const (
	ShelfKeptNone    = "none"
	ShelfKeptChanges = "changes"
	ShelfKeptDamaged = "damaged"
	ShelfKeptUnknown = "unknown"
)

// AtariStShelfDisk is one library disk on the Atari ST shelf. Made is true
// for a disk made here, a new blank disk or a copy. KeptRevision names what
// the kit has saved for it, and may be set while Kept is "none"; pass it
// back as it is to revert, rename or delete the disk.
type AtariStShelfDisk struct {
	GameID       string `json:"game_id"`
	Title        string `json:"title"`
	PackageID    string `json:"package_id"`
	MediaID      string `json:"media_id"`
	Size         int64  `json:"size"`
	Made         bool   `json:"made"`
	Kept         string `json:"kept"`
	KeptRevision string `json:"kept_revision,omitempty"`
	InDrive      bool   `json:"in_drive"`
}

// AtariStShelf is the shelf as one kit sees it. When KeptKnown is false the
// kit could not be asked, and only the disks made here are listed.
type AtariStShelf struct {
	Target    string             `json:"target"`
	TargetID  string             `json:"target_id"`
	KeptKnown bool               `json:"kept_known"`
	Disks     []AtariStShelfDisk `json:"disks"`
}

func validShelfDisk(disk AtariStShelfDisk) bool {
	if protocol.ValidateGameID(disk.GameID) != nil || strings.TrimSpace(disk.Title) == "" ||
		protocol.ValidateDigest(disk.PackageID) != nil || protocol.ValidateDigest(disk.MediaID) != nil ||
		!protocol.AdmitAtariStFloppySize(disk.Size) {
		return false
	}
	switch disk.Kept {
	case ShelfKeptUnknown:
		return disk.KeptRevision == ""
	case ShelfKeptNone:
		// A kit saves a disk at Stop even when nothing was written.
		return disk.KeptRevision == "" || protocol.ValidateDigest(disk.KeptRevision) == nil
	case ShelfKeptChanges, ShelfKeptDamaged:
		return protocol.ValidateDigest(disk.KeptRevision) == nil
	}
	return false
}

func validShelfRequest(gameID, revision string) bool {
	return protocol.ValidateGameID(gameID) == nil && (revision == "" || protocol.ValidateDigest(revision) == nil)
}

func validShelfTitle(title string) bool {
	return strings.TrimSpace(title) != "" && len(title) <= 120
}

func shelfDiskPath(gameID, action string) string {
	return "/api/v1/library/atari-st-shelf/" + url.PathEscape(gameID) + "/" + action
}

// AtariStShelf lists the disks made here and the game disks with saved
// changes.
func (c *Client) AtariStShelf(ctx context.Context) (AtariStShelf, error) {
	var shelf AtariStShelf
	if err := c.getJSON(ctx, "/api/v1/library/atari-st-shelf", &shelf); err != nil {
		return AtariStShelf{}, err
	}
	for _, disk := range shelf.Disks {
		if !validShelfDisk(disk) || (!shelf.KeptKnown && (disk.Kept != ShelfKeptUnknown || !disk.Made)) ||
			(shelf.KeptKnown && disk.Kept == ShelfKeptUnknown) {
			return AtariStShelf{}, fmt.Errorf("ST disk shelf response is invalid")
		}
	}
	return shelf, nil
}

// NewAtariStShelfDisk makes a blank disk named title for an installed ST
// package. Zero geometry is the 720 KiB disk.
func (c *Client) NewAtariStShelfDisk(ctx context.Context, title, packageID string, tracks, heads, sectors int) (AtariStShelfDisk, error) {
	geometry, ok := atarist.Geometry(tracks, heads, sectors)
	if !ok || !validShelfTitle(title) || protocol.ValidateDigest(packageID) != nil {
		return AtariStShelfDisk{}, fmt.Errorf("new ST shelf disk request is invalid")
	}
	var disk AtariStShelfDisk
	payload := map[string]any{"title": title, "package_id": packageID, "tracks": geometry.Tracks, "heads": geometry.Heads, "sectors": geometry.Sectors}
	if err := c.doJSONStatuses(ctx, http.MethodPost, "/api/v1/library/atari-st-shelf", payload, &disk, http.StatusCreated); err != nil {
		return AtariStShelfDisk{}, err
	}
	want := int64(geometry.Tracks) * int64(geometry.Heads) * int64(geometry.Sectors) * 512
	if !validShelfDisk(disk) || !disk.Made || disk.Kept != ShelfKeptNone || disk.Size != want || disk.PackageID != packageID || disk.Title != strings.TrimSpace(title) {
		return AtariStShelfDisk{}, fmt.Errorf("new ST shelf disk response does not match the request")
	}
	return disk, nil
}

// CopyAtariStShelfDisk makes a new disk named title from a disk as it
// stands. The source is unchanged.
func (c *Client) CopyAtariStShelfDisk(ctx context.Context, gameID, title string) (AtariStShelfDisk, error) {
	if !validShelfRequest(gameID, "") || !validShelfTitle(title) {
		return AtariStShelfDisk{}, fmt.Errorf("ST shelf disk request is invalid")
	}
	var disk AtariStShelfDisk
	if err := c.doJSONStatuses(ctx, http.MethodPost, shelfDiskPath(gameID, "copy"), map[string]string{"title": title}, &disk, http.StatusCreated); err != nil {
		return AtariStShelfDisk{}, err
	}
	if !validShelfDisk(disk) || !disk.Made || disk.Kept != ShelfKeptNone || disk.GameID == gameID || disk.Title != strings.TrimSpace(title) {
		return AtariStShelfDisk{}, fmt.Errorf("ST shelf disk copy response does not match the request")
	}
	return disk, nil
}

// RevertAtariStShelfDisk discards a disk's saved changes, so it is again
// the image it was imported or made as.
func (c *Client) RevertAtariStShelfDisk(ctx context.Context, gameID, keptRevision string) (AtariStShelfDisk, error) {
	if !validShelfRequest(gameID, keptRevision) || keptRevision == "" {
		return AtariStShelfDisk{}, fmt.Errorf("ST shelf disk request is invalid")
	}
	var disk AtariStShelfDisk
	if err := c.doJSON(ctx, http.MethodPost, shelfDiskPath(gameID, "revert"), map[string]string{"expected_revision": keptRevision}, &disk); err != nil {
		return AtariStShelfDisk{}, err
	}
	if !validShelfDisk(disk) || disk.GameID != gameID || disk.Kept != ShelfKeptNone {
		return AtariStShelfDisk{}, fmt.Errorf("ST shelf disk still has saved changes")
	}
	return disk, nil
}

// RenameAtariStShelfDisk renames a disk made here. The result is a new
// library disk: its game ID differs from the one renamed.
func (c *Client) RenameAtariStShelfDisk(ctx context.Context, gameID, keptRevision, title string) (AtariStShelfDisk, error) {
	if !validShelfRequest(gameID, keptRevision) || !validShelfTitle(title) {
		return AtariStShelfDisk{}, fmt.Errorf("ST shelf disk request is invalid")
	}
	var disk AtariStShelfDisk
	payload := map[string]string{"expected_revision": keptRevision, "title": title}
	if err := c.doJSON(ctx, http.MethodPost, shelfDiskPath(gameID, "rename"), payload, &disk); err != nil {
		return AtariStShelfDisk{}, err
	}
	if !validShelfDisk(disk) || !disk.Made || disk.Title != strings.TrimSpace(title) {
		return AtariStShelfDisk{}, fmt.Errorf("ST shelf disk rename response does not match the request")
	}
	return disk, nil
}

// DeleteAtariStShelfDisk removes a disk made here together with its saved
// changes on the kit asked.
func (c *Client) DeleteAtariStShelfDisk(ctx context.Context, gameID, keptRevision string) error {
	if !validShelfRequest(gameID, keptRevision) {
		return fmt.Errorf("ST shelf disk request is invalid")
	}
	var removed struct {
		GameID string `json:"game_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, shelfDiskPath(gameID, "delete"), map[string]string{"expected_revision": keptRevision}, &removed); err != nil {
		return err
	}
	if removed.GameID != gameID {
		return fmt.Errorf("ST shelf disk delete response names another disk")
	}
	return nil
}
