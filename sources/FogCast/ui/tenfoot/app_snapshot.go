package tenfoot

import (
	"image"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
)

const detailMetaGap = 12

// layoutDetailMeta gives attribution its own reserved width so fitLabel cannot
// clip provenance off a shared facts line.
func layoutDetailMeta(d shared.FocusDetail, x, maxWidth, sizePx int) (facts string, factsX, factsW int, attr string, attrX, attrW int) {
	facts = d.MetaFacts()
	attr = strings.TrimSpace(d.Attribution)
	if maxWidth < 1 {
		return facts, x, 0, attr, x, 0
	}
	if attr == "" {
		return facts, x, maxWidth, "", x, 0
	}
	attrW = measureLabel(attr, sizePx)
	if attrW < 1 || attrW > maxWidth {
		attrW = maxWidth
	}
	if facts == "" || attrW+detailMetaGap >= maxWidth {
		return "", x, 0, attr, x, maxWidth
	}
	factsW = maxWidth - attrW - detailMetaGap
	return facts, x, factsW, attr, x + factsW + detailMetaGap, attrW
}

// Snapshot copies renderer-facing state. The games slice is shared: loadLibrary
// replaces it rather than mutating elements. Visible labels project prepared
// names while drawing. Cover images are shared; the cover map is copied.
func (a *App) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	games := a.games
	covers := make(map[string]*image.RGBA, len(a.covers))
	hits := 0
	for id, slot := range a.covers {
		if slot != nil && slot.image != nil {
			covers[id] = slot.image
			hits++
		}
	}
	status := a.status
	if a.setupRoomOpenLocked() && status == setupBlockedStatus && !a.setupSessionBusyLocked() {
		// The block is over: a notice it deferred, else the room's own line.
		status = a.setup.deferredNotice
		if status == "" {
			status = a.room.Title()
		}
		a.status = status
		a.setup.deferredNotice = ""
	}
	if a.setupRoomOpenLocked() && status == setupBlockedStatus && a.setupSessionBusyLocked() {
		// Preserve the setup action's reason while a launch status is present.
		status = setupBlockedStatus
	} else if a.tapePickerOpen && strings.TrimSpace(a.tapePickerStatus) != "" {
		status = a.tapePickerStatus
	} else if line := a.stopStatusLocked(); line != "" {
		status = line
	} else if a.retryStopLock && strings.TrimSpace(a.retryStopHint) != "" {
		status = a.retryStopHint
	} else if a.inputBusy && strings.TrimSpace(a.inputMessage) != "" {
		status = a.inputMessage
	} else if a.developmentLoadingLocked() && strings.TrimSpace(a.devLoadMessage) != "" {
		status = a.devLoadMessage
	} else if (a.devLoadPhase == "error" || a.devLoadPhase == "host") && a.session.State != "active" && strings.TrimSpace(a.devLoadMessage) != "" {
		status = a.devLoadMessage
	} else if a.launch.Phase != "idle" && a.launch.Phase != "ok" && a.launch.Message != "" {
		status = a.launch.Message
	} else if line := a.nowPlayingStatusLocked(); line != "" {
		status = line
	} else if a.session.State == "active" && strings.HasPrefix(strings.TrimSpace(a.status), "Tape ") {
		status = strings.TrimSpace(a.status)
	} else if a.launch.Phase == "ok" && a.launch.Message != "" {
		status = a.launch.Message
	} else if line := strings.TrimSpace(a.inputMessage); line != "" {
		status = line
	}
	if a.kitLocalKeyboardUnavailableLocked() {
		if status != "" {
			status = kitLocalKeyboardUnavailableStatus + " · " + status
		} else {
			status = kitLocalKeyboardUnavailableStatus
		}
	}
	if a.platformErr != "" {
		if status == "" {
			status = "platform list failed"
		} else {
			status = status + " · platform list failed"
		}
	}
	if a.collectionsErr != "" {
		if status == "" {
			status = "collection list failed"
		} else {
			status = status + " · collection list failed"
		}
	}
	health := a.healthSnapshotLocked()
	if health.Line != "" && (status == "" || status == "connecting to host API") {
		status = health.Line
	}
	snap := Snapshot{
		Games:               games,
		Grid:                a.grid,
		Status:              status,
		LoadErr:             a.loadErr,
		Loading:             a.loading,
		Covers:              covers,
		Launch:              a.launch,
		Gamepads:            a.gamepads,
		Keyboards:           a.keyboards,
		Mice:                a.mice,
		Affinity:            a.affinity.current.Kind,
		AffinityID:          a.affinity.current.ID,
		Prompts:             a.affinityPromptsLocked(),
		CoverHits:           hits,
		Platforms:           a.platforms,
		PlatformID:          a.platformID,
		Sort:                a.sort,
		Query:               a.searchField.Buffer,
		SearchOpen:          a.searchOpen,
		FocusDetail:         a.focusDetailLocked(),
		DetailPlay:          a.detailPlayLocked(),
		KitPlay:             a.kitPlayStateLocked(),
		LibraryBackTo:       a.libraryBackToLocked(),
		Collection:          a.collectionID,
		ViewLabel:           a.viewLabelLocked(),
		ViewPicker:          a.viewPickerOpen,
		ViewPickerIndex:     a.viewPickerIndex,
		Views:               a.viewChoicesLocked(),
		PickerRows:          a.pickerRowsLocked(),
		CollectionMenu:      a.collectionMenuSnapshotLocked(),
		Controllers:         a.controllersSnapshotLocked(),
		Session:             a.sessionSnapshotLocked(),
		GPUParked:           a.gpuParked,
		Surround:            SurroundSnapshot{Active: a.surroundShownLocked()},
		Attract:             a.attractSnapshotLocked(),
		SafeAreaPct:         a.safeAreaPct,
		Settings:            a.settingsSnapshotLocked(),
		Filters:             a.filtersSnapshotLocked(),
		Genre:               a.filterGenre,
		Year:                a.filterYear,
		Region:              a.filterRegion,
		HidePrerelease:      a.hidePrerelease,
		HideHacks:           a.hideHacks,
		OSK:                 a.oskSnapshotLocked(),
		Health:              health,
		KitLease:            a.kitLeaseSnapshotLocked(),
		Detail:              a.detailSnapshotLocked(),
		Screenshots:         a.screenshotImagesLocked(),
		Preview:             a.previewSnapshotLocked(),
		Theme:               a.theme.Complete(),
		DebugHUD:            a.debugHUDSnapshotLocked(),
		Room:                a.roomSnapshotLocked(true),
		RoomPicker:          a.roomPickerSnapshotLocked(),
		Storage:             a.driveSettingsSnapshotLocked(),
		CoreLibrary:         a.coreLibrarySnapshotLocked(),
		FirmwarePicker:      a.firmwarePickerSnapshotLocked(),
		FirmwareCard:        a.firmwareCardSnapshotLocked(),
		TapePicker:          a.tapePickerSnapshotLocked(),
		ReducedMotion:       a.reducedMotion,
		sessionObservation:  a.sessionObservation,
		displayFrameRequest: a.pendingDisplayFrameLocked(),
		HostPresentsPaused:  a.hostPresentsPaused || (a.kitHost && (a.session.State == "launching" || a.session.State == "stopping" || (a.session.State == "active" && !a.sessionDisplayLocked().Visible))),
		HostLateAdopt:       (a.kitHost && (a.hostAwaitStatus || a.session.State == "active" || a.session.State == "launching" || a.session.State == "stopping")),
	}
	destination := snap.DetailPlay
	if snap.Room.Open {
		destination = snap.Room.Destination
	}
	snap.BrowseActions = a.browseActionsSnapshotLocked(destination)
	return snap

}

