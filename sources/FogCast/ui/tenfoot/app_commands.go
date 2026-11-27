package tenfoot

import (
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// HandleCommand applies a gamepad, USB-keyboard, or pointer-mapped command.
func (a *App) HandleCommand(cmd Command, now time.Time) {
	if cmd == CmdNone {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handleCommandLocked(cmd, now)
}

func (a *App) handleCommandLocked(cmd Command, now time.Time) {
	if a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing) {
		return
	}
	a.traceInputLocked(cmd, now)
	a.noteActivityLocked(now)
	if a.sessionDisplayBusy {
		if cmd == CmdStop {
			a.startStopLocked()
		}
		return
	}
	if cmd == CmdBack && a.sessionDisplayUncertain {
		a.beginSessionDisplayLocked(a.sessionReturnDisplayLocked(), false)
		return
	}
	if a.mediaControlsDirect && a.roomDuringPlay {
		if cmd == CmdSettings {
			return
		}
		if !a.tapePickerOpen {
			switch cmd {
			case CmdBack, CmdHome, CmdLayoutCycle:
				a.resumeRoomSessionLocked()
			case CmdStop:
				a.startStopLocked()
			}
			return
		}
	}
	if a.driveSettings.Open {
		if a.settingsOSKOpenLocked() {
			a.handleSettingsOSKLocked(cmd)
		} else {
			a.handleDriveSettingsLocked(cmd)
		}
		return
	}
	if a.coreLibrary.Open {
		if a.settingsOSKOpenLocked() {
			a.handleSettingsOSKLocked(cmd)
		} else {
			a.handleCoreLibraryLocked(cmd)
		}
		return
	}
	if a.consumeAttractLocked(cmd, now) {
		return
	}
	if a.searchOpen || a.nameEntryOpenLocked() || a.settingsOSKOpenLocked() {
		switch cmd {
		case CmdSafeAreaIn, CmdSafeAreaOut, CmdLayoutCycle:
			return
		}
	}
	roomOwnsInput := a.room != nil && !a.roomPickerOpen && !a.settingsOpen && !a.settingsOSKOpenLocked() && !a.firmwarePickerOpen && !a.tapePickerOpen && !a.firmwareCard.Open
	switch cmd {
	case CmdSafeAreaIn:
		a.setSafeAreaPctLocked(a.safeAreaPct+safeAreaNudge, true)
		a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		return
	case CmdSafeAreaOut:
		a.setSafeAreaPctLocked(a.safeAreaPct-safeAreaNudge, true)
		a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		return
	case CmdLayoutCycle:
		if a.firmwarePickerOpen || a.tapePickerOpen || a.firmwareCard.Open {
			return
		}
		if a.sessionStopOfferedLocked() {
			if a.roomDuringPlay {
				a.resumeRoomSessionLocked()
			} else {
				a.openPlayingHardwareRoomLocked()
			}
			return
		}
		if roomOwnsInput {
			a.handleRoomLocked(cmd)
			return
		}
		a.cycleLayoutLocked()
		return
	case CmdHome:
		if a.settingsOSKOpenLocked() || a.settingsOpen || a.searchOpen || a.nameEntryOpenLocked() {
			return
		}
		if a.tapePickerOpen {
			a.dismissMediaControlsLocked()
			return
		}
		if a.sessionStopOfferedLocked() {
			if a.roomDuringPlay {
				a.toggleRoomPickerLocked()
			} else {
				a.openPlayingHardwareRoomLocked()
			}
			return
		}
		if a.firmwarePickerOpen {
			a.closeFirmwarePickerLocked()
		}
		a.closeFirmwareCardLocked()
		if !a.openHomeRoomAsRootLocked() {
			a.toggleRoomPickerLocked()
		}
		return
	case CmdSettings:
		if a.tapePickerOpen {
			a.closeTapePickerLocked()
		}
		if a.firmwarePickerOpen {
			a.closeFirmwarePickerLocked()
		}
		a.closeFirmwareCardLocked()
		if a.settingsOpen {
			a.closeSettingsLocked()
		} else {
			a.openSettingsLocked()
		}
		return
	case CmdFilters:
		if a.firmwarePickerOpen || a.tapePickerOpen || a.firmwareCard.Open {
			return
		}
		if roomOwnsInput {
			a.handleRoomLocked(cmd)
			return
		}
		if a.filtersOpen {
			a.closeFiltersLocked()
		} else {
			a.openFiltersLocked()
		}
		return
	}
	if a.settingsOSKOpenLocked() {
		a.handleSettingsOSKLocked(cmd)
		return
	}
	// Settings › Firmware opens a family's card over Settings.
	if a.firmwareCard.Open {
		a.handleFirmwareCardLocked(cmd)
		return
	}
	if a.settingsOpen {
		a.handleSettingsLocked(cmd)
		return
	}
	if a.tapePickerOpen {
		a.handleTapePickerLocked(cmd)
		return
	}
	if a.firmwarePickerOpen {
		a.handleFirmwarePickerLocked(cmd)
		return
	}
	if a.roomPickerOpen && (!a.sessionStopOfferedLocked() || a.roomDuringPlay) {
		a.handleRoomPickerLocked(cmd)
		return
	}
	if a.room != nil && (!a.sessionStopOfferedLocked() || a.roomDuringPlay) {
		if cmd == CmdStop {
			a.startStopLocked()
			return
		}
		if cmd == CmdBack && a.roomDuringPlay && !a.roomChoiceOpen && !a.detailOpen {
			a.resumeRoomSessionLocked()
			return
		}
		a.handleRoomLocked(cmd)
		return
	}
	if a.filtersOpen {
		a.handleFiltersLocked(cmd)
		return
	}
	if a.nameEntryOpenLocked() {
		a.handleNameEntryLocked(cmd)
		return
	}
	if a.viewPickerOpen {
		a.handleViewPickerLocked(cmd)
		return
	}
	if a.searchOpen {
		a.handleSearchLocked(cmd, now)
		return
	}
	if a.sessionStopOfferedLocked() && !a.searchOpen {
		switch cmd {
		case CmdBack, CmdStop:
			a.startStopLocked()
			return
		case CmdSelect:
			if a.room != nil {
				if dest := a.roomDestinationLocked(); dest.Confirm() == rooms.ConfirmLauncherAction {
					a.runLauncherActionLocked(dest.LauncherAction)
				}
			}
			return
		case CmdSearch, CmdDetails:
			if a.sessionLiveMediaOfferedLocked() || (!a.roomDuringPlay && a.colecoCartridgeOfferedLocked()) {
				a.openTapePickerLocked()
			}
			return
		case CmdSortCycle:
			a.startInputToggleLocked()
			return
		case CmdUp, CmdDown, CmdLeft, CmdRight, CmdFilterPrev, CmdFilterNext, CmdViewPrev, CmdViewNext, CmdViewPicker, CmdFavorite, CmdFilters, CmdSafeAreaIn, CmdSafeAreaOut, CmdTab, CmdTabPrev:
			return
		}
	}
	if a.handleBrowseActionsLocked(cmd) {
		return
	}
	if cmd == CmdSearch && a.affinity.current.Kind == InputGamepad {
		if d, _, ok := a.browseActionsDestinationLocked(); ok {
			if _, hasSecondary := browseActionLabels(d); hasSecondary {
				a.openDetailLocked()
				return
			}
		}
	}
	if a.handleDetailLocked(cmd) {
		return
	}
	switch cmd {
	case CmdUp:
		a.moveFocusLocked(0, -1)
	case CmdDown:
		a.moveFocusLocked(0, 1)
	case CmdLeft:
		a.moveFocusLocked(-1, 0)
	case CmdRight:
		a.moveFocusLocked(1, 0)
	case CmdSelect:
		a.startLaunchLocked()
	case CmdStop:
		a.startStopLocked()
	case CmdBack:
		if a.launch.Phase == "launching" {
			a.status = "launch in progress"
		} else {
			a.returnFromLibraryLocked()
		}
	case CmdFilterPrev:
		a.cyclePlatformLocked(-1)
	case CmdFilterNext:
		a.cyclePlatformLocked(1)
	case CmdSortCycle:
		a.cycleSortLocked()
	case CmdSearch, CmdTab:
		a.openSearchLocked()
	case CmdDetails:
		a.openDetailLocked()
	case CmdViewPrev:
		a.cycleViewLocked(-1)
	case CmdViewNext:
		a.cycleViewLocked(1)
	case CmdViewPicker:
		a.openViewPickerLocked()
	case CmdFavorite:
		a.toggleFavoriteLocked()
	case CmdTabPrev:
		a.openFiltersLocked()
	}
}

// NoteActivity records input so idle/attract timers reset.
func (a *App) NoteActivity(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
}

// Press records a button down, including hold-repeat bookkeeping.
func (a *App) Press(cmd Command, now time.Time) Command {
	fired := a.repeat.Down(cmd, now)
	if fired != CmdNone && fired != CmdQuit {
		a.HandleCommand(fired, now)
	}
	return fired
}

// Release records a button up.
func (a *App) Release(cmd Command) {
	a.repeat.Up(cmd)
}

// Tick drains async work and queues visible covers. It must not block.
func (a *App) Tick(now time.Time) Command {
	a.drainResults()
	a.drainAttractResults()
	a.mu.Lock()
	a.flushPendingPlayKeyReleasesLocked()
	a.frameLua = rooms.StepTiming{}
	a.flushSearchLocked(now)
	a.tickAttractLocked(now)
	a.syncPreviewLocked()
	a.tickSessionPadLocked(now)
	a.tickLearnLocked(now)
	a.syncSurroundLocked()
	// Stop drains a shown plane, so its frames fail without a display fault.
	if a.sessionDisplay.Visible && !a.sessionDisplayBusy && a.stopPhase != "stopping" && a.menuDisplay != nil {
		if err := a.menuDisplay.LastError(); err != nil {
			a.roomSessionNotice = sessionDisplayUnavailableNotice
		} else if a.roomSessionNotice == sessionDisplayUnavailableNotice {
			a.roomSessionNotice = ""
		}
	}
	if !a.attractActive {
		a.tickRoomLocked(now)
	}
	a.mu.Unlock()
	a.queueVisibleWork(now)
	if a.ForwardsPlayHID() {
		a.repeat.Clear()
	}
	if a.browseHoldEnabled() {
		if cmd := a.hold.Tick(now); cmd != CmdNone {
			a.HandleCommand(cmd, now)
			return cmd
		}
	}
	if cmd := a.repeat.Tick(now); cmd != CmdNone {
		a.HandleCommand(cmd, now)
		return cmd
	}
	return CmdNone
}

// SetDebugHUD enables the optional corner overlay (gen, lease ttl, last error).
func (a *App) SetDebugHUD(on bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.debugHUD = on
}

// SetGamepads records how many gamepads SDL currently has open.
func (a *App) SetGamepads(n int) {
	a.mu.Lock()
	a.gamepads = n
	a.mu.Unlock()
}

func (a *App) syncInputCountsLocked() {
	a.keyboards = a.affinity.count(InputKeyboard)
	a.mice = a.affinity.count(InputMouse)
	a.gamepads = a.affinity.count(InputGamepad)
}

func (a *App) noteInputLocked(kind InputKind, id int) {
	a.affinity.Note(kind, id)
	a.syncInputCountsLocked()
}

// SeedInput records a device present at start without claiming affinity.
func (a *App) SeedInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.Seed(kind, id)
	a.syncInputCountsLocked()
}

// AttachInput records a hotplug. A newly seen keyboard, mouse, or gamepad
// claims hint and focus ownership without moving catalog focus.
func (a *App) AttachInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.Attach(kind, id)
	a.syncInputCountsLocked()
}

// DetachInput drops a device. Unplugging the owner restores a remaining one.
func (a *App) DetachInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.Detach(kind, id)
	if kind == InputGamepad {
		a.dropPadInfoLocked(id)
	}
	a.syncInputCountsLocked()
}

// NoteInput marks last-used input so hints follow that device.
func (a *App) NoteInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteInputLocked(kind, id)
}

// FinishInputSeed picks a startup owner: gamepad if any, else keyboard, else mouse.
func (a *App) FinishInputSeed() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.preferStartup()
	a.syncInputCountsLocked()
}
