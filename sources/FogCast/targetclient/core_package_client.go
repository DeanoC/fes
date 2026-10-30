package targetclient

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// InspectCore sends one bounded package to the target runtime's read-only
// compatibility authority. It deliberately does not authorize a kit mutation.
func (c *Client) InspectCore(ctx context.Context, size int64, content io.Reader) (protocol.CoreInspection, error) {
	if size < 1 || size > corepackage.MaxArchiveSize || content == nil {
		return protocol.CoreInspection{}, fmt.Errorf("development core input is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint("/v1/development/core/inspect", nil).String(), readOnlyReader{Reader: content})
	if err != nil {
		return protocol.CoreInspection{}, fmt.Errorf("create request: %w", err)
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return protocol.CoreInspection{}, errors.Join(&protocol.APIError{
			Code: protocol.CodeTransferFailed, Message: "core package inspection transfer failed"}, err)
	}
	defer response.Body.Close()
	var inspection protocol.CoreInspection
	if err := decodeResponse(response, &inspection); err != nil {
		return protocol.CoreInspection{}, err
	}
	if !validCoreInspection(inspection) {
		return protocol.CoreInspection{}, fmt.Errorf("development core inspection response is invalid")
	}
	return inspection, nil
}

func validCoreInspection(inspection protocol.CoreInspection) bool {
	if !lowerHex(inspection.PackageID, 64) || corepackage.ValidateDescriptor(inspection.Descriptor) != nil {
		return false
	}
	if inspection.Compatible {
		return inspection.CompatibilityError == nil
	}
	return inspection.CompatibilityError != nil && inspection.CompatibilityError.Code != "" &&
		inspection.CompatibilityError.Message != "" && inspection.CompatibilityError.Phase == "compatibility"
}

// LoadCore sends one bounded package mutation through the client's shared kit
// lease and accepts only a complete custom-development status.
func (c *Client) LoadCore(ctx context.Context, size int64, content io.Reader) (protocol.Status, error) {
	return c.loadCore(ctx, size, content, "")
}
func (c *Client) loadCore(ctx context.Context, size int64, content io.Reader, libraryID string, composition ...bool) (protocol.Status, error) {
	return c.loadCoreWithGameID(ctx, size, content, libraryID, "", composition...)
}
func (c *Client) loadCoreWithGameID(ctx context.Context, size int64, content io.Reader, libraryID, gameID string, composition ...bool) (protocol.Status, error) {
	return c.loadCoreWithGameIDAndAtaDisk(ctx, size, content, libraryID, gameID, nil, nil, composition...)
}

// LoadLibraryCoreWithAtaDisk sends the selected base image before the package
// bytes; the target API stages it locally and never accepts a caller path.
func (c *Client) LoadLibraryCoreWithAtaDisk(ctx context.Context, size int64, content io.Reader,
	id, gameID string, disk protocol.AtaDiskSelection, diskContent io.Reader) (protocol.Status, error) {
	if id == "" || protocol.ValidateGameID(gameID) != nil || !disk.Valid() || diskContent == nil {
		return protocol.Status{}, fmt.Errorf("library ATA disk request is invalid")
	}
	return c.loadCoreWithGameIDAndAtaDisk(ctx, size, content, id, gameID, &disk, diskContent)
}

func (c *Client) loadCoreWithGameIDAndAtaDisk(ctx context.Context, size int64, content io.Reader,
	libraryID, gameID string, ataDisk *protocol.AtaDiskSelection, diskContent io.Reader, composition ...bool) (protocol.Status, error) {
	composed := len(composition) == 1 && composition[0]
	parts := len(composition) == 2 && composition[1]
	limit := int64(corepackage.MaxROMInputSize)
	if composed {
		limit = corepackage.MaxCompositionArchiveSize
	}
	if parts {
		limit = corepackage.MaxPartsArchiveSize
	}
	if size < 1 || size > limit || content == nil ||
		(ataDisk != nil && (!ataDisk.Valid() || int64(ataDisk.Size) < protocol.AtaDiskMinBytes || diskContent == nil || libraryID == "" || protocol.ValidateGameID(gameID) != nil)) {
		return protocol.Status{}, fmt.Errorf("development core input is invalid")
	}
	path := "/v1/development/core"
	if libraryID != "" {
		path = "/v1/library/core/load"
	}
	if composed {
		path = "/v1/library/core/compose"
	}
	if parts {
		path = "/v1/library/core/parts"
	}
	var body io.Reader = readOnlyReader{Reader: content}
	contentLength := size
	if ataDisk != nil {
		body = io.MultiReader(readOnlyReader{Reader: diskContent}, readOnlyReader{Reader: content})
		contentLength += int64(ataDisk.Size)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path, nil).String(), body)
	if err != nil {
		return protocol.Status{}, fmt.Errorf("create request: %w", err)
	}
	request.ContentLength = contentLength
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")
	if libraryID != "" {
		request.Header.Set("X-FogCast-Package-ID", libraryID)
	}
	if gameID != "" {
		request.Header.Set("X-FogCast-Game-ID", gameID)
	}
	if ataDisk != nil {
		request.Header.Set(protocol.CorePackageSizeHeader, strconv.FormatInt(size, 10))
		request.Header.Set(protocol.AtaDiskSizeHeader, strconv.FormatUint(uint64(ataDisk.Size), 10))
		request.Header.Set(protocol.AtaDiskBaseIDHeader, ataDisk.BaseMediaID)
		request.Header.Set(protocol.AtaDiskCylindersHeader, strconv.FormatUint(uint64(ataDisk.Cylinders), 10))
		request.Header.Set(protocol.AtaDiskHeadsHeader, strconv.FormatUint(uint64(ataDisk.Heads), 10))
		request.Header.Set(protocol.AtaDiskSectorsHeader, strconv.FormatUint(uint64(ataDisk.SectorsPerTrack), 10))
	}
	if err := c.authorizeMutation(request); err != nil {
		return protocol.Status{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return protocol.Status{}, newTransferError(err)
	}
	defer response.Body.Close()
	var status protocol.Status
	if err := decodeResponse(response, &status); err != nil {
		return protocol.Status{}, err
	}
	singleComposed, slotComposed := status.CorePackage != nil && status.CorePackage.Composition != nil, status.CorePackage != nil && status.CorePackage.SlotComposition != nil
	// A ROM input composes on the target, so its reply may carry a composition.
	romLinked := status.CorePackage != nil && (status.CorePackage.ROMLink != nil || status.CorePackage.ROMLinks != nil)
	if !validCorePackageStatus(status) || (parts && (status.CorePackage.PartsComposition == nil || romLinked)) || (!parts && status.CorePackage.PartsComposition != nil && !romLinked) || (composed && singleComposed == slotComposed) || (!composed && !parts && (singleComposed || slotComposed) && !romLinked) || (libraryID != "" && (status.CorePackage.PackageID != libraryID || (status.CorePackage.PersistenceMode != "persistent" && status.CorePackage.PersistenceMode != "volatile"))) {
		return protocol.Status{}, fmt.Errorf("development core response does not match requested load")
	}
	// A physical load can finish after lease expiry has scheduled target cleanup.
	// Its valid active reply is not authority to publish a new host play.
	if c.kitLease != nil && !c.kitLease.ownsCurrentGrant(request.Header.Get(KitLeaseHeader)) {
		return protocol.Status{}, errors.Join(ErrKitLeaseLost, &protocol.APIError{
			Code: protocol.CodeKitLeaseDenied, Message: "kit lease lost during core activation", Phase: "recovery"})
	}
	return status, nil
}

var romStatusIDRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)

func validCorePackageStatus(status protocol.Status) bool {
	if status.State != protocol.StateActive || !status.Development || status.GameID != nil ||
		status.System != nil || status.ExpectedCore != nil || status.LastError != nil ||
		status.Recovery != "" || status.CorePackage == nil {
		return false
	}
	value := status.CorePackage
	if protocol.ComputerMediaABI(value.ABI.ID, int64(value.ABI.Major), int64(value.ABI.Minor)) &&
		value.PersistenceMode == "persistent" && !protocol.PersistentMediaStatusValid(value) {
		return false
	}
	if r := value.ROMLink; r != nil {
		if !romStatusIDRE.MatchString(r.ROMID) || !lowerHex(r.MapSHA256, 64) || !lowerHex(r.SourceSHA256, 64) || !lowerHex(r.ProgrammedSHA256, 64) || r.SourceSize < 1024 || r.SourceSize > 256<<10 || r.SourceSize%1024 != 0 || r.ProgrammedSize < 1 || r.ProgrammedSize > corepackage.MaxPayloadSize {
			return false
		}
	}
	if value.MediaStream != nil && !protocol.MediaStreamCapable(value) {
		return false
	}
	if value.Composition != nil && value.SlotComposition != nil {
		return false
	}
	if c := value.PartsComposition; c != nil {
		if value.Composition != nil || value.SlotComposition != nil || !validPartsStatus(*c, value.PackageID) {
			return false
		}
		// The ST shell links its firmware ROM (format 3). Coleco shells run
		// ROM-less (format 2) or with the linked BIOS and cartridge (format 4).
		if c.Layout == expansion.AtariStVideoLayout {
			bound := protocol.MediaDataBound(value)
			persistence := (value.PersistenceMode == "volatile" && !bound) || (value.PersistenceMode == "persistent" && bound)
			if !protocol.ComputerMediaABI(value.ABI.ID, int64(value.ABI.Major), int64(value.ABI.Minor)) ||
				value.ROMLink == nil || value.ROMLinks != nil || !persistence {
				return false
			}
		} else if value.ROMLink != nil || (value.ROMLinks != nil && c.Layout != expansion.ColecoNativeVideoLayout && c.Layout != expansion.ColecoVideoLayout) ||
			value.PersistenceMode != "volatile" || value.ABI != (protocol.RuntimeContract{ID: "fes.application", Major: 1}) {
			return false
		}
	}
	if c := value.SlotComposition; c != nil {
		if id, err := expansion.SlotCompositionID(c.PackageID, c.Expansions, c.PayloadSHA256); err != nil || id != c.ID || c.PackageID != value.PackageID {
			return false
		}
	}
	for index, unit := range value.MediaUnits {
		if !unit.Valid() || (index > 0 && value.MediaUnits[index-1].Unit >= unit.Unit) ||
			(unit.Interface == protocol.AtaDiskInterface() && !protocol.ComputerAtaDiskABI(value.ABI.ID, int64(value.ABI.Major), int64(value.ABI.Minor))) ||
			(unit.Persistence != nil && (value.PersistenceMode != "persistent" || !protocol.PersistentMediaUnitCapable(value, unit))) {
			return false
		}
	}
	if !lowerHex(value.PackageID, 64) || value.Generation == 0 || value.ABI.ID == "" ||
		value.ABI.Major == 0 || !lowerHex(value.BuildID, 32) ||
		!sort.SliceIsSorted(value.ActiveInterfaces, func(i, j int) bool {
			return value.ActiveInterfaces[i].ID < value.ActiveInterfaces[j].ID
		}) {
		return false
	}
	gamepad := false
	for index, contract := range value.ActiveInterfaces {
		if contract.ID == "" || contract.Major == 0 ||
			(index > 0 && value.ActiveInterfaces[index-1].ID == contract.ID) {
			return false
		}
		if (contract.ID == "fes.gamepad" || contract.ID == "fes.gamepad.ports") && contract.Major == 1 && contract.Minor == 0 {
			gamepad = true
		}
	}
	return value.Gamepad == gamepad
}

func lowerHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != size/2 {
		return false
	}
	for _, char := range value {
		if char >= 'A' && char <= 'F' {
			return false
		}
	}
	return true
}

func (c *Client) LoadComposedCore(ctx context.Context, size int64, body io.Reader, id string) (protocol.Status, error) {
	if !lowerHex(id, 64) {
		return protocol.Status{}, fmt.Errorf("invalid package identity")
	}
	return c.loadCore(ctx, size, body, id, true)
}

// LoadLibraryPartsCore retains library core-data context while selecting the
// closed video composition. The target independently admits all bytes.
func (c *Client) LoadLibraryPartsCore(ctx context.Context, size int64, body io.Reader, id string) (protocol.Status, error) {
	if !lowerHex(id, 64) {
		return protocol.Status{}, fmt.Errorf("invalid package identity")
	}
	return c.loadCore(ctx, size, body, id, true, true)
}

func validPartsStatus(c expansion.PartsComposition, id string) bool {
	calculated, err := expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
	if err != nil || calculated != c.ID || c.PackageID != id || !lowerHex(c.ShellSHA256, 64) || c.PayloadSize < 40408 || c.PayloadSize > corepackage.MaxPayloadSize {
		return false
	}
	for i, part := range c.Parts {
		if i > 0 && c.Parts[i-1].Role >= part.Role {
			return false
		}
	}
	return true
}
