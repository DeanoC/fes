package tenfoot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/shared"
)

const (
	tapePickerKindOSK    = "osk"
	tapePickerKindRoot   = "root"
	tapePickerKindParent = "parent"
	tapePickerKindDir    = "dir"
	tapePickerKindFile   = "file"
	tapePickerKindEject  = "eject"

	tapePickerOSKLabel   = "Type a path…"
	tapePickerEjectLabel = "Eject tape"
	tapePickerTitle      = "Load tape"
)

// TapePickerRow is one pad-selectable line in the mid-session Load-tape overlay.
type TapePickerRow struct {
	Name       string
	Path       string
	Kind       string
	Size       int64
	Selectable bool
}

// TapePickerSnapshot is renderer-facing overlay state for ZX81 Load-tape.
type TapePickerSnapshot struct {
	Open   bool
	Title  string
	Path   string
	Index  int
	Rows   []TapePickerRow
	Status string
	Busy   bool
	Hint   string
}

func tapeSelectable(name string, size int64, kind string) bool {
	return kind == tapePickerKindFile && protocol.AdmitTapeMediaName(name) && protocol.AdmitTapeMediaSize(size)
}

func tapePickerHint(kind InputKind) string {
	return selectWord(kind) + " arm  " + detailsWord(kind) + " info  " + backWord(kind) + " back"
}

func openTapeFile(path string) (*os.File, int64, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, 0, "", fmt.Errorf("tape path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, "", fmt.Errorf("tape path is not a regular file")
	}
	base := filepath.Base(path)
	if !protocol.AdmitTapeMediaName(base) {
		return nil, 0, "", fmt.Errorf("tape must be a .p / .P file")
	}
	if !protocol.AdmitTapeMediaSize(info.Size()) {
		return nil, 0, "", fmt.Errorf("tape must be 1..%d bytes (this file is %d)", protocol.MaxDevelopmentMediaBytes, info.Size())
	}
	ok = true
	return file, info.Size(), base, nil
}

func tapePickerRoots(home string, libraries []hostclient.LibraryRoot) []TapePickerRow {
	rows := []TapePickerRow{
		{Name: tapePickerEjectLabel, Kind: tapePickerKindEject, Selectable: true},
		{Name: tapePickerOSKLabel, Kind: tapePickerKindOSK},
	}
	seen := map[string]bool{}
	addRoot := func(name, path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || seen[path] {
			return
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return
		}
		seen[path] = true
		if strings.TrimSpace(name) == "" {
			name = filepath.Base(path)
		}
		rows = append(rows, TapePickerRow{
			Name: name,
			Path: path,
			Kind: tapePickerKindRoot,
		})
	}
	addRoot("Home", home)
	for _, lib := range libraries {
		label := strings.TrimSpace(lib.System)
		if label == "" {
			label = strings.TrimSpace(lib.ID)
		}
		if label == "" {
			label = "Library"
		} else {
			label = "Library · " + label
		}
		addRoot(label, lib.Root)
	}
	for _, extra := range []string{"/Volumes", "/media", "/run/media"} {
		addRoot(filepath.Base(extra), extra)
	}
	return rows
}

