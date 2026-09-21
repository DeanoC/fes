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
	firmwarePickerKindOSK    = "osk"
	firmwarePickerKindRoot   = "root"
	firmwarePickerKindParent = "parent"
	firmwarePickerKindDir    = "dir"
	firmwarePickerKindFile   = "file"

	firmwarePickerOSKLabel = "Type a path…"
	firmwarePickerTitle    = "Import Coleco BIOS"
)

// FirmwarePickerRow is one pad-selectable line in the household BIOS overlay.
type FirmwarePickerRow struct {
	Name       string
	Path       string
	Kind       string
	Size       int64
	Selectable bool
}

// FirmwarePickerSnapshot is renderer-facing overlay state for BIOS import.
type FirmwarePickerSnapshot struct {
	Open   bool
	Title  string
	Path   string
	Index  int
	Rows   []FirmwarePickerRow
	Status string
	Busy   bool
	Hint   string
}

func colecoBIOSSelectable(size int64, kind string) bool {
	return kind == firmwarePickerKindFile && size == protocol.FirmwareBytes
}

func firmwarePickerHint(kind InputKind) string {
	return selectWord(kind) + " import  " + detailsWord(kind) + " info  " + backWord(kind) + " back"
}

func openColecoBIOSFile(path string) (*os.File, int64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, 0, fmt.Errorf("Coleco BIOS path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("Coleco BIOS path is not a regular file")
	}
	if info.Size() != protocol.FirmwareBytes {
		return nil, 0, fmt.Errorf("Coleco BIOS must be exactly %d bytes (this file is %d)", protocol.FirmwareBytes, info.Size())
	}
	ok = true
	return file, info.Size(), nil
}

