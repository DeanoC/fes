package tenfoot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/rooms"
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
	// Core binds a Systems row to its catalog entry independently of focus position.
	Core *hostclient.AvailableCore
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

func firmwarePickerRoots(home string, libraries []hostclient.LibraryRoot) []FirmwarePickerRow {
	rows := []FirmwarePickerRow{{
		Name: firmwarePickerOSKLabel,
		Kind: firmwarePickerKindOSK,
	}}
	seen := map[string]bool{}
	addRoot := func(name, path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || seen[path] || blockprobe.Skips(path) {
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
		if blockprobe.Skips(full) {
			continue
		}
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
			return label + "  ·  Firmware"
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
		Title:  a.firmwarePickerTitleLocked(),
		Path:   a.pickerDisplayPathLocked(a.firmwarePickerPath),
		Index:  a.firmwarePickerIndex,
		Rows:   append([]FirmwarePickerRow(nil), a.firmwarePickerRows...),
		Status: a.firmwarePickerStatus,
		Busy:   a.firmwarePickerBusy,
		Hint:   firmwarePickerHint(a.affinity.current.Kind),
	}
}

func (a *App) closeFirmwarePickerLocked() {
	a.firmwarePickerFromDetail = false
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
	a.firmwarePickerFromDetail = a.room == nil && a.detailOpen
	a.closeDetailLocked()
	a.roomChoiceOpen = false
	a.roomChoice = nil
	a.roomDetail = hostclient.Game{}
	a.firmwarePickerGame = game
	a.firmwarePickerOpen = true
	a.firmwarePickerBusy = false
	a.firmwarePickerGen++
	a.showFirmwarePickerRootsLocked(a.firmwarePickerPromptLocked())
	a.refreshStorageRootsLocked()
}

func (a *App) entryFirmwarePickerLocked() bool {
	return a.firmwarePickerGame.ROMRole == protocol.FirmwareRole && a.firmwarePickerGame.ROMID != "" && a.firmwarePickerGame.ROMSourceSize > 0
}

func (a *App) firmwarePickerRequiredSizeLocked() int64 {
	if a.entryFirmwarePickerLocked() {
		return a.firmwarePickerGame.ROMSourceSize
	}
	return protocol.FirmwareBytes
}

func (a *App) firmwarePickerTitleLocked() string {
	if a.entryFirmwarePickerLocked() {
		return "Import package firmware"
	}
	return firmwarePickerTitle
}

func (a *App) firmwarePickerPromptLocked() string {
	if a.entryFirmwarePickerLocked() {
		return fmt.Sprintf("Choose a %d-byte firmware ROM for this title.", a.firmwarePickerRequiredSizeLocked())
	}
	return "Choose an 8192-byte Coleco BIOS."
}

func (a *App) markFirmwarePickerRowsLocked(rows []FirmwarePickerRow) {
	want := a.firmwarePickerRequiredSizeLocked()
	for i := range rows {
		rows[i].Selectable = rows[i].Kind == firmwarePickerKindFile && rows[i].Size == want
	}
}

func (a *App) showFirmwarePickerRootsLocked(status string) {
	a.firmwarePickerAtRoots = true
	a.firmwarePickerPath = ""
	a.firmwarePickerRows = a.storagePickerRootsLocked("bios")
	a.firmwarePickerIndex = 0
	a.firmwarePickerStatus = status
	a.status = status
}

func (a *App) showFirmwarePickerDirLocked(path, status string) {
	if path == "" {
		a.showFirmwarePickerRootsLocked("")
		return
	}
	if _, _, _, ok := splitDrivePath(path); ok {
		a.loadDrivePickerDirLocked(path, "firmware")
		return
	}
	rows, err := listFirmwarePickerDir(path)
	if err != nil {
		a.firmwarePickerStatus = err.Error()
		a.status = a.firmwarePickerStatus
		return
	}
	a.markFirmwarePickerRowsLocked(rows)
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
			a.firmwarePickerStatus = "Firmware import in progress"
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
			a.showFirmwarePickerRootsLocked(a.firmwarePickerPromptLocked())
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
		reopen := a.firmwarePickerFromDetail
		game := a.firmwarePickerGame
		a.closeFirmwarePickerLocked()
		// Cancel returns to the Library Details that asked for the BIOS,
		// including a title that is not the focused row.
		if reopen && a.room == nil {
			a.openLibraryDetailLocked(game)
		}
		return
	}
	path := a.firmwarePickerPath
	parent := filepath.Dir(path)
	if parent == path || parent == "." {
		a.showFirmwarePickerRootsLocked(a.firmwarePickerPromptLocked())
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
			a.firmwarePickerStatus = fmt.Sprintf("%s is a %d-byte firmware ROM. Confirm to import.", row.Name, row.Size)
		} else {
			a.firmwarePickerStatus = fmt.Sprintf("%s is %d bytes. Firmware must be exactly %d bytes.", row.Name, row.Size, a.firmwarePickerRequiredSizeLocked())
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
			a.firmwarePickerStatus = fmt.Sprintf("Firmware must be exactly %d bytes (this file is %d).", a.firmwarePickerRequiredSizeLocked(), row.Size)
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
		a.firmwarePickerStatus = "Firmware path is empty"
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
	if a.kitMutationBlockedLocked() {
		a.firmwarePickerStatus = a.status
		return
	}
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
	game := a.firmwarePickerGame
	if a.entryFirmwarePickerLocked() {
		a.firmwarePickerStatus = "Importing package firmware…"
	} else {
		a.firmwarePickerStatus = "Importing Coleco BIOS…"
	}
	a.status = a.firmwarePickerStatus
	gameID := strings.TrimSpace(a.firmwarePickerGame.ID)
	ids := a.firmwareRefreshIDsLocked(gameID)
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doFirmwareImport(ctx, gen, path, ids, a.driveManagerLocked(), game)
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
		addDestinationGameIDs(add, a.roomDestinationLocked())
	}
	for _, room := range a.suspendedRoomsLocked() {
		addDestinationGameIDs(add, room.Destination())
	}
	for _, g := range a.games {
		if g.FirmwareRequired {
			add(g.ID)
		}
	}
	return ids
}