func (a *App) oskSnapshotLocked() shared.OSKSnapshot {
	if a.settingsOSKOpenLocked() {
		snap := a.settingsOSKField.Snapshot()
		snap.Open = true
		switch a.settingsOSKKind {
		case settingsOSKDriveField:
			snap.Prompt = "SMB " + a.driveSettings.Status
			if a.driveSettings.Status == "password" {
				snap.Masked = true
				snap.Buffer = shared.MaskSecret(a.settingsOSKField.Buffer)
			}
		case settingsOSKLibraryPath:
			snap.Prompt = "Library path"
		case settingsOSKTargetName:
			snap.Prompt = "Target name"
		case settingsOSKTargetAddress:
			snap.Prompt = "Target address"
		case settingsOSKTargetAgent:
			snap.Prompt = "Agent password"
			snap.Masked = true
			snap.Buffer = shared.MaskSecret(a.settingsOSKField.Buffer)
		case settingsOSKDevelopmentPath:
			snap.Prompt = "DIAGNOSTIC RBF path"
		case settingsOSKCoreTitle:
			snap.Prompt = "Game title"
		case settingsOSKShelfName:
			snap.Prompt = "Disk name"
		case settingsOSKCorePath:
			snap.Prompt = "ROM or media path"
		case settingsOSKFirmwarePath:
			snap.Prompt = "Firmware ROM path"
		case settingsOSKTapePath:
			snap.Prompt = "Media path"
		}
		return a.withOSKHintLocked(snap)
	}
	if a.nameEntryOpenLocked() {
		snap := a.nameField.Snapshot()
		snap.Open = true
		snap.Prompt = "Collection name"
		return a.withOSKHintLocked(snap)
	}
	if !a.searchOpen {
		return shared.OSKSnapshot{}
	}
	snap := a.searchField.Snapshot()
	snap.Open = true
	snap.Prompt = "Search"
	return a.withOSKHintLocked(snap)
}

