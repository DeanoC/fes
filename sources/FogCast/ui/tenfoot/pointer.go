package tenfoot

import "time"

// PointerKind is the sofa widget under a pointer coordinate.
type PointerKind int

const (
	PointerNone PointerKind = iota
	PointerCatalog
	PointerDetail
	PointerCarousel
	PointerOSK
	PointerSettings
	PointerFilters
	PointerViewPicker
	PointerCollectionMenu
	PointerAttract
	PointerBackdrop
	PointerRoom
	PointerRoomPicker
	PointerRoomChoice
	PointerRoomDestination
	PointerLaunchOverlay
	PointerFirmwarePicker
	PointerTapePicker
)

// PointerHit is one hit-test result in logical sofa pixels.
type PointerHit struct {
	Kind  PointerKind
	Index int
	KeyID string
}

func (k PointerKind) String() string {
	switch k {
	case PointerCatalog:
		return "catalog"
	case PointerDetail:
		return "detail"
	case PointerCarousel:
		return "carousel"
	case PointerOSK:
		return "osk"
	case PointerSettings:
		return "settings"
	case PointerFilters:
		return "filters"
	case PointerViewPicker:
		return "view-picker"
	case PointerCollectionMenu:
		return "collection-menu"
	case PointerAttract:
		return "attract"
	case PointerBackdrop:
		return "backdrop"
	case PointerRoom:
		return "room"
	case PointerRoomPicker:
		return "room-picker"
	case PointerRoomChoice:
		return "room-choice"
	case PointerRoomDestination:
		return "room-destination"
	case PointerLaunchOverlay:
		return "launch-overlay"
	case PointerFirmwarePicker:
		return "firmware-picker"
	case PointerTapePicker:
		return "tape-picker"
	default:
		return "none"
	}
}