func (a *App) doFirmwareImport(ctx context.Context, gen int, path string, refreshIDs []string, manager *storage.Manager, game hostclient.Game) {
	var err error
	if game.ROMRole == protocol.FirmwareRole && game.ROMID != "" {
		err = importAndSelectCoreFirmwareROM(ctx, a.client, game, path, manager)
	} else {
		err = importAndSelectColecoBIOS(ctx, a.client, path, manager)
	}
	var games []hostclient.Game
	if err == nil {
		games = a.fetchFirmwareGames(ctx, refreshIDs)
		if len(refreshIDs) > 0 && !firmwareGamesContain(games, refreshIDs[0]) {
			err = fmt.Errorf("library did not refresh after firmware import")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.firmwarePickerGen == gen
	if err == nil {
		// The picker may have been dismissed while the host operation was in
		// flight. The refreshed library rows are still valid for the app and
		// room cache, but an older operation must not change a newer picker's
		// busy/error state.
		a.applyFirmwareGamesLocked(games)
	}
	if !current {
		return
	}
	a.firmwarePickerBusy = false
	if err != nil {
		a.firmwarePickerStatus = err.Error()
		a.status = a.firmwarePickerStatus
		return
	}
	a.closeFirmwarePickerLocked()
	if game.ROMRole == protocol.FirmwareRole && game.ROMID != "" {
		a.status = "Package firmware imported. Ready titles can Play."
	} else {
		a.status = "Coleco BIOS imported. Ready titles can Play."
	}
}

func importAndSelectCoreFirmwareROM(ctx context.Context, client *Client, game hostclient.Game, path string, manager *storage.Manager) error {
	if client == nil || game.ID == "" || game.ROMRole != protocol.FirmwareRole || game.ROMID == "" || game.ROMSourceSize < 1 {
		return fmt.Errorf("firmware ROM requirement is unavailable")
	}
	file, size, _, err := openPickerFile(ctx, manager, client, path, storage.MaxFileBytes)
	if err != nil {
		return err
	}
	defer file.Close()
	if size != game.ROMSourceSize {
		return fmt.Errorf("firmware ROM must be exactly %d bytes", game.ROMSourceSize)
	}
	media, err := client.ImportCoreMedia(ctx, size, io.LimitReader(file, size))
	if err != nil {
		return err
	}
	if media.Size != game.ROMSourceSize {
		return fmt.Errorf("imported firmware ROM size differs")
	}
	entry, err := client.CoreEntry(ctx, game.ID)
	if err != nil {
		return err
	}
	selection, err := client.SelectCoreEntryROM(ctx, game.ID, entry.PackageID, game.ROMID, game.ROMMediaID, media.MediaID)
	if err != nil {
		return err
	}
	if selection.GameID != game.ID || selection.PackageID != entry.PackageID || selection.ROMID != game.ROMID || selection.MediaID != media.MediaID {
		return fmt.Errorf("selected firmware ROM identity mismatch")
	}
	return nil
}

func importAndSelectColecoBIOS(ctx context.Context, client *Client, path string, manager *storage.Manager) error {
	if client == nil {
		return fmt.Errorf("host API is unavailable")
	}
	file, size, _, err := openPickerFile(ctx, manager, client, path, storage.MaxFileBytes)
	if err != nil {
		return err
	}
	defer file.Close()
	if size != protocol.FirmwareBytes {
		return fmt.Errorf("Coleco BIOS must be exactly %d bytes", protocol.FirmwareBytes)
	}
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
	for _, room := range a.suspendedRoomsLocked() {
		room.RefreshCachedGames(games)
	}
	if game, ok := byID[strings.TrimSpace(a.firmwarePickerGame.ID)]; ok {
		a.firmwarePickerGame = game
	}
}

// suspendedRoomsLocked is the room the Library was opened from, plus the
// parents suspended with it. Firmware refresh has to reach them: resume
// republishes from this cache.
func (a *App) suspendedRoomsLocked() []*rooms.Instance {
	if a.libraryReturn == nil {
		return nil
	}
	out := make([]*rooms.Instance, 0, 1+len(a.libraryReturn.stack))
	if a.libraryReturn.room != nil {
		out = append(out, a.libraryReturn.room)
	}
	for _, parent := range a.libraryReturn.stack {
		if parent != nil {
			out = append(out, parent)
		}
	}
	return out
}

func addDestinationGameIDs(add func(string), dest rooms.Destination) {
	add(dest.GameID)
	for _, g := range dest.Matches {
		add(g.ID)
	}
	for _, g := range dest.Candidates {
		add(g.ID)
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
