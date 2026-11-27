// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

const programC64DiskSetPath = "FES/DISKSET.JSON"

type retainedC64Disk struct {
	media hostclient.SetMedia
	image []byte
}

// ProgramC64DiskSet is an immutable, startup-retained set for one catalogue
// entry. It carries no storage lease or authority to modify a core.
type ProgramC64DiskSet struct {
	entry hostclient.CoreEntry
	disks []retainedC64Disk
}

// ReadProgramC64DiskSet is used only by the opt-in startup, before the paired
// menu or a core can contain SD. The launch catalogue supplies the first disk.
func ReadProgramC64DiskSet(ctx context.Context, store *packagestore.Store, entries []ProgramEntry) (*ProgramC64DiskSet, error) {
	raw, err := store.ReadStagingRecord(ctx, programC64DiskSetPath)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		GameID string `json:"game_id"`
		Disks  []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"disks"`
	}
	if err := decodeCompleteRecord(raw, 4096, &manifest); err != nil {
		return nil, err
	}
	if len(manifest.Disks) < 1 || len(manifest.Disks) > 3 {
		return nil, errors.New("native C64 disk set requires 1 to 3 additional disks")
	}
	var entry ProgramEntry
	for _, candidate := range entries {
		if candidate.ID == manifest.GameID {
			entry = candidate
			break
		}
	}
	first := entry.Request.Tape
	if entry.ID == "" || first.Format != packagestore.C64DiskFormat || !protocol.AdmitC64DiskName(path.Base(first.Path)) {
		return nil, errors.New("native C64 disk set needs a catalogue entry with an initial D64")
	}
	requests := []packagestore.TapeRequest{first}
	names := []string{path.Base(first.Path)}
	for _, disk := range manifest.Disks {
		if disk.Name != path.Base(disk.Name) || !protocol.AdmitC64DiskName(disk.Name) {
			return nil, errors.New("native C64 disk set requires D64 basenames in FES")
		}
		for i, name := range names {
			if strings.EqualFold(disk.Name, name) || requests[i].SHA256 == disk.SHA256 {
				return nil, errors.New("native C64 disk set contains duplicate media")
			}
		}
		requests = append(requests, packagestore.TapeRequest{
			Path: "FES/" + disk.Name, Format: packagestore.C64DiskFormat,
			SHA256: disk.SHA256, Size: protocol.C64DiskBytes,
		})
		names = append(names, disk.Name)
	}
	images, err := store.ReadC64DiskSet(ctx, requests)
	if err != nil {
		return nil, err
	}
	set := &ProgramC64DiskSet{entry: hostclient.CoreEntry{
		GameID: entry.ID, PackageID: entry.Request.PackageID, MediaRole: protocol.DiskRole,
		MediaID: first.SHA256, MediaName: names[0],
	}}
	for i, image := range images {
		media := hostclient.SetMedia{MediaID: requests[i].SHA256, MediaName: names[i]}
		set.disks = append(set.disks, retainedC64Disk{media: media, image: image})
		if i != 0 {
			set.entry.MoreMedia = append(set.entry.MoreMedia, media)
		}
	}
	return set, nil
}

// AttachC64DiskSet publishes startup-retained media before the App starts.
func (b *ProgramBackend) AttachC64DiskSet(set *ProgramC64DiskSet) error {
	if set == nil || !b.operation.TryLock() {
		return ErrProgramSession
	}
	defer b.operation.Unlock()
	if b.c64Disks != nil || b.service.Status().State != ProgramReady {
		return ErrProgramSession
	}
	for _, entry := range b.entries {
		if entry.ID == set.entry.GameID && entry.Request.PackageID == set.entry.PackageID &&
			entry.Request.Tape.Format == packagestore.C64DiskFormat && entry.Request.Tape.SHA256 == set.entry.MediaID {
			b.c64Disks = set
			return nil
		}
	}
	return ErrProgramSession
}

func (b *ProgramBackend) describesC64Disk(v ProgramServiceStatus) bool {
	return b.c64Disks != nil && b.play.Game == b.c64Disks.entry.GameID &&
		v.Identity.PackageID == b.c64Disks.entry.PackageID && v.Identity.ProgrammingProfile == c64Profile &&
		int64(v.DiskBytes) == protocol.C64DiskBytes && v.State == ProgramRunning
}

func (b *ProgramBackend) describeC64Disk(v ProgramServiceStatus, pkg *hostclient.SessionCorePackage, wire map[string]any) {
	if !b.describesC64Disk(v) {
		return
	}
	iface := protocol.C64DiskInterface()
	pkg.ActiveInterfaces = append(pkg.ActiveInterfaces, hostclient.SessionCoreInterface{
		ID: iface.ID, Major: iface.Major, Minor: iface.Minor,
	})
	pkg.MediaUnits = []protocol.MediaUnitStatus{{
		Unit: protocol.C64DiskUnit, Interface: iface, MinBytes: uint32(protocol.C64DiskBytes),
		MaxBytes: uint32(protocol.C64DiskBytes), ChunkBytes: 512, State: protocol.MediaUnitReady,
	}}
	for _, disk := range b.c64Disks.disks {
		if disk.media.MediaID == v.DiskSHA256 {
			wire["media_name"] = disk.media.MediaName
			break
		}
	}
}

func (b *ProgramBackend) c64DiskRequest(r *http.Request) (*http.Response, bool, error) {
	set := b.c64Disks // configured once before publication of the frontend
	if set == nil {
		return nil, false, nil
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/core-entries/"+set.entry.GameID {
		response, err := programResponse(r, http.StatusOK, set.entry)
		return response, true, err
	}
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/session/live-media" {
		return nil, false, nil
	}
	response, err := b.replaceC64Disk(r, set)
	return response, true, err
}

func decodeC64DiskJSON(raw []byte, destination any) bool {
	if len(raw) > 4096 || !uniqueProgramJSON(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination) == nil
}

func c64DiskNoChange(r *http.Request, status int, code protocol.ErrorCode, message string) (*http.Response, error) {
	return programResponse(r, status, map[string]*protocol.APIError{
		"error": {Code: code, Message: message, NoChange: true},
	})
}

func (b *ProgramBackend) replaceC64Disk(r *http.Request, set *ProgramC64DiskSet) (*http.Response, error) {
	if !b.operation.TryLock() {
		return c64DiskNoChange(r, http.StatusConflict, protocol.CodeBusy, "another native operation is active")
	}
	defer b.operation.Unlock()
	binding, ok := protocol.DevelopmentMediaHeaders(r.Header)
	play := b.play
	session := r.Header.Values(protocol.HostSessionIDHeader)
	if !ok || len(session) != 1 || session[0] != play.ID ||
		len(r.Header.Values(protocol.HostTargetHeader)) != 1 || len(r.Header.Values(protocol.HostTargetIDHeader)) != 1 ||
		binding.PackageID != play.PackageID || binding.Generation != play.Generation ||
		binding.Target != play.Target || binding.TargetID != play.TargetID ||
		!b.describesC64Disk(b.service.Status()) {
		return c64DiskNoChange(r, http.StatusConflict, protocol.CodeBusy, "the selected C64 session has changed")
	}
	if r.Body == nil {
		return c64DiskNoChange(r, http.StatusBadRequest, protocol.CodeBadRequest, "missing D64 selection")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	var request protocol.LiveMediaRequest
	if err != nil || !decodeC64DiskJSON(raw, &request) {
		return c64DiskNoChange(r, http.StatusBadRequest, protocol.CodeBadRequest, "invalid D64 selection")
	}
	for _, disk := range set.disks {
		if disk.media.MediaID != request.MediaID || disk.media.MediaName != request.Name {
			continue
		}
		defer b.publish()
		if err := b.withInputPaused(r.Context(), func() error {
			return b.service.replaceC64Disk(r.Context(), b.session, disk)
		}); err != nil {
			return programError(r, http.StatusConflict, string(protocol.CodeMiSTerUnavailable), err.Error())
		}
		return programResponse(r, http.StatusOK, b.snapshot())
	}
	return c64DiskNoChange(r, http.StatusBadRequest, protocol.CodeBadRequest, "D64 is outside the retained disk set")
}

func (s *ProgramService) replaceC64Disk(ctx context.Context, session ProgramSession, disk retainedC64Disk) error {
	p, view, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	if !p.session(view, session) || view.Identity.ProgrammingProfile != c64Profile ||
		p.computer.port.liveMediaBatch == nil || int64(len(disk.image)) != protocol.C64DiskBytes {
		return ErrProgramSession
	}
	sum := sha256.Sum256(disk.image)
	if hex.EncodeToString(sum[:]) != disk.media.MediaID {
		return ErrProgramSession
	}
	if view.DiskSHA256 == disk.media.MediaID {
		return nil
	}
	deadline := p.limits.Now().Add(5 * time.Second)
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.computer.Computer().ReplaceC64Disk(bounded, disk.image, deadline); err != nil {
		return p.fail(err)
	}
	view.DiskSHA256, view.DiskBytes = disk.media.MediaID, len(disk.image)
	p.publish(view)
	return nil
}