// HitTest reports the topmost sofa widget at logical pixel (x, y).
func HitTest(snap Snapshot, x, y int) PointerHit {
	if snap.Attract.Active {
		return PointerHit{Kind: PointerAttract}
	}
	if snap.GPUParked {
		return PointerHit{}
	}
	if snap.OSK.Open {
		geom, keys, ok := oskLayout(snap)
		if ok {
			for _, key := range keys {
				if (rectI{X: key.X, Y: key.Y, W: key.W, H: key.H}).contains(x, y) {
					return PointerHit{Kind: PointerOSK, Index: key.Row, KeyID: key.ID}
				}
			}
			if geom.contains(x, y) {
				return PointerHit{}
			}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := collectionMenuPanel(snap); ok {
		if snap.CollectionMenu.Confirm {
			if panel.contains(x, y) {
				return PointerHit{Kind: PointerCollectionMenu, Index: 0}
			}
			return PointerHit{Kind: PointerBackdrop}
		}
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.CollectionMenu.Rows) {
			return PointerHit{Kind: PointerCollectionMenu, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := viewPickerPanel(snap); ok {
		rows := viewPickerRows(snap)
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(rows) {
			return PointerHit{Kind: PointerViewPicker, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := settingsPanel(snap); ok {
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.Settings.Rows) {
			return PointerHit{Kind: PointerSettings, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := filtersPanel(snap); ok {
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.Filters.Rows) {
			return PointerHit{Kind: PointerFilters, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := roomPickerPanel(snap); ok {
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.RoomPicker.Rows) {
			return PointerHit{Kind: PointerRoomPicker, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := firmwarePickerPanel(snap); ok {
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.FirmwarePicker.Rows) {
			return PointerHit{Kind: PointerFirmwarePicker, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if panel, ok := tapePickerPanel(snap); ok {
		if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.TapePicker.Rows) {
			return PointerHit{Kind: PointerTapePicker, Index: idx}
		}
		if panel.contains(x, y) {
			return PointerHit{}
		}
		return PointerHit{Kind: PointerBackdrop}
	}
	if snap.Room.Open {
		if panel, ok := launchOverlayPanel(snap); ok {
			if panel.contains(x, y) {
				return PointerHit{Kind: PointerLaunchOverlay}
			}
			return PointerHit{Kind: PointerBackdrop}
		}
		if panel, ok := roomChoicePanel(snap); ok {
			if idx, hit := panel.rowAt(x, y); hit && idx >= 0 && idx < len(snap.Room.Choice.Rows) {
				return PointerHit{Kind: PointerRoomChoice, Index: idx}
			}
			if panel.contains(x, y) {
				return PointerHit{}
			}
			return PointerHit{Kind: PointerBackdrop}
		}
		if snap.Detail.Open {
			if geom, ok := detailGeomOf(snap); ok {
				if geom.Carousel.contains(x, y) {
					return PointerHit{Kind: PointerCarousel}
				}
				if geom.Pane.contains(x, y) {
					return PointerHit{Kind: PointerDetail}
				}
			}
			return PointerHit{Kind: PointerBackdrop}
		}
		if pane, ok := roomDestGeom(snap); ok && pane.contains(x, y) {
			return PointerHit{Kind: PointerRoomDestination}
		}
		if hit, ok := snap.Room.Frame.HitAt(float32(x-snap.Room.OffsetX), float32(y-snap.Room.OffsetY)); ok {
			return PointerHit{Kind: PointerRoom, KeyID: hit.ID}
		}
		return PointerHit{}
	}
	if snap.Detail.Open {
		if geom, ok := detailGeomOf(snap); ok {
			if geom.Carousel.contains(x, y) {
				return PointerHit{Kind: PointerCarousel}
			}
			if geom.Pane.contains(x, y) {
				return PointerHit{Kind: PointerDetail}
			}
		}
	}
	grid := snap.Grid
	if i, ok := grid.HitIndex(x, y); ok && i < len(snap.Games) {
		return PointerHit{Kind: PointerCatalog, Index: i}
	}
	return PointerHit{}
}

// PointerMove updates sofa focus from a pointer coordinate. Hover does not
// activate; primary click does. Coordinates are logical sofa pixels.
func (a *App) PointerMove(x, y int, now time.Time) {
	a.PointerMoveFrom(0, x, y, now)
}

// PointerMoveFrom is PointerMove with an SDL mouse instance id for affinity.
func (a *App) PointerMoveFrom(id, x, y int, now time.Time) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.tapePickerOpen || a.firmwarePickerOpen {
		a.noteInputLocked(InputMouse, id)
		a.applyPointerFocusLocked(a.hitTestLocked(x, y))
		return
	}
	if a.gpuParked || a.forwardsCoreKeyboardLocked() || a.sessionStopOfferedLocked() {
		return
	}
	a.noteInputLocked(InputMouse, id)
	if a.attractActive {
		a.hideAttractLocked()
		return
	}
	a.applyPointerFocusLocked(a.hitTestLocked(x, y))
}

// PointerClick focuses the widget under the pointer and activates it
// (launch, OSK key, overlay confirm). Empty space does not activate the
// previously focused title.
func (a *App) PointerClick(x, y int, now time.Time) {
	a.PointerClickFrom(0, x, y, now)
}

// PointerClickFrom is PointerClick with an SDL mouse instance id for affinity.
func (a *App) PointerClickFrom(id, x, y int, now time.Time) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.tapePickerOpen || a.firmwarePickerOpen {
		a.noteInputLocked(InputMouse, id)
		hit := a.hitTestLocked(x, y)
		a.applyPointerFocusLocked(hit)
		a.activatePointerHitLocked(hit, now)
		return
	}
	if a.gpuParked || a.forwardsCoreKeyboardLocked() || a.sessionStopOfferedLocked() {
		return
	}
	a.noteInputLocked(InputMouse, id)
	if a.attractActive {
		a.consumeAttractLocked(CmdSelect, now)
		return
	}
	hit := a.hitTestLocked(x, y)
	a.applyPointerFocusLocked(hit)
	a.activatePointerHitLocked(hit, now)
}

func (a *App) hitTestLocked(x, y int) PointerHit {
	return HitTest(a.pointerSnapshotLocked(), x, y)
}

func (a *App) pointerSnapshotLocked() Snapshot {
	return Snapshot{
		Games:           a.games,
		Grid:            a.grid,
		ViewPicker:      a.viewPickerOpen,
		ViewPickerIndex: a.viewPickerIndex,
		Views:           a.viewChoicesLocked(),
		PickerRows:      a.pickerRowsLocked(),
		CollectionMenu:  a.collectionMenuSnapshotLocked(),
		GPUParked:       a.gpuParked,
		Launch:          a.launch,
		Attract:         AttractSnapshot{Active: a.attractActive},
		Settings:        a.settingsSnapshotLocked(),
		Filters:         a.filtersSnapshotLocked(),
		OSK:             a.oskSnapshotLocked(),
		Detail:          a.detailSnapshotLocked(),
		FocusDetail:     a.focusDetailLocked(),
		Room:            a.roomSnapshotLocked(false),
		RoomPicker:      a.roomPickerSnapshotLocked(),
		FirmwarePicker:  a.firmwarePickerSnapshotLocked(),
		TapePicker:      a.tapePickerSnapshotLocked(),
	}
}

func (a *App) applyPointerFocusLocked(hit PointerHit) {
	switch hit.Kind {
	case PointerCatalog:
		a.focusIndexLocked(hit.Index)
	case PointerOSK:
		if hit.KeyID == "" {
			return
		}
		if a.settingsOSKOpenLocked() {
			a.settingsOSKField.OSK.SelectID(hit.KeyID)
		} else if a.nameEntryOpenLocked() {
			a.nameField.OSK.SelectID(hit.KeyID)
		} else if a.searchOpen {
			a.searchField.OSK.SelectID(hit.KeyID)
		}
	case PointerSettings:
		if hit.Index >= 0 {
			a.settingsIndex = hit.Index
		}
	case PointerFilters:
		if hit.Index >= 0 {
			a.filterIndex = hit.Index
		}
	case PointerViewPicker:
		if hit.Index >= 0 {
			a.viewPickerIndex = hit.Index
		}
	case PointerCollectionMenu:
		if a.collectionManageOpen && !a.collectionConfirmOpen && hit.Index >= 0 {
			a.collectionManageIndex = hit.Index
		}
	case PointerRoomPicker:
		if hit.Index >= 0 {
			a.roomPickerIndex = hit.Index
		}
	case PointerFirmwarePicker:
		if hit.Index >= 0 && hit.Index < len(a.firmwarePickerRows) {
			a.firmwarePickerIndex = hit.Index
		}
	case PointerTapePicker:
		if hit.Index >= 0 && hit.Index < len(a.tapePickerRows) {
			a.tapePickerIndex = hit.Index
		}
	case PointerRoomChoice:
		if hit.Index >= 0 && hit.Index < len(a.roomChoice) {
			a.roomChoiceIndex = hit.Index
		}
	case PointerRoom:
		if a.room != nil && hit.KeyID != "" && a.room.Err() == nil {
			a.room.Hover(hit.KeyID)
			a.applyRoomActionsLocked()
		}
	}
}

func (a *App) activatePointerHitLocked(hit PointerHit, now time.Time) {
	switch hit.Kind {
	case PointerCatalog, PointerDetail:
		if a.room != nil && a.detailOpen {
			a.applyRoomDestinationConfirmLocked()
			return
		}
		if a.detailOpen && hit.Kind == PointerCatalog {
			a.closeDetailLocked()
		}
		a.startLaunchLocked()
	case PointerCarousel:
		a.stepCarouselLocked(1)
	case PointerOSK:
		if a.settingsOSKOpenLocked() {
			a.handleSettingsOSKLocked(CmdSelect)
		} else if a.nameEntryOpenLocked() {
			a.handleNameEntryLocked(CmdSelect)
		} else if a.searchOpen {
			a.handleSearchLocked(CmdSelect, now)
		}
	case PointerSettings:
		a.handleSettingsLocked(CmdSelect)
	case PointerFilters:
		a.handleFiltersLocked(CmdSelect)
	case PointerViewPicker:
		a.handleViewPickerLocked(CmdSelect)
	case PointerCollectionMenu:
		a.handleViewPickerLocked(CmdSelect)
	case PointerRoomPicker:
		a.handleRoomPickerLocked(CmdSelect)
	case PointerFirmwarePicker:
		a.handleFirmwarePickerLocked(CmdSelect)
	case PointerTapePicker:
		a.handleTapePickerLocked(CmdSelect)
	case PointerRoomChoice:
		a.handleRoomChoiceLocked(CmdSelect)
	case PointerLaunchOverlay:
		a.handleLaunchOverlayLocked(CmdSelect)
	case PointerRoom:
		if a.launchOverlayActiveLocked() {
			return
		}
		if a.room != nil && hit.KeyID != "" && a.room.Err() == nil {
			a.room.Activate(hit.KeyID)
			a.applyRoomActionsLocked()
			if a.room != nil {
				a.applyRoomDestinationConfirmLocked()
			}
		}
	case PointerRoomDestination:
		a.openRoomDetailsLocked()
	case PointerBackdrop:
		a.pointerBackdropLocked(now)
	}
}

func (a *App) pointerBackdropLocked(now time.Time) {
	switch {
	case a.settingsOSKOpenLocked():
		a.handleSettingsOSKLocked(CmdBack)
	case a.nameEntryOpenLocked():
		a.handleNameEntryLocked(CmdBack)
	case a.searchOpen:
		a.handleSearchLocked(CmdBack, now)
	case a.collectionConfirmOpen, a.collectionManageOpen, a.viewPickerOpen:
		a.handleViewPickerLocked(CmdBack)
	case a.roomPickerOpen:
		a.handleRoomPickerLocked(CmdBack)
	case a.firmwarePickerOpen:
		a.handleFirmwarePickerLocked(CmdBack)
	case a.tapePickerOpen:
		a.handleTapePickerLocked(CmdBack)
	case a.settingsOpen:
		a.handleSettingsLocked(CmdBack)
	case a.filtersOpen:
		a.handleFiltersLocked(CmdBack)
	case a.launchOverlayActiveLocked():
		a.handleLaunchOverlayLocked(CmdBack)
	case a.roomChoiceOpen, a.detailOpen && a.room != nil:
		a.closeRoomOverlaysLocked()
	}
}