func (a *App) withOSKHintLocked(snap shared.OSKSnapshot) shared.OSKSnapshot {
	snap.Hint = oskHintFor(a.affinity.current.Kind, snap.Page)
	return snap
}

// SearchOpen reports whether the on-screen search field is active.
func (a *App) SearchOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.searchOpen
}

// ViewPickerOpen reports whether the library view list is on screen.
func (a *App) ViewPickerOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.viewPickerOpen
}

func (a *App) browseHoldEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.browseHoldEnabledLocked()
}

func (a *App) browseHoldEnabledLocked() bool {
	if a.coreLibrary.Open || a.gpuParked || a.stopPhase == "stopping" || a.launch.Phase == "launching" || a.retryStopLock || a.developmentLoadingLocked() {
		return false
	}
	switch a.session.State {
	case "active", "launching":
		return false
	default:
		return true
	}
}

// ChromeLine is the header label: now-playing while a session is active, otherwise browse chrome.
func (s Snapshot) ChromeLine() string {
	line := s.chromeBody()
	if health := strings.TrimSpace(s.Health.Line); health != "" && !strings.Contains(line, health) {
		line = health + "  ·  " + line
	}
	if lease := strings.TrimSpace(s.KitLease.Line); lease != "" && !strings.Contains(line, lease) {
		line = line + "  ·  " + lease
	}
	return line
}

func (s Snapshot) chromeBody() string {
	if s.GPUParked || s.Session.State == "active" {
		line := s.NowPlayingLine()
		if strings.TrimSpace(s.Status) != "" && s.Status != line && s.Status != s.Health.Line {
			return line + "  ·  " + s.Status
		}
		return line
	}
	view := strings.TrimSpace(s.ViewLabel)
	if view == "" {
		view = "All"
	}
	platform := "All"
	if s.PlatformID != "" {
		platform = s.PlatformID
		for _, row := range s.Platforms {
			if row.ID == s.PlatformID {
				if label := strings.TrimSpace(row.Label); label != "" {
					platform = label
				}
				break
			}
		}
	}
	search := "Search"
	if s.OSK.Open && s.OSK.Prompt != "" && s.OSK.Prompt != "Search" {
		search = s.OSK.Prompt + ": " + s.OSK.Buffer + "_"
	} else if s.SearchOpen {
		search = "Search: " + s.Query + "_"
	} else if strings.TrimSpace(s.Query) != "" {
		search = "Search: " + s.Query
	}
	parts := []string{view, platform, sortLabel(s.Collection, s.Sort)}
	parts = append(parts, filterSummaryParts(s.Genre, s.Year, s.Region, s.HidePrerelease, s.HideHacks)...)
	parts = append(parts, search)
	if s.Grid.Focus >= 0 && s.Grid.Focus < len(s.Games) {
		play := classifyLibraryGame(s.Games[s.Grid.Focus], s.KitPlay)
		if label := play.Availability.Label(); label != "" {
			parts = append(parts, label)
		}
	}
	status := s.Status
	if health := strings.TrimSpace(s.Health.Line); health != "" && status == health {
		return strings.Join(parts, "  ·  ")
	}
	return strings.Join(append(parts, status), "  ·  ")
}