func listTapePickerDir(path string) ([]TapePickerRow, error) {
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
		files = append(files, TapePickerRow{
			Name:       name,
			Path:       full,
			Kind:       tapePickerKindFile,
			Size:       info.Size(),
			Selectable: tapeSelectable(name, info.Size(), tapePickerKindFile),
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	return append(append(rows, dirs...), files...), nil
}

func tapePickerRowLabel(row TapePickerRow) string {
	switch row.Kind {
	case tapePickerKindFile:
		label := row.Name + "  ·  " + formatByteSize(row.Size)
		if row.Selectable {
			return label + "  ·  ZX81 tape"
		}
		return label
	case tapePickerKindEject:
		return row.Name
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
	return TapePickerSnapshot{
		Open:   true,
		Title:  tapePickerTitle,
		Path:   a.tapePickerPath,
		Index:  a.tapePickerIndex,
		Rows:   append([]TapePickerRow(nil), a.tapePickerRows...),
		Status: a.tapePickerStatus,
		Busy:   a.tapePickerBusy,
		Hint:   tapePickerHint(a.affinity.current.Kind),
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
	if a.settingsOSKKind == settingsOSKTapePath {
		a.closeSettingsOSKLocked()
	}
}

func (a *App) sessionLiveMediaOfferedLocked() bool {
	return a.session.State == "active" && hostclient.LiveMediaCapable(a.session.CorePackage)
}

func (a *App) openTapePickerLocked() {
	if !a.sessionLiveMediaOfferedLocked() {
		return
	}
	a.closeFirmwarePickerLocked()
	a.closeDetailLocked()
	a.roomChoiceOpen = false
	a.roomChoice = nil
	a.tapePickerOpen = true
	a.tapePickerBusy = false
	a.tapePickerGen++
	a.showTapePickerRootsLocked("Choose a .p tape to arm on the running ZX81.")
}

func (a *App) showTapePickerRootsLocked(status string) {
	home, _ := os.UserHomeDir()
	a.tapePickerAtRoots = true
	a.tapePickerPath = ""
	a.tapePickerRows = tapePickerRoots(home, a.hostSettings.Libraries)
	a.tapePickerIndex = 0
	a.tapePickerStatus = status
	a.status = status
}

func (a *App) showTapePickerDirLocked(path, status string) {
	rows, err := listTapePickerDir(path)
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
	if a.tapePickerBusy {
		if cmd == CmdBack {
			a.tapePickerStatus = "Tape arm in progress"
			a.status = a.tapePickerStatus
		}
		return
	}
	n := len(a.tapePickerRows)
	if n == 0 {
		if cmd == CmdBack {
			if a.tapePickerAtRoots {
				a.closeTapePickerLocked()
				return
			}
			a.showTapePickerRootsLocked("Choose a .p tape to arm on the running ZX81.")
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
		a.closeTapePickerLocked()
		return
	}
	path := a.tapePickerPath
	parent := filepath.Dir(path)
	if parent == path || parent == "." {
		a.showTapePickerRootsLocked("Choose a .p tape to arm on the running ZX81.")
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
			a.tapePickerStatus = row.Name + " is a ZX81 .p tape. Confirm to arm the deck."
		} else if !protocol.AdmitTapeMediaName(row.Name) {
			a.tapePickerStatus = row.Name + " is not a .p / .P tape."
		} else {
			a.tapePickerStatus = fmt.Sprintf("%s is %d bytes. Tape must be 1..%d bytes.", row.Name, row.Size, protocol.MaxDevelopmentMediaBytes)
		}
		a.status = a.tapePickerStatus
	case tapePickerKindEject:
		a.tapePickerStatus = "Clear the armed mailbox so the next empty LOAD \"\" is 0/0."
		a.status = a.tapePickerStatus
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
	case tapePickerKindRoot, tapePickerKindDir:
		a.showTapePickerDirLocked(row.Path, "")
	case tapePickerKindParent:
		a.tapePickerBackLocked()
	case tapePickerKindFile:
		if !row.Selectable {
			if !protocol.AdmitTapeMediaName(row.Name) {
				a.tapePickerStatus = "Tape must be a .p / .P file."
			} else {
				a.tapePickerStatus = fmt.Sprintf("Tape must be 1..%d bytes (this file is %d).", protocol.MaxDevelopmentMediaBytes, row.Size)
			}
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
	a.settingsOSKField = shared.TextField{Buffer: a.tapePickerPath}
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
	if !a.sessionLiveMediaOfferedLocked() {
		a.tapePickerStatus = protocol.LiveMediaIdentityError().Message
		a.status = a.tapePickerStatus
		return
	}
	a.tapePickerBusy = true
	a.tapePickerGen++
	gen := a.tapePickerGen
	a.tapePickerStatus = "Arming tape…"
	a.status = a.tapePickerStatus
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doTapeArm(ctx, gen, path)
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
	if !a.sessionLiveMediaOfferedLocked() {
		a.tapePickerStatus = protocol.LiveMediaIdentityError().Message
		a.status = a.tapePickerStatus
		return
	}
	a.tapePickerBusy = true
	a.tapePickerGen++
	gen := a.tapePickerGen
	a.tapePickerStatus = "Ejecting tape…"
	a.status = a.tapePickerStatus
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doTapeEject(ctx, gen)
}

func (a *App) doTapeArm(ctx context.Context, gen int, path string) {
	result, err := importAndArmTape(ctx, a.client, path)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tapePickerGen != gen {
		return
	}
	a.tapePickerBusy = false
	if err != nil {
		a.tapePickerStatus = liveMediaStatusMessage(err)
		a.status = a.tapePickerStatus
		return
	}
	a.applySessionLocked(result)
	a.closeTapePickerLocked()
	a.status = "Tape armed."
}

func (a *App) doTapeEject(ctx context.Context, gen int) {
	result, err := a.client.ClearLiveMedia(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tapePickerGen != gen {
		return
	}
	a.tapePickerBusy = false
	if err != nil {
		a.tapePickerStatus = liveMediaStatusMessage(err)
		a.status = a.tapePickerStatus
		return
	}
	a.applySessionLocked(result)
	a.closeTapePickerLocked()
	a.status = "Tape ejected."
}

func importAndArmTape(ctx context.Context, client *Client, path string) (hostclient.SessionResult, error) {
	if client == nil {
		return hostclient.SessionResult{}, fmt.Errorf("host API is unavailable")
	}
	file, size, name, err := openTapeFile(path)
	if err != nil {
		return hostclient.SessionResult{}, err
	}
	defer file.Close()
	media, err := client.ImportCoreMedia(ctx, size, io.LimitReader(file, size))
	if err != nil {
		return hostclient.SessionResult{}, err
	}
	if !protocol.AdmitTapeMediaSize(media.Size) {
		return hostclient.SessionResult{}, protocol.LiveMediaRequestError()
	}
	return client.ReplaceLiveMedia(ctx, media.MediaID, name)
}
