package tenfoot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/shared"
)

const (
	tapePickerKindOSK     = "osk"
	tapePickerKindRoot    = "root"
	tapePickerKindParent  = "parent"
	tapePickerKindDir     = "dir"
	tapePickerKindFile    = "file"
	tapePickerKindEject   = "eject"
	tapePickerKindStarter = "starter"

	tapePickerOSKLabel   = "Type a path…"
	tapePickerEjectLabel = "Eject media"
	tapePickerTitle      = "Change media"
)

// TapePickerRow is one selectable line in the running machine media selector.
type TapePickerRow struct {
	Name       string
	Path       string
	Kind       string
	Size       int64
	Selectable bool
	Starter    *hostclient.HardwareTape
}

// TapePickerSnapshot is renderer-facing state for the running media selector.
type TapePickerSnapshot struct {
	Open    bool
	Title   string
	Current string
	Path    string
	Index   int
	Rows    []TapePickerRow
	Status  string
	Busy    bool
	Hint    string
}

func tapePickerHint(kind InputKind) string {
	return selectWord(kind) + " insert  " + detailsWord(kind) + " info  " + backWord(kind) + " back"
}

func appendTapePickerFilesystemRoot(rows []TapePickerRow, name, path string) []TapePickerRow {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || blockprobe.Skips(path) {
		return rows
	}
	for _, row := range rows {
		if row.Kind == tapePickerKindRoot && row.Path == path {
			return rows
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return rows
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(path)
	}
	return append(rows, TapePickerRow{Name: name, Path: path, Kind: tapePickerKindRoot})
}

// tapePickerVolumeRoots supplements saved storage roots with local mounts.
// Roots are included only when they exist on the device running tenfoot.
var tapePickerVolumeRoots = []string{"/Volumes", "/media", "/run/media", "/mnt/emuhd"}

func listTapePickerDir(p *hostclient.SessionCorePackage, path string) ([]TapePickerRow, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("folder path is empty")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	rows := []TapePickerRow{{
		Name: "..",
		Path: parent,
		Kind: tapePickerKindParent,
	}}
	var dirs, files []TapePickerRow
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		if blockprobe.Skips(full) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, TapePickerRow{Name: name + "/", Path: full, Kind: tapePickerKindDir})
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if !admitSessionMedia(p, name, info.Size()) {
			continue
		}
		files = append(files, TapePickerRow{
			Name:       name,
			Path:       full,
			Kind:       tapePickerKindFile,
			Size:       info.Size(),
			Selectable: true,
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	return tapePickerFolderActions(append(append(rows, dirs...), files...), !hostclient.CartridgeSwapCapable(p)), nil
}

// A linked cartridge has no eject: the machine always runs one.
func tapePickerFolderActions(rows []TapePickerRow, eject bool) []TapePickerRow {
	var actions []TapePickerRow
	if eject {
		actions = append(actions, TapePickerRow{Name: tapePickerEjectLabel, Kind: tapePickerKindEject, Selectable: true})
	}
	actions = append(actions, TapePickerRow{Name: tapePickerOSKLabel, Kind: tapePickerKindOSK, Selectable: true})
	if len(rows) != 0 && rows[0].Kind == tapePickerKindParent {
		return append(append(append([]TapePickerRow(nil), rows[:1]...), actions...), rows[1:]...)
	}
	return append(actions, rows...)
}

func tapePickerRowLabel(row TapePickerRow) string {
	switch row.Kind {
	case tapePickerKindFile:
		label := row.Name + "  ·  " + formatByteSize(row.Size)
		if row.Selectable {
			return label + "  ·  Select"
		}
		return label
	case tapePickerKindEject:
		return row.Name
	case tapePickerKindStarter:
		if row.Starter != nil && row.Starter.MediaID != "" {
			return row.Name + "  ·  Saved program"
		}
		return row.Name + "  ·  Starter tape"
	case tapePickerKindDir, tapePickerKindRoot, tapePickerKindParent:
		return row.Name
	default:
		return row.Name
	}
}

func liveMediaStatusMessage(err error) string {
	if err == nil {
		return ""
	}
	if api, ok := err.(*protocol.APIError); ok {
		msg := strings.TrimSpace(api.Message)
		if msg != "" {
			return msg
		}
	}
	return err.Error()
}

func (a *App) tapePickerSnapshotLocked() TapePickerSnapshot {
	if !a.tapePickerOpen {
		return TapePickerSnapshot{}
	}
	status := a.tapePickerStatus
	if a.roomSessionNotice != "" {
		status += " · " + a.roomSessionNotice
	}
	return TapePickerSnapshot{
		Open:    true,
		Title:   tapePickerTitle,
		Current: currentMediaLabel(a.session),
		Path:    a.pickerDisplayPathLocked(a.tapePickerPath),
		Index:   a.tapePickerIndex,
		Rows:    append([]TapePickerRow(nil), a.tapePickerRows...),
		Status:  status,
		Busy:    a.tapePickerBusy,
		Hint:    tapePickerHint(a.affinity.current.Kind),
	}
}

func (a *App) closeTapePickerLocked() {
	// Invalidate in-flight arm/eject. Home and Settings dismiss the overlay
	// without waiting, and a later success must not applySession or paint
	// Tape armed/ejected onto a stopped sofa.
	a.tapePickerGen++
	a.tapePickerOpen = false
	a.tapePickerBusy = false
	a.tapePickerStatus = ""
	a.tapePickerRows = nil
	a.tapePickerIndex = 0
	a.tapePickerPath = ""
	a.tapePickerAtRoots = false
	a.tapePickerSession = hostclient.SessionResult{}
	a.tapePickerStarters = nil
	if a.settingsOSKKind == settingsOSKTapePath {
		a.closeSettingsOSKLocked()
	}
}

// sessionMediaOfferedLocked is the media selector over a running machine: a
// live tape or disk unit, or a linked cartridge that is swapped by relaunch.
func (a *App) sessionMediaOfferedLocked() bool {
	p := a.session.CorePackage
	return a.session.State == "active" && a.liveControlsOfferedLocked() &&
		(hostclient.RemovableMediaCapable(p) || hostclient.CartridgeSwapCapable(p))
}

func (a *App) sessionLiveMediaOfferedLocked() bool {
	return a.session.State == "active" && hostclient.RemovableMediaCapable(a.session.CorePackage) && a.liveControlsOfferedLocked()
}

func (a *App) colecoCartridgeOfferedLocked() bool {
	return a.session.State == "active" && a.colecoSessionLocked() && a.liveControlsOfferedLocked() &&
		hostclient.CartridgeSwapCapable(a.session.CorePackage)
}

// sessionCartridgeLocked is a selector that changes the linked cartridge.
func (a *App) sessionCartridgeLocked() bool {
	return hostclient.CartridgeSwapCapable(a.tapePickerSession.CorePackage)
}

// admitSessionMedia is a file the running machine's selector can use.
func admitSessionMedia(p *hostclient.SessionCorePackage, name string, size int64) bool {
	return hostclient.AdmitRemovableMedia(p, name, size) || hostclient.AdmitCartridge(p, name, size)
}

func sessionMediaAdmissionError(p *hostclient.SessionCorePackage, name string) *protocol.APIError {
	if hostclient.RemovableMediaCapable(p) {
		return hostclient.RemovableMediaRequestError(p, name)
	}
	return protocol.LiveMediaRequestError()
}

func (a *App) tapePickerPromptLocked() string {
	if a.sessionCartridgeLocked() {
		return "Choose a cartridge for the running machine."
	}
	return "Choose tape or disk media for the running machine."
}

func (a *App) openTapePickerLocked() {
	if !a.sessionMediaOfferedLocked() {
		a.status = "Removable media is unavailable for this running machine."
		a.roomSessionNotice = a.status
		return
	}
	if a.tapePickerBusy || a.sessionDisplayBusy {
		return
	}
	if a.kitSessionDisplayPlaneLocked() && !a.launcherDisplayFocusedLocked() {
		a.beginSessionDisplayLocked(protocol.SessionDisplayFocused, true)
		return
	}
	a.beginColecoCartridgeControlsLocked()
	a.releasePlayHIDLocked()
	a.suppressHeldPlayKeysLocked()
	a.closeFirmwarePickerLocked()
	a.closeDetailLocked()
	a.roomChoiceOpen = false
	a.roomChoice = nil
	a.tapePickerOpen = true
	a.tapePickerSession = a.session
	a.tapePickerBusy = false
	a.tapePickerGen++
	a.refreshStorageRootsLocked()
	a.showTapePickerRootsLocked(a.tapePickerPromptLocked())
	if a.client != nil && hostclient.LiveMediaCapable(a.session.CorePackage) {
		parent := a.ctx
		if parent == nil {
			parent = context.Background()
		}
		go a.fetchLiveTapeStarters(parent, a.tapePickerGen, a.tapePickerSession.GameID)
	}
}

func (a *App) fetchLiveTapeStarters(parent context.Context, gen int, gameID string) {
	if gameID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	tapes, err := a.client.ZX81TapesForGame(ctx, gameID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil || !a.tapePickerOpen || a.tapePickerGen != gen {
		return
	}
	for _, tape := range tapes {
		if tape.ID != "" && protocol.ValidateDigest(tape.SHA256) == nil && protocol.AdmitTapeMediaName(tape.Filename) {
			a.tapePickerStarters = append(a.tapePickerStarters, tape)
		}
	}
	if a.tapePickerAtRoots {
		a.appendTapeStartersLocked()
	}
}

func (a *App) appendTapeStartersLocked() {
	for _, tape := range a.tapePickerStarters {
		copy := tape
		a.tapePickerRows = append(a.tapePickerRows, TapePickerRow{Name: tape.Name, Kind: tapePickerKindStarter, Selectable: true, Starter: &copy})
	}
}

func (a *App) showTapePickerRootsLocked(status string) {
	a.tapePickerAtRoots = true
	a.tapePickerPath = ""
	a.tapePickerRows = nil
	if !a.sessionCartridgeLocked() {
		a.tapePickerRows = append(a.tapePickerRows, TapePickerRow{Name: tapePickerEjectLabel, Kind: tapePickerKindEject, Selectable: true})
	}
	for _, r := range a.storagePickerRootsLocked("media") {
		a.tapePickerRows = append(a.tapePickerRows, TapePickerRow{Name: r.Name, Path: r.Path, Kind: r.Kind})
	}
	for _, path := range tapePickerVolumeRoots {
		a.tapePickerRows = appendTapePickerFilesystemRoot(a.tapePickerRows, filepath.Base(path), path)
	}
	a.appendTapeStartersLocked()
	a.tapePickerIndex = 0
	a.tapePickerStatus = status
	a.status = status
}

func (a *App) showTapePickerDirLocked(path, status string) {
	if path == "" {
		a.showTapePickerRootsLocked("")
		return
	}
	if _, _, _, ok := splitDrivePath(path); ok {
		a.loadDrivePickerDirLocked(path, "tape")
		return
	}
	rows, err := listTapePickerDir(a.tapePickerSession.CorePackage, path)
	if err != nil {
		a.tapePickerStatus = err.Error()
		a.status = a.tapePickerStatus
		return
	}
	a.tapePickerAtRoots = false
	a.tapePickerPath = filepath.Clean(path)
	a.tapePickerRows = rows
	a.tapePickerIndex = 0
	a.tapePickerStatus = status
	if status != "" {
		a.status = status
	}
}

func (a *App) handleTapePickerLocked(cmd Command) {
	if !a.tapePickerOpen {
		return
	}
	if cmd == CmdStop {
		a.startStopLocked()
		return
	}
	if a.tapePickerBusy {
		if cmd == CmdBack {
			a.tapePickerStatus = "Tape change in progress"
			a.status = a.tapePickerStatus
		}
		return
	}
	n := len(a.tapePickerRows)
	if n == 0 {
		if cmd == CmdBack {
			if a.tapePickerAtRoots {
				a.dismissMediaControlsLocked()
				return
			}
			a.showTapePickerRootsLocked(a.tapePickerPromptLocked())
		}
		return
	}
	if a.tapePickerIndex < 0 {
		a.tapePickerIndex = 0
	}
	if a.tapePickerIndex >= n {
		a.tapePickerIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdTabPrev:
		a.tapePickerIndex = (a.tapePickerIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdTab:
		a.tapePickerIndex = (a.tapePickerIndex + 1) % n
	case CmdBack:
		a.tapePickerBackLocked()
	case CmdDetails, CmdSearch:
		a.tapePickerDetailsLocked()
	case CmdSelect:
		a.tapePickerConfirmLocked()
	}
}

func (a *App) tapePickerBackLocked() {
	if a.tapePickerAtRoots {
		a.dismissMediaControlsLocked()
		return
	}
	path := a.tapePickerPath
	parent := filepath.Dir(path)
	if parent == path || parent == "." {
		a.showTapePickerRootsLocked(a.tapePickerPromptLocked())
		return
	}
	for _, row := range a.tapePickerRows {
		if row.Kind == tapePickerKindParent {
			a.showTapePickerDirLocked(row.Path, "")
			return
		}
	}
	a.showTapePickerDirLocked(parent, "")
}

func (a *App) tapePickerDetailsLocked() {
	row, ok := a.tapePickerRowLocked()
	if !ok {
		return
	}
	switch row.Kind {
	case tapePickerKindFile:
		if row.Selectable {
			a.tapePickerStatus = row.Name + " · Select to insert into the running machine."
		} else {
			a.tapePickerStatus = "This file does not match the running machine's media format or size limits."
		}
		a.status = a.tapePickerStatus
	case tapePickerKindEject:
		a.tapePickerStatus = "Eject the running machine's current medium."
		a.status = a.tapePickerStatus
	case tapePickerKindStarter:
		if row.Starter != nil {
			a.tapePickerStatus = row.Starter.Controls + " · " + row.Starter.License + " · " + row.Starter.SourceURL
			if row.Starter.RAMKB > 0 {
				a.tapePickerStatus = fmt.Sprintf("%d KiB RAM · %s", row.Starter.RAMKB, a.tapePickerStatus)
			}
			a.status = a.tapePickerStatus
		}
	case tapePickerKindDir, tapePickerKindRoot:
		a.showTapePickerDirLocked(row.Path, "")
	case tapePickerKindParent:
		a.tapePickerBackLocked()
	case tapePickerKindOSK:
		a.openTapePathOSKLocked()
	}
}

func (a *App) tapePickerConfirmLocked() {
	row, ok := a.tapePickerRowLocked()
	if !ok {
		return
	}
	switch row.Kind {
	case tapePickerKindOSK:
		a.openTapePathOSKLocked()
	case tapePickerKindEject:
		a.startTapeEjectLocked()
	case tapePickerKindStarter:
		if row.Starter != nil {
			a.startStarterTapeArmLocked(*row.Starter)
		}
	case tapePickerKindRoot, tapePickerKindDir:
		a.showTapePickerDirLocked(row.Path, "")
	case tapePickerKindParent:
		a.tapePickerBackLocked()
	case tapePickerKindFile:
		if !row.Selectable {
			a.tapePickerStatus = "This file does not match the running machine's media format or size limits."
			a.status = a.tapePickerStatus
			return
		}
		a.startTapeArmLocked(row.Path)
	}
}

func (a *App) tapePickerRowLocked() (TapePickerRow, bool) {
	if a.tapePickerIndex < 0 || a.tapePickerIndex >= len(a.tapePickerRows) {
		return TapePickerRow{}, false
	}
	return a.tapePickerRows[a.tapePickerIndex], true
}

func (a *App) openTapePathOSKLocked() {
	a.settingsOSKKind = settingsOSKTapePath
	a.settingsOSKIndex = 0
	a.settingsOSKIsAdd = false
	path := a.tapePickerPath
	if _, _, _, ok := splitDrivePath(path); ok {
		path = ""
	}
	a.settingsOSKField = shared.TextField{Buffer: path}
	a.settingsOSKField.OSK.Reset()
	a.settingsOSKField.OSK.CyclePage(1)
}

func (a *App) submitTapePathOSKLocked() {
	if a.settingsOSKKind != settingsOSKTapePath {
		a.closeSettingsOSKLocked()
		return
	}
	path := strings.TrimSpace(a.settingsOSKField.Buffer)
	a.closeSettingsOSKLocked()
	if path == "" {
		a.tapePickerStatus = "Tape path is empty"
		a.status = a.tapePickerStatus
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		a.tapePickerStatus = err.Error()
		a.status = a.tapePickerStatus
		return
	}
	if info.IsDir() {
		a.showTapePickerDirLocked(path, "")
		return
	}
	a.startTapeArmLocked(path)
}

func (a *App) startTapeArmLocked(path string) {
	if a.tapePickerBusy {
		return
	}
	if a.client == nil {
		a.tapePickerStatus = "host API is unavailable"
		a.status = a.tapePickerStatus
		return
	}
	if !a.sessionMediaOfferedLocked() {
		a.tapePickerStatus = protocol.LiveMediaIdentityError().Message
		a.status = a.tapePickerStatus
		return
	}
	a.tapePickerBusy = true
	a.tapePickerGen++
	gen := a.tapePickerGen
	a.tapePickerStatus = "Inserting media…"
	if a.sessionCartridgeLocked() {
		a.tapePickerStatus = "Changing cartridge…"
	}
	a.status = a.tapePickerStatus
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doTapeArm(ctx, gen, a.tapePickerSession, path, a.driveManagerLocked())
}

func (a *App) startTapeEjectLocked() {
	if a.tapePickerBusy {
		return
	}
	if a.client == nil {
		a.tapePickerStatus = "host API is unavailable"
		a.status = a.tapePickerStatus
		return
	}
	if !a.sessionMediaOfferedLocked() {
		a.tapePickerStatus = protocol.LiveMediaIdentityError().Message
		a.status = a.tapePickerStatus
		return
	}
	a.tapePickerBusy = true
	a.tapePickerGen++
	gen := a.tapePickerGen
	a.tapePickerStatus = "Ejecting media…"
	a.status = a.tapePickerStatus
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doTapeEject(ctx, gen, a.tapePickerSession)
}

func (a *App) doTapeArm(ctx context.Context, gen int, prior hostclient.SessionResult, path string, manager *storage.Manager) {
	result, replacementDispatched, err := importAndArmTape(ctx, a.client, prior, path, manager)
	a.finishTapeArm(gen, prior, result, replacementDispatched, err)
}

func (a *App) startStarterTapeArmLocked(tape hostclient.HardwareTape) {
	if a.tapePickerBusy || a.client == nil || !a.sessionMediaOfferedLocked() {
		return
	}
	a.tapePickerBusy = true
	a.tapePickerGen++
	gen, prior := a.tapePickerGen, a.tapePickerSession
	a.tapePickerStatus = "Arming " + tape.Name + "…"
	a.status = a.tapePickerStatus
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		mediaID := tape.MediaID
		if mediaID != "" && (protocol.ValidateDigest(mediaID) != nil || mediaID != tape.SHA256) {
			a.finishTapeArm(gen, prior, hostclient.SessionResult{}, false, fmt.Errorf("invalid saved cassette identity"))
			return
		}
		if mediaID == "" {
			media, err := a.client.ImportZX81Tape(ctx, tape)
			if err != nil {
				a.finishTapeArm(gen, prior, hostclient.SessionResult{}, false, err)
				return
			}
			mediaID = media.MediaID
		}
		var result hostclient.SessionResult
		replacementDispatched := true
		result, err := a.client.ReplaceRemovableMediaForSession(ctx, prior, mediaID, tape.Filename)
		a.finishTapeArm(gen, prior, result, replacementDispatched, err)
	}()
}

func (a *App) finishTapeArm(gen int, prior hostclient.SessionResult, result hostclient.SessionResult, replacementDispatched bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tapePickerGen != gen {
		return
	}
	a.tapePickerBusy = false
	// A cartridge change is a new load of the title: the reply is the new
	// generation, which a session poll may already have applied.
	cartridge := hostclient.CartridgeSwapCapable(prior.CorePackage)
	if !samePlayHIDSession(a.session, prior) && !(cartridge && err == nil && samePlayHIDSession(a.session, result)) {
		a.tapePickerStatus = "The running machine changed. Close the picker and choose tape again."
		a.status = a.tapePickerStatus
		return
	}
	if err != nil {
		if replacementDispatched && !protocol.LiveMediaNoChange(err) {
			a.session.MediaName = ""
			a.session.MediaEjected = false
		}
		a.tapePickerStatus = liveMediaStatusMessage(err)
		a.status = a.tapePickerStatus
		return
	}
	a.applySessionLocked(result)
	if cartridge {
		// The new generation retired the old plane and its focus; play
		// resumes on the new machine without a return through the plane.
		a.closeTapePickerLocked()
		a.status = "Cartridge changed."
		return
	}
	a.dismissMediaControlsLocked()
	a.status = "Media inserted."
	if hostclient.LiveMediaCapable(prior.CorePackage) {
		a.status = "Tape armed."
	}
}

func (a *App) doTapeEject(ctx context.Context, gen int, prior hostclient.SessionResult) {
	result, err := clearLiveMediaWithRetry(ctx, a.client, prior)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tapePickerGen != gen {
		return
	}
	a.tapePickerBusy = false
	if !samePlayHIDSession(a.session, prior) {
		a.tapePickerStatus = "The running machine changed. Close the picker and choose tape again."
		a.status = a.tapePickerStatus
		return
	}
	if err != nil {
		// Eject failure never stops the active session. Clear cached media state
		// only when the mutation outcome is uncertain.
		if !protocol.LiveMediaNoChange(err) {
			a.session.MediaName = ""
			a.session.MediaEjected = false
		}
		a.tapePickerStatus = liveMediaStatusMessage(err)
		a.status = a.tapePickerStatus
		return
	}
	a.applySessionLocked(result)
	a.dismissMediaControlsLocked()
	a.status = "Media ejected."
	if hostclient.LiveMediaCapable(prior.CorePackage) {
		a.status = "Tape ejected."
	}
}

func clearLiveMediaWithRetry(ctx context.Context, client *Client, prior hostclient.SessionResult) (hostclient.SessionResult, error) {
	if client == nil {
		return hostclient.SessionResult{}, fmt.Errorf("host API is unavailable")
	}
	var result hostclient.SessionResult
	var err error
	uncertain := false
	waits := []time.Duration{0, 40 * time.Millisecond, 80 * time.Millisecond}
	for _, wait := range waits {
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				if err != nil {
					return result, protocol.LiveMediaRetryError(err, uncertain)
				}
				return result, ctx.Err()
			case <-timer.C:
			}
		}
		result, err = client.ClearRemovableMediaForSession(ctx, prior)
		if err != nil && !protocol.LiveMediaNoChange(err) {
			uncertain = true
		}
		if err == nil || !ejectRetryable(err) {
			return result, protocol.LiveMediaRetryError(err, uncertain)
		}
	}
	return result, protocol.LiveMediaRetryError(err, uncertain)
}

func ejectRetryable(err error) bool {
	api, ok := err.(*protocol.APIError)
	if !ok || api == nil {
		return false
	}
	if api.Code == protocol.CodeBusy && (api.Phase == "input" || strings.Contains(strings.ToLower(api.Message), "tape loader")) {
		return true
	}
	// A phase-less or input unavailable report is retried, then kept as a
	// status line. It must not be applied as a new session.
	return api.Code == protocol.CodeMiSTerUnavailable
}

func importAndArmTape(ctx context.Context, client *Client, prior hostclient.SessionResult, path string, manager *storage.Manager) (hostclient.SessionResult, bool, error) {
	if client == nil {
		return hostclient.SessionResult{}, false, fmt.Errorf("host API is unavailable")
	}
	file, size, name, err := openPickerFile(ctx, manager, client, path, storage.MaxFileBytes)
	if err != nil {
		return hostclient.SessionResult{}, false, err
	}
	defer file.Close()
	if !admitSessionMedia(prior.CorePackage, name, size) {
		return hostclient.SessionResult{}, false, sessionMediaAdmissionError(prior.CorePackage, name)
	}
	media, err := client.ImportCoreMedia(ctx, size, io.LimitReader(file, size))
	if err != nil {
		return hostclient.SessionResult{}, false, err
	}
	if media.Size != size || !admitSessionMedia(prior.CorePackage, name, media.Size) {
		return hostclient.SessionResult{}, false, sessionMediaAdmissionError(prior.CorePackage, name)
	}
	if hostclient.CartridgeSwapCapable(prior.CorePackage) {
		result, err := client.ReplaceCartridgeForSession(ctx, prior, media.MediaID, name)
		return result, true, err
	}
	result, err := client.ReplaceRemovableMediaForSession(ctx, prior, media.MediaID, name)
	return result, true, err
}

// A selector opened from play returns directly to the captured machine.
func (a *App) dismissMediaControlsLocked() {
	direct := a.mediaControlsDirect
	a.closeTapePickerLocked()
	if direct {
		a.resumeRoomSessionLocked()
	}
}

// currentMediaLabel prefers observed unit state to the host's last confirmed
// filename. Original filenames cannot be recovered from a content digest.
func currentMediaLabel(s hostclient.SessionResult) string {
	if u, ok := hostclient.RemovableMediaUnit(s.CorePackage); ok {
		if u.State == protocol.MediaUnitEmpty {
			return "No media inserted"
		}
		if u.State == protocol.MediaUnitLoading {
			return "Media loading…"
		}
	}
	if s.MediaEjected {
		return "No media inserted"
	}
	if s.MediaName != "" {
		return "Inserted: " + s.MediaName
	}
	return "Inserted media name unavailable"
}