func firmwarePickerRoots(home string, libraries []hostclient.LibraryRoot) []FirmwarePickerRow {
	rows := []FirmwarePickerRow{{
		Name: firmwarePickerOSKLabel,
		Kind: firmwarePickerKindOSK,
	}}
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
		rows = append(rows, FirmwarePickerRow{
			Name: name,
			Path: path,
			Kind: firmwarePickerKindRoot,
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

func listFirmwarePickerDir(path string) ([]FirmwarePickerRow, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, fmt.Errorf("folder path is empty")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	rows := []FirmwarePickerRow{{
		Name: "..",
		Path: parent,
		Kind: firmwarePickerKindParent,
	}}
	var dirs, files []FirmwarePickerRow
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
			dirs = append(dirs, FirmwarePickerRow{Name: name + "/", Path: full, Kind: firmwarePickerKindDir})
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, FirmwarePickerRow{
			Name:       name,
			Path:       full,
			Kind:       firmwarePickerKindFile,
			Size:       info.Size(),
			Selectable: colecoBIOSSelectable(info.Size(), firmwarePickerKindFile),
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	return append(append(rows, dirs...), files...), nil
}

func firmwarePickerRowLabel(row FirmwarePickerRow) string {
	switch row.Kind {
	case firmwarePickerKindFile:
		label := row.Name + "  ·  " + formatByteSize(row.Size)
		if row.Selectable {
			return label + "  ·  Coleco BIOS"
		}
		return label
	case firmwarePickerKindDir, firmwarePickerKindRoot, firmwarePickerKindParent:
		return row.Name
	default:
		return row.Name
	}
}

func formatByteSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d bytes", size)
	}
	if size%(1024*1024) == 0 {
		return fmt.Sprintf("%d MiB", size/(1024*1024))
	}
	if size%1024 == 0 {
		return fmt.Sprintf("%d KiB", size/1024)
	}
	return fmt.Sprintf("%d bytes", size)
}

func (a *App) firmwarePickerSnapshotLocked() FirmwarePickerSnapshot {
	if !a.firmwarePickerOpen {
		return FirmwarePickerSnapshot{}
	}
	return FirmwarePickerSnapshot{
		Open:   true,
		Title:  firmwarePickerTitle,
		Path:   a.firmwarePickerPath,
		Index:  a.firmwarePickerIndex,
		Rows:   append([]FirmwarePickerRow(nil), a.firmwarePickerRows...),
		Status: a.firmwarePickerStatus,
		Busy:   a.firmwarePickerBusy,
		Hint:   firmwarePickerHint(a.affinity.current.Kind),
	}
}

func (a *App) closeFirmwarePickerLocked() {
	a.firmwarePickerOpen = false
	a.firmwarePickerBusy = false
	a.firmwarePickerStatus = ""
	a.firmwarePickerRows = nil
	a.firmwarePickerIndex = 0
	a.firmwarePickerPath = ""
	a.firmwarePickerAtRoots = false
	if a.settingsOSKKind == settingsOSKFirmwarePath {
		a.closeSettingsOSKLocked()
	}
}

func (a *App) openFirmwarePickerLocked(game hostclient.Game) {
	a.closeDetailLocked()
	a.roomChoiceOpen = false
	a.roomChoice = nil
	a.roomDetail = hostclient.Game{}
	a.firmwarePickerGame = game
	a.firmwarePickerOpen = true
	a.firmwarePickerBusy = false
	a.firmwarePickerGen++
	a.showFirmwarePickerRootsLocked("Choose an 8192-byte Coleco BIOS.")
}

func (a *App) showFirmwarePickerRootsLocked(status string) {
	home, _ := os.UserHomeDir()
	a.firmwarePickerAtRoots = true
	a.firmwarePickerPath = ""
	a.firmwarePickerRows = firmwarePickerRoots(home, a.hostSettings.Libraries)
	a.firmwarePickerIndex = 0
	a.firmwarePickerStatus = status
	a.status = status
}

func (a *App) showFirmwarePickerDirLocked(path, status string) {
	rows, err := listFirmwarePickerDir(path)
	if err != nil {
		a.firmwarePickerStatus = err.Error()
		a.status = a.firmwarePickerStatus
		return
	}
	a.firmwarePickerAtRoots = false
	a.firmwarePickerPath = filepath.Clean(path)
	a.firmwarePickerRows = rows
	a.firmwarePickerIndex = 0
	a.firmwarePickerStatus = status
	if status != "" {
		a.status = status
	}
}

func (a *App) handleFirmwarePickerLocked(cmd Command) {
	if !a.firmwarePickerOpen {
		return
	}
	if a.firmwarePickerBusy {
		if cmd == CmdBack {
			a.firmwarePickerStatus = "Coleco BIOS import in progress"
			a.status = a.firmwarePickerStatus
		}
		return
	}
	n := len(a.firmwarePickerRows)
	if n == 0 {
		if cmd == CmdBack {
			if a.firmwarePickerAtRoots {
				a.closeFirmwarePickerLocked()
				return
			}
			a.showFirmwarePickerRootsLocked("Choose an 8192-byte Coleco BIOS.")
		}
		return
	}
	if a.firmwarePickerIndex < 0 {
		a.firmwarePickerIndex = 0
	}
	if a.firmwarePickerIndex >= n {
		a.firmwarePickerIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdTabPrev:
		a.firmwarePickerIndex = (a.firmwarePickerIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdTab:
		a.firmwarePickerIndex = (a.firmwarePickerIndex + 1) % n
	case CmdBack:
		a.firmwarePickerBackLocked()
	case CmdDetails, CmdSearch:
		a.firmwarePickerDetailsLocked()
	case CmdSelect:
		a.firmwarePickerConfirmLocked()
	}
}

func (a *App) firmwarePickerBackLocked() {
	if a.firmwarePickerAtRoots {
		a.closeFirmwarePickerLocked()
		return
	}
	path := a.firmwarePickerPath
	parent := filepath.Dir(path)
	if parent == path || parent == "." {
		a.showFirmwarePickerRootsLocked("Choose an 8192-byte Coleco BIOS.")
		return
	}
	for _, row := range a.firmwarePickerRows {
		if row.Kind == firmwarePickerKindParent {
			a.showFirmwarePickerDirLocked(row.Path, "")
			return
		}
	}
	a.showFirmwarePickerDirLocked(parent, "")
}

func (a *App) firmwarePickerDetailsLocked() {
	row, ok := a.firmwarePickerRowLocked()
	if !ok {
		return
	}
	switch row.Kind {
	case firmwarePickerKindFile:
		if row.Selectable {
			a.firmwarePickerStatus = row.Name + " is an 8192-byte Coleco BIOS. Confirm to import."
		} else {
			a.firmwarePickerStatus = fmt.Sprintf("%s is %d bytes. Coleco BIOS must be exactly %d bytes.", row.Name, row.Size, protocol.FirmwareBytes)
		}
		a.status = a.firmwarePickerStatus
	case firmwarePickerKindDir, firmwarePickerKindRoot:
		a.showFirmwarePickerDirLocked(row.Path, "")
	case firmwarePickerKindParent:
		a.firmwarePickerBackLocked()
	case firmwarePickerKindOSK:
		a.openFirmwarePathOSKLocked()
	}
}

func (a *App) firmwarePickerConfirmLocked() {
	row, ok := a.firmwarePickerRowLocked()
	if !ok {
		return
	}
	switch row.Kind {
	case firmwarePickerKindOSK:
		a.openFirmwarePathOSKLocked()
	case firmwarePickerKindRoot, firmwarePickerKindDir:
		a.showFirmwarePickerDirLocked(row.Path, "")
	case firmwarePickerKindParent:
		a.firmwarePickerBackLocked()
	case firmwarePickerKindFile:
		if !row.Selectable {
			a.firmwarePickerStatus = fmt.Sprintf("Coleco BIOS must be exactly %d bytes (this file is %d).", protocol.FirmwareBytes, row.Size)
			a.status = a.firmwarePickerStatus
			return
		}
		a.startFirmwareImportLocked(row.Path)
	}
}

func (a *App) firmwarePickerRowLocked() (FirmwarePickerRow, bool) {
	if a.firmwarePickerIndex < 0 || a.firmwarePickerIndex >= len(a.firmwarePickerRows) {
		return FirmwarePickerRow{}, false
	}
	return a.firmwarePickerRows[a.firmwarePickerIndex], true
}

func (a *App) openFirmwarePathOSKLocked() {
	a.settingsOSKKind = settingsOSKFirmwarePath
	a.settingsOSKIndex = 0
	a.settingsOSKIsAdd = false
	a.settingsOSKField = shared.TextField{Buffer: a.firmwarePickerPath}
	a.settingsOSKField.OSK.Reset()
	a.settingsOSKField.OSK.CyclePage(1)
}

func (a *App) submitFirmwarePathOSKLocked() {
	if a.settingsOSKKind != settingsOSKFirmwarePath {
		a.closeSettingsOSKLocked()
		return
	}
	path := strings.TrimSpace(a.settingsOSKField.Buffer)
	a.closeSettingsOSKLocked()
	if path == "" {
		a.firmwarePickerStatus = "Coleco BIOS path is empty"
		a.status = a.firmwarePickerStatus
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		a.firmwarePickerStatus = err.Error()
		a.status = a.firmwarePickerStatus
		return
	}
	if info.IsDir() {
		a.showFirmwarePickerDirLocked(path, "")
		return
	}
	a.startFirmwareImportLocked(path)
}

func (a *App) startFirmwareImportLocked(path string) {
	if a.firmwarePickerBusy {
		return
	}
	if a.client == nil {
		a.firmwarePickerStatus = "host API is unavailable"
		a.status = a.firmwarePickerStatus
		return
	}
	a.firmwarePickerBusy = true
	a.firmwarePickerGen++
	gen := a.firmwarePickerGen
	a.firmwarePickerStatus = "Importing Coleco BIOS…"
	a.status = a.firmwarePickerStatus
	gameID := strings.TrimSpace(a.firmwarePickerGame.ID)
	ids := a.firmwareRefreshIDsLocked(gameID)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doFirmwareImport(ctx, gen, path, ids)
}

func (a *App) firmwareRefreshIDsLocked(gameID string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	add(gameID)
	if a.room != nil {
		dest := a.roomDestinationLocked()
		add(dest.GameID)
		for _, g := range dest.Matches {
			add(g.ID)
		}
	}
	for _, g := range a.games {
		if g.FirmwareRequired {
			add(g.ID)
		}
	}
	return ids
}

func (a *App) doFirmwareImport(ctx context.Context, gen int, path string, refreshIDs []string) {
	err := importAndSelectColecoBIOS(ctx, a.client, path)
	var games []hostclient.Game
	if err == nil {
		games = a.fetchFirmwareGames(ctx, refreshIDs)
		if len(refreshIDs) > 0 && !firmwareGamesContain(games, refreshIDs[0]) {
			err = fmt.Errorf("library did not refresh after Coleco BIOS import")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.firmwarePickerOpen || a.firmwarePickerGen != gen {
		return
	}
	a.firmwarePickerBusy = false
	if err != nil {
		a.firmwarePickerStatus = err.Error()
		a.status = a.firmwarePickerStatus
		return
	}
	a.applyFirmwareGamesLocked(games)
	a.closeFirmwarePickerLocked()
	a.status = "Coleco BIOS imported. Ready titles can Play."
}

func importAndSelectColecoBIOS(ctx context.Context, client *Client, path string) error {
	if client == nil {
		return fmt.Errorf("host API is unavailable")
	}
	file, size, err := openColecoBIOSFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	media, err := client.ImportCoreMedia(ctx, size, io.LimitReader(file, size))
	if err != nil {
		return err
	}
	if media.Size != protocol.FirmwareBytes {
		return fmt.Errorf("Coleco BIOS must be exactly %d bytes", protocol.FirmwareBytes)
	}
	slot, err := client.SelectHouseholdFirmware(ctx, media.MediaID)
	if err != nil {
		return err
	}
	if slot.MediaID != media.MediaID || slot.Size != protocol.FirmwareBytes {
		return fmt.Errorf("household firmware selection did not bind the imported BIOS")
	}
	return nil
}

func (a *App) fetchFirmwareGames(ctx context.Context, ids []string) []hostclient.Game {
	if a.client == nil {
		return nil
	}
	out := make([]hostclient.Game, 0, len(ids))
	for _, id := range ids {
		game, err := a.client.Game(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, game)
	}
	return out
}

func (a *App) applyFirmwareGamesLocked(games []hostclient.Game) {
	if len(games) == 0 {
		return
	}
	byID := map[string]hostclient.Game{}
	for _, g := range games {
		if strings.TrimSpace(g.ID) == "" {
			continue
		}
		byID[g.ID] = g
	}
	if len(a.games) > 0 {
		next := append([]hostclient.Game(nil), a.games...)
		for i, g := range next {
			if updated, ok := byID[g.ID]; ok {
				next[i] = updated
			}
		}
		a.games = next
	}
	if a.room != nil {
		a.room.RefreshCachedGames(games)
	}
	if game, ok := byID[strings.TrimSpace(a.firmwarePickerGame.ID)]; ok {
		a.firmwarePickerGame = game
	}
}

func missingFirmwareGame(game hostclient.Game) bool {
	return game.LaunchBlock() == hostclient.LaunchMissingFirmware
}

func firmwareGamesContain(games []hostclient.Game, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return true
	}
	for _, g := range games {
		if g.ID == id {
			return true
		}
	}
	return false
}