// NowPlayingLine is the compact active-session chrome.
func (s Snapshot) NowPlayingLine() string {
	if s.Session.Diagnostic {
		return diagnosticNowPlayingLine(s.Session)
	}
	heading := nowPlayingHeading(s)
	parts := make([]string, 0, 6)
	parts = append(parts, heading)
	title := strings.TrimSpace(s.Session.Title)
	if title == "" {
		title = strings.TrimSpace(s.Session.GameID)
	}
	if title != "" {
		parts = append(parts, title)
	}
	if chrome := strings.TrimSpace(s.Session.Chrome); chrome != "" {
		parts = append(parts, chrome)
	} else if state := strings.TrimSpace(s.Session.State); state != "" {
		parts = append(parts, state)
	}
	if exec := strings.TrimSpace(s.Session.Execution); exec != "" {
		parts = append(parts, exec)
	}
	if media := strings.TrimSpace(s.Session.Media); media != "" {
		parts = append(parts, "media "+media)
	}
	if input := strings.TrimSpace(s.Session.InputState); input != "" {
		parts = append(parts, "input "+input)
	}
	if hint := strings.TrimSpace(s.Session.InputHint); hint != "" {
		parts = append(parts, hint)
	}
	if s.Session.Stopping && strings.TrimSpace(s.Session.Chrome) != sessionChromeStopping {
		parts = append(parts, "stopping")
	}
	if s.Session.RetryStop {
		hint := strings.TrimSpace(s.Session.RetryHint)
		if hint == "" {
			hint = "retry Stop"
		}
		parts = append(parts, hint)
	}
	return strings.Join(parts, "  ·  ")
}

const (
	nowPlayingHeadingPlay = "Now playing"
	nowPlayingHeadingSave = "Save failed"
	nowPlayingHeadingStop = "Stop failed"
)

// nowPlayingHeading is the living-room label for the parked session surface.
// A failed save must not read as Now playing or Completed.
func nowPlayingHeading(s Snapshot) string {
	if s.Session.Diagnostic {
		return diagnosticLabel
	}
	if s.Session.RetryStop || strings.TrimSpace(s.Session.Chrome) == sessionChromeFailed {
		if sessionSaveFailedVisible(s) {
			return nowPlayingHeadingSave
		}
		return nowPlayingHeadingStop
	}
	return nowPlayingHeadingPlay
}

func sessionSaveFailedVisible(s Snapshot) bool {
	blob := strings.ToLower(strings.Join([]string{
		s.Session.RetryCode,
		s.Session.RetryHint,
		s.Status,
		s.Session.Progress,
	}, " "))
	return strings.Contains(blob, "save_failed") ||
		strings.Contains(blob, "save could not") ||
		strings.Contains(blob, "save fail")
}

// Selected returns the focused game, if any.
func (a *App) Selected() (hostclient.Game, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return hostclient.Game{}, false
	}
	return a.games[a.grid.Focus], true
}

// detailPlayLocked classifies the title Details describes. In a room it is
// the room's own destination. In the Library it is the refused launch when
// that title is not the focused row, and otherwise the focused row, with the
// kit lease applied as Confirm would see it.
func (a *App) detailPlayLocked() rooms.Destination {
	var game hostclient.Game
	switch {
	case a.room != nil && strings.TrimSpace(a.roomDetail.ID) != "":
		// Details for one chosen edition describes that edition.
		game = a.roomDetail
		d := a.gamePlayDestinationLocked(game)
		if a.session.State == "active" && a.roomDuringPlay && a.room.ID() == colecoArcadeRoomID && a.colecoSessionLocked() && d.Confirm() == rooms.ConfirmLaunch {
			d.Status = colecoRunningDestinationStatus
			d.Action = colecoRunningDestinationAction
		}
		return d
	case a.room != nil:
		return a.roomDestinationLocked()
	case strings.TrimSpace(a.libraryDetail.ID) != "":
		game = a.libraryDetail
	case a.grid.Focus < 0 || a.grid.Focus >= len(a.games):
		return rooms.Destination{}
	default:
		game = a.games[a.grid.Focus]
	}
	return a.gamePlayDestinationLocked(game)
}

