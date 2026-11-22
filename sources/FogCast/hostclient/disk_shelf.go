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

// ShelfDisk is one library disk on a family's shelf. Made is true
// for a disk made here, a new blank disk or a copy. KeptRevision names what
// the kit has saved for it, and may be set while Kept is "none"; pass it
// back as it is to revert, rename or delete the disk.
type ShelfDisk struct {
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

// DiskShelf is the shelf as one kit sees it. When KeptKnown is false the
// kit could not be asked, and only the disks made here are listed.
type DiskShelf struct {
	Target    string      `json:"target"`
	TargetID  string      `json:"target_id"`
	KeptKnown bool        `json:"kept_known"`
	Disks     []ShelfDisk `json:"disks"`
}

// Shelf families, by core ID, and what a new disk on one holds.
const (
	AtariStShelfCore     = "fes.atari-st"
	PcxtShelfCore        = "fes.pcxt-dos"
	ShelfContentsBlank   = "blank"
	ShelfContentsFreeDOS = "freedos"
)

// NewShelfDiskRequest asks for a new disk. An ST disk is blank, and zero
// geometry is the 720 KiB disk. A PC/XT disk is 360 KiB and takes no
// geometry; Contents is blank or freedos.
type NewShelfDiskRequest struct {
	Title     string
	PackageID string
	Contents  string
	Tracks    int
	Heads     int
	Sectors   int
}

// shelfDiskSize reports whether size is a disk the family's drive takes.
func shelfDiskSize(coreID string, size int64) bool {
	switch coreID {
	case AtariStShelfCore:
		return protocol.AdmitAtariStFloppySize(size)
	case PcxtShelfCore:
		return size == protocol.PcxtFloppyBytes
	}
	return false
}

func validShelfDisk(coreID string, disk ShelfDisk) bool {
	if protocol.ValidateGameID(disk.GameID) != nil || strings.TrimSpace(disk.Title) == "" ||
		protocol.ValidateDigest(disk.PackageID) != nil || protocol.ValidateDigest(disk.MediaID) != nil ||
		!shelfDiskSize(coreID, disk.Size) {
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

func shelfPath(coreID string) string {
	return "/api/v1/library/disk-shelves/" + url.PathEscape(coreID)
}

func shelfDiskPath(coreID, gameID, action string) string {
	return shelfPath(coreID) + "/" + url.PathEscape(gameID) + "/" + action
}

func shelfCore(coreID string) error {
	if coreID != AtariStShelfCore && coreID != PcxtShelfCore {
		return fmt.Errorf("%q has no disk shelf", coreID)
	}
	return nil
}

// DiskShelf lists a family's disks made here and its game disks with
// saved changes.
func (c *Client) DiskShelf(ctx context.Context, coreID string) (DiskShelf, error) {
	if err := shelfCore(coreID); err != nil {
		return DiskShelf{}, err
	}
	var shelf DiskShelf
	if err := c.libraryOperation().getJSON(ctx, shelfPath(coreID), &shelf); err != nil {
		return DiskShelf{}, err
	}
	for _, disk := range shelf.Disks {
		if !validShelfDisk(coreID, disk) || (!shelf.KeptKnown && (disk.Kept != ShelfKeptUnknown || !disk.Made)) ||
			(shelf.KeptKnown && disk.Kept == ShelfKeptUnknown) {
			return DiskShelf{}, fmt.Errorf("disk shelf response is invalid")
		}
	}
	return shelf, nil
}

// NewShelfDisk makes a new disk named req.Title for an installed package of
// the family coreID.
func (c *Client) NewShelfDisk(ctx context.Context, coreID string, req NewShelfDiskRequest) (ShelfDisk, error) {
	if err := shelfCore(coreID); err != nil || !validShelfTitle(req.Title) || protocol.ValidateDigest(req.PackageID) != nil {
		return ShelfDisk{}, fmt.Errorf("new shelf disk request is invalid")
	}
	payload := map[string]any{"title": req.Title, "package_id": req.PackageID}
	var want int64
	switch coreID {
	case AtariStShelfCore:
		geometry, ok := atarist.Geometry(req.Tracks, req.Heads, req.Sectors)
		if !ok || (req.Contents != "" && req.Contents != ShelfContentsBlank) {
			return ShelfDisk{}, fmt.Errorf("new ST shelf disk request is invalid")
		}
		payload["tracks"], payload["heads"], payload["sectors"] = geometry.Tracks, geometry.Heads, geometry.Sectors
		want = int64(geometry.Tracks) * int64(geometry.Heads) * int64(geometry.Sectors) * 512
	case PcxtShelfCore:
		if req.Tracks != 0 || req.Heads != 0 || req.Sectors != 0 || (req.Contents != ShelfContentsBlank && req.Contents != ShelfContentsFreeDOS) {
			return ShelfDisk{}, fmt.Errorf("new PC/XT shelf disk request is invalid")
		}
		payload["contents"] = req.Contents
		want = protocol.PcxtFloppyBytes
	}
	var disk ShelfDisk
	if err := c.libraryOperation().doJSONStatuses(ctx, http.MethodPost, shelfPath(coreID), payload, &disk, http.StatusCreated); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfDisk(coreID, disk) || !disk.Made || disk.Kept != ShelfKeptNone || disk.Size != want || disk.PackageID != req.PackageID || disk.Title != strings.TrimSpace(req.Title) {
		return ShelfDisk{}, fmt.Errorf("new shelf disk response does not match the request")
	}
	return disk, nil
}

// CopyShelfDisk makes a new disk named title from a disk as it
// stands. The source is unchanged.
func (c *Client) CopyShelfDisk(ctx context.Context, coreID, gameID, title string) (ShelfDisk, error) {
	if err := shelfCore(coreID); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfRequest(gameID, "") || !validShelfTitle(title) {
		return ShelfDisk{}, fmt.Errorf("shelf disk request is invalid")
	}
	var disk ShelfDisk
	if err := c.libraryOperation().doJSONStatuses(ctx, http.MethodPost, shelfDiskPath(coreID, gameID, "copy"), map[string]string{"title": title}, &disk, http.StatusCreated); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfDisk(coreID, disk) || !disk.Made || disk.Kept != ShelfKeptNone || disk.GameID == gameID || disk.Title != strings.TrimSpace(title) {
		return ShelfDisk{}, fmt.Errorf("shelf disk copy response does not match the request")
	}
	return disk, nil
}

// RevertShelfDisk discards a disk's saved changes, so it is again
// the image it was imported or made as.
func (c *Client) RevertShelfDisk(ctx context.Context, coreID, gameID, keptRevision string) (ShelfDisk, error) {
	if err := shelfCore(coreID); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfRequest(gameID, keptRevision) || keptRevision == "" {
		return ShelfDisk{}, fmt.Errorf("shelf disk request is invalid")
	}
	var disk ShelfDisk
	if err := c.libraryOperation().doJSON(ctx, http.MethodPost, shelfDiskPath(coreID, gameID, "revert"), map[string]string{"expected_revision": keptRevision}, &disk); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfDisk(coreID, disk) || disk.GameID != gameID || disk.Kept != ShelfKeptNone {
		return ShelfDisk{}, fmt.Errorf("shelf disk still has saved changes")
	}
	return disk, nil
}

// RenameShelfDisk renames a disk made here. The result is a new
// library disk: its game ID differs from the one renamed.
func (c *Client) RenameShelfDisk(ctx context.Context, coreID, gameID, keptRevision, title string) (ShelfDisk, error) {
	if err := shelfCore(coreID); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfRequest(gameID, keptRevision) || !validShelfTitle(title) {
		return ShelfDisk{}, fmt.Errorf("shelf disk request is invalid")
	}
	var disk ShelfDisk
	payload := map[string]string{"expected_revision": keptRevision, "title": title}
	if err := c.libraryOperation().doJSON(ctx, http.MethodPost, shelfDiskPath(coreID, gameID, "rename"), payload, &disk); err != nil {
		return ShelfDisk{}, err
	}
	if !validShelfDisk(coreID, disk) || !disk.Made || disk.Title != strings.TrimSpace(title) {
		return ShelfDisk{}, fmt.Errorf("shelf disk rename response does not match the request")
	}
	return disk, nil
}

// DeleteShelfDisk removes a disk made here together with its saved
// changes on the kit asked.
func (c *Client) DeleteShelfDisk(ctx context.Context, coreID, gameID, keptRevision string) error {
	if err := shelfCore(coreID); err != nil {
		return err
	}
	if !validShelfRequest(gameID, keptRevision) {
		return fmt.Errorf("shelf disk request is invalid")
	}
	var removed struct {
		GameID string `json:"game_id"`
	}
	if err := c.libraryOperation().doJSON(ctx, http.MethodPost, shelfDiskPath(coreID, gameID, "delete"), map[string]string{"expected_revision": keptRevision}, &removed); err != nil {
		return err
	}
	if removed.GameID != gameID {
		return fmt.Errorf("shelf disk delete response names another disk")
	}
	return nil
}