// gamePlayDestinationLocked classifies one title as Confirm will treat it.
func (a *App) gamePlayDestinationLocked(game hostclient.Game) rooms.Destination {
	return classifyLibraryGame(game, a.kitPlayStateLocked())
}

// kitPlayStateLocked is the kit state every Library classification shares.
func (a *App) kitPlayStateLocked() kitPlayState {
	paired := a.client != nil && a.client.paired
	return kitPlayState{
		Paired:  paired,
		Unknown: paired && a.pairedKitStatusUnavailableLocked(),
		Foreign: a.foreignKitLeaseLocked(),
	}
}

func (a *App) focusDetailLocked() shared.FocusDetail {
	if a.room != nil && strings.TrimSpace(a.roomDetail.ID) != "" {
		return a.detailForGameLocked(a.roomDetail)
	}
	if a.room == nil && strings.TrimSpace(a.libraryDetail.ID) != "" {
		return a.detailForGameLocked(a.libraryDetail)
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return shared.FocusDetail{}
	}
	return a.detailForGameLocked(a.games[a.grid.Focus])
}

func (a *App) detailForGameLocked(game hostclient.Game) shared.FocusDetail {
	detail := shared.FocusDetail{
		Title:    displayGameTitle(game),
		Platform: a.platformLabelLocked(game.System),
		Year:     strings.TrimSpace(game.Year),
		Genre:    strings.TrimSpace(game.Genre),
		Favorite: game.Favorite,
	}
	if cached, ok := a.details[game.ID]; ok {
		if strings.TrimSpace(cached.Year) != "" {
			detail.Year = cached.Year
		}
		if strings.TrimSpace(cached.Genre) != "" {
			detail.Genre = cached.Genre
		}
		detail.Summary = strings.TrimSpace(cached.Summary)
		detail.Studio = strings.TrimSpace(cached.Studio)
		detail.Players = strings.TrimSpace(cached.Players)
		detail.Attribution = strings.TrimSpace(cached.Attribution)
		if len(cached.ScreenshotIDs) > 0 {
			detail.ScreenshotIDs = append([]string(nil), cached.ScreenshotIDs...)
		}
	}
	if ids := a.shotIDs[game.ID]; len(ids) > 0 {
		detail.ScreenshotIDs = append([]string(nil), ids...)
	}
	return detail
}

func (a *App) platformLabelLocked(system string) string {
	system = strings.TrimSpace(system)
	for _, platform := range a.platforms {
		if platform.ID == system {
			if label := strings.TrimSpace(platform.Label); label != "" {
				return label
			}
			return platform.ID
		}
	}
	if a.platformID != "" && a.platformID == system {
		return a.platformID
	}
	return system
}

func (a *App) moveFocusLocked(dx, dy int) {
	if dy > 0 && a.detailEnterFromBrowseLocked() && a.enterBrowseActionsLocked(0) {
		return
	}
	before := a.grid.Focus
	a.grid.Move(dx, dy)
	if a.grid.Focus != before {
		a.navDirty = true
		a.carouselIndex = 0
		a.stampFocusLocked()
		return
	}
	if dy > 0 {
		a.enterBrowseActionsLocked(0)
	}
}

func (a *App) focusIndex(i int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.focusIndexLocked(i)
}

func (a *App) focusIndexLocked(i int) bool {
	if i < 0 || i >= len(a.games) {
		return false
	}
	if a.grid.Focus != i {
		a.navDirty = true
		a.carouselIndex = 0
		a.grid.Focus = i
		a.grid.ensureVisible()
		a.stampFocusLocked()
		return true
	}
	a.grid.Focus = i
	a.grid.ensureVisible()
	return true
}

func wrapWords(text string, maxChars, maxLines int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxChars < 1 || maxLines < 1 {
		return nil
	}
	words := strings.Fields(text)
	lines := make([]string, 0, maxLines)
	var cur string
	flush := func() bool {
		if cur == "" {
			return len(lines) >= maxLines
		}
		lines = append(lines, cur)
		cur = ""
		return len(lines) >= maxLines
	}
	for _, word := range words {
		if len([]rune(word)) > maxChars {
			word = string([]rune(word)[:maxChars])
		}
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if len([]rune(next)) <= maxChars {
			cur = next
			continue
		}
		if flush() {
			return lines
		}
		cur = word
	}
	flush()
	return lines
}
