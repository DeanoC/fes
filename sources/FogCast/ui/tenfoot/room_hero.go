package tenfoot

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// Lounge colours for the FES-owned hero panel and footer. They are the
// light seed roles in docs/ux/visual-language.md, used only by rooms that
// choose destination_panel = "hero". The room draws its own card in
// heroSurface so this text column sits on a matching opaque ground.
var (
	heroBackground = gfx.RGBA(0xF4, 0xF0, 0xE8, 0xFF)
	heroSurface    = gfx.RGBA(0xFF, 0xFC, 0xF5, 0xFF)
	heroRaised     = gfx.RGBA(0xE9, 0xE6, 0xDC, 0xFF)
	heroText       = gfx.RGBA(0x24, 0x36, 0x31, 0xFF)
	heroSecondary  = gfx.RGBA(0x53, 0x62, 0x5A, 0xFF)
	heroDivider    = gfx.RGBA(0xCE, 0xCF, 0xC0, 0xFF)
	heroAccent     = gfx.RGBA(0x23, 0x5A, 0x46, 0xFF)
	heroOnAccent   = gfx.RGBA(0xFC, 0xFC, 0xF3, 0xFF)
)

// Hero type roles at the 1280×720 baseline. They follow the visual-language
// seeds, slightly reduced for the supporting lines so a two-line title,
// readiness, explanation and actions fit the card without scrolling.
const (
	heroKickerPx  = 16
	heroTitlePx   = 36
	heroMetaPx    = 20
	heroStatePx   = 20
	heroLinePx    = 22
	heroExplainPx = 20
	heroButtonPx  = 22
	heroFooterPx  = 18
)

// Readiness names shown in the hero. Missing is a reason within Unavailable.
const (
	heroStateChecking    = "Checking"
	heroStateReady       = "Ready"
	heroStateNeedsChoice = "Needs a choice"
	heroStateUnavailable = "Unavailable"
)

// heroCopy is everything the hero text column says about the selection.
type heroCopy struct {
	Kicker    string
	Title     string
	Meta      string
	State     string
	Headline  string
	Explain   string
	Ack       string
	Primary   string
	Filled    bool
	Secondary string
}

// heroPlaysOnThisTV is the destination of a kit-local core: the kit's own
// HDMI output, the screen this menu is on.
const heroPlaysOnThisTV = "Plays on this TV"

// waitAckCopy answers a Confirm received while Checking.
func waitAckCopy(kind InputKind) string {
	return "Still checking. Press " + selectWord(kind) + " again once this says Ready."
}

// heroInputsLine names required input devices before Play.
func heroInputsLine(inputs []string) string {
	var keyboard, mouse bool
	for _, name := range inputs {
		switch name {
		case "keyboard":
			keyboard = true
		case "mouse":
			mouse = true
		}
	}
	switch {
	case keyboard && mouse:
		return "Needs a keyboard and mouse"
	case keyboard:
		return "Needs a keyboard"
	case mouse:
		return "Needs a mouse"
	default:
		return ""
	}
}

func heroState(a rooms.Availability) string {
	switch a {
	case rooms.AvailChecking:
		return heroStateChecking
	case rooms.AvailReady:
		return heroStateReady
	case rooms.AvailNeedsChoice:
		return heroStateNeedsChoice
	case rooms.AvailUnavailable, rooms.AvailMissing:
		return heroStateUnavailable
	default:
		return ""
	}
}

// heroCoreBlocked turns an installed core's requirement into a consequence
// and the remedy that exists from this screen. The verbatim block stays in
// Details.
func heroCoreBlocked(d rooms.Destination) (headline, explain string) {
	switch {
	case d.LeaseHeld || d.Status == rooms.InUseStatus:
		return rooms.InUseStatus, rooms.InUseDetail
	case d.CoreBlock == machineStatusUnknown:
		return machineStatusUnknown, "Wait for the kit to answer before starting anything."
	}
	switch d.CoreNeeds {
	case "parts":
		return "Display setup incomplete", "This screen can't add its video part. Start it from the FogCast library instead."
	case "firmware":
		return "Needs its system firmware", "Import the firmware in FogCast on the computer that manages your library."
	case "media":
		return "Needs a cartridge", "Choose a game for it from the Library."
	}
	block := strings.TrimSpace(d.CoreBlock)
	if block == "" {
		block = strings.TrimSpace(d.Status)
	}
	if block == "" {
		block = "This can't start here."
	}
	return block, "It can't start from this screen."
}

func heroDestinationCopy(snap Snapshot) heroCopy {
	d := snap.Room.Destination
	kind := snap.Affinity
	c := heroCopy{Title: strings.TrimSpace(d.Label)}
	switch d.Kind {
	case rooms.KindCore:
		c.Kicker = "On this kit"
		c.Meta = heroInputsLine(d.Inputs)
		if d.Confirm() == rooms.ConfirmLaunchCore {
			c.State = heroStateReady
			c.Headline = heroPlaysOnThisTV
			c.Explain = "To stop, hold Select and Start together for one second."
			c.Primary, c.Filled = "Play", true
		} else {
			c.State = heroStateUnavailable
			c.Headline, c.Explain = heroCoreBlocked(d)
			c.Primary = "Details"
		}
	case rooms.KindRoom:
		c.Kicker = "Room"
		c.Headline = "Another way to browse what's here."
		c.Primary, c.Filled = "Enter", true
	case rooms.KindLibrary:
		c.Kicker = "Library"
		c.Headline = "Browse every title in your library."
		c.Primary, c.Filled = "Open library", true
	case rooms.KindAction:
		c.Kicker = "System"
		c.Headline = "Display, home screen, targets and kit tools."
		c.Primary, c.Filled = "Open", true
	default:
		c.Kicker = strings.ToUpper(strings.TrimSpace(d.System))
		c.Meta = strings.TrimSpace(d.History.Line())
		if inputs := heroInputsLine(d.Inputs); inputs != "" {
			if c.Meta != "" {
				c.Meta = inputs + "  ·  " + c.Meta
			} else {
				c.Meta = inputs
			}
		}
		c.State = heroState(d.Availability)
		c.Headline = strings.TrimSpace(d.Status)
		if action := strings.TrimSpace(d.Action); action != c.Headline {
			c.Explain = action
		}
		switch d.Confirm() {
		case rooms.ConfirmLaunch, rooms.ConfirmLaunchKit:
			c.Primary, c.Filled = "Play", true
		case rooms.ConfirmChoose:
			c.Primary = "Choose"
		case rooms.ConfirmOpenLibrary:
			c.Primary = "Open library"
		case rooms.ConfirmImportFirmware:
			c.Primary = "Import"
		case rooms.ConfirmExplain:
			c.Primary = "Details"
		}
		if c.State == heroStateChecking {
			c.Explain = ""
		}
	}
	if c.Title == "" {
		c.Title = "Selected"
	}
	if snap.Room.WaitAck {
		c.Ack = waitAckCopy(kind)
	}
	if c.Filled && (d.Kind == rooms.KindCore || d.Kind == rooms.KindGame || d.Kind == "") {
		c.Secondary = "Details"
	}
	return c
}

// heroLine draws one line of text in colour and returns its height.
func heroLine(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y, maxW, size int, bold bool, color gfx.Color, text string) int {
	text = strings.TrimSpace(text)
	if text == "" || maxW < 1 {
		return 0
	}
	weight := gfx.WeightRegular
	if bold {
		weight = gfx.WeightBold
	}
	text = gfx.FitTextWeight(text, size, maxW, weight)
	tex, ok := roomTextTexture(dev, labels, used, rooms.Op{Kind: rooms.OpText, Text: text, Size: size, Bold: bold, Color: color})
	if !ok {
		return gfx.TextHeightWeight(size, weight)
	}
	dev.Draw(tex.tex, nil, gfx.Rect{X: float32(x), Y: float32(y), W: float32(tex.w), H: float32(tex.h)})
	return tex.h
}

func heroWrapped(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y, maxW, size, lines int, bold bool, color gfx.Color, text string) int {
	weight := gfx.WeightRegular
	if bold {
		weight = gfx.WeightBold
	}
	step := gfx.TextHeightWeight(size, weight) + size/6
	h := 0
	for _, line := range gfx.WrapTextWeight(text, size, maxW, lines, weight) {
		heroLine(dev, labels, used, x, y+h, maxW, size, bold, color, line)
		h += step
	}
	return h
}

func heroOutline(dev gfx.Device, x, y, w, h, t int, c gfx.Color) {
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(t)}, c)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y + h - t), W: float32(w), H: float32(t)}, c)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(t), H: float32(h)}, c)
	dev.FillRect(gfx.Rect{X: float32(x + w - t), Y: float32(y), W: float32(t), H: float32(h)}, c)
}

// heroButton draws one action with its prompt word and returns its width.
// Filled is the primary action; an outline is a secondary one.
func heroButton(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y, h int, prompt, label string, filled bool) int {
	text := strings.TrimSpace(prompt + "  " + label)
	w := gfx.MeasureTextWeight(text, heroButtonPx, gfx.WeightBold) + 40
	fg := heroText
	if filled {
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroAccent)
		fg = heroOnAccent
	} else {
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroSurface)
		heroOutline(dev, x, y, w, h, 2, heroDivider)
	}
	th := gfx.TextHeightWeight(heroButtonPx, gfx.WeightBold)
	heroLine(dev, labels, used, x+20, y+(h-th)/2, w-40, heroButtonPx, true, fg, text)
	return w
}

// heroStatePill draws the readiness name. Ready is filled; the others are
// outlined, and Unavailable carries a solid leading bar, so shape and text
// carry the state as well as colour.
func heroStatePill(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y int, state string) (int, int) {
	th := gfx.TextHeightWeight(heroStatePx, gfx.WeightBold)
	h := th + 10
	lead := 14
	if state == heroStateUnavailable {
		lead = 24
	}
	w := gfx.MeasureTextWeight(state, heroStatePx, gfx.WeightBold) + lead + 14
	fg := heroText
	switch state {
	case heroStateReady:
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroAccent)
		fg = heroOnAccent
	case heroStateUnavailable:
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroRaised)
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: 8, H: float32(h)}, heroText)
	default:
		heroOutline(dev, x, y, w, h, 2, heroSecondary)
	}
	heroLine(dev, labels, used, x+lead, y+5, w-lead, heroStatePx, true, fg, state)
	return w, h
}

// drawHeroDestination is the FES-owned selected-destination panel of a
// hero room. Readiness, destination and actions come from the launcher's
// classification, never from the room script's drawing.
func drawHeroDestination(dev gfx.Device, snap Snapshot, pane rectI, labels map[string]gpuTexture, used map[string]struct{}) {
	c := heroDestinationCopy(snap)
	kind := snap.Affinity
	x, y, w := pane.X, pane.Y, pane.W
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(pane.H)}, heroSurface)
	cursor := y
	if c.Kicker != "" {
		cursor += heroLine(dev, labels, used, x, cursor, w, heroKickerPx, true, heroSecondary, strings.ToUpper(c.Kicker)) + 6
	}
	cursor += heroWrapped(dev, labels, used, x, cursor, w, heroTitlePx, 2, true, heroText, c.Title)
	if c.Meta != "" {
		cursor += heroLine(dev, labels, used, x, cursor, w, heroMetaPx, false, heroSecondary, c.Meta)
	}
	cursor += 12
	if c.State != "" || c.Headline != "" {
		lineX := x
		rowH := gfx.TextHeight(heroLinePx)
		if c.State != "" {
			pw, ph := heroStatePill(dev, labels, used, x, cursor, c.State)
			lineX += pw + 14
			rowH = max(rowH, ph)
		}
		lineY := cursor + (rowH-gfx.TextHeight(heroLinePx))/2
		heroLine(dev, labels, used, lineX, lineY, x+w-lineX, heroLinePx, false, heroText, c.Headline)
		cursor += rowH + 8
	}
	buttonH := 48
	buttonY := y + pane.H - buttonH
	if c.Explain != "" {
		cursor += heroWrapped(dev, labels, used, x, cursor, w, heroExplainPx, 2, false, heroSecondary, c.Explain)
	}
	if c.Ack != "" {
		ackY := min(cursor+4, buttonY-gfx.TextHeightWeight(heroExplainPx, gfx.WeightBold)-8)
		heroLine(dev, labels, used, x, ackY, w, heroExplainPx, true, heroAccent, c.Ack)
	}
	bx := x
	if c.Primary != "" {
		bx += heroButton(dev, labels, used, bx, buttonY, buttonH, selectWord(kind), c.Primary, c.Filled) + 14
	}
	if c.Secondary != "" {
		heroButton(dev, labels, used, bx, buttonY, buttonH, detailsWord(kind), c.Secondary, false)
	}
}

// heroFooterStatus is the footer's left line: only conditions a player can
// act on. Lease generations and routine health stay in Settings/Details.
func heroFooterStatus(snap Snapshot) string {
	if !launchOverlayVisible(snap) {
		switch snap.Launch.Phase {
		case "launching", "error", "host":
			if msg := strings.TrimSpace(snap.Launch.Message); msg != "" {
				return msg
			}
		}
	}
	status := strings.TrimSpace(snap.Status)
	if strings.HasPrefix(status, "room failed") || status == localInUseCopy || status == machineStatusUnknown {
		return status
	}
	if local := strings.TrimSpace(snap.LocalCoreStatus); local != "" && local == status {
		return local
	}
	switch status {
	case localLaunchCheckingCopy, "Settings are unavailable during play.", "That action is not available.":
		return status
	}
	if snap.Health.Line != "" && (snap.Health.HostUnreachable || !snap.Health.TargetReachable) {
		return snap.Health.Line
	}
	// A foreign lease is the hero's In use; the raw lease line stays in
	// Settings. An unreachable lease service is still worth saying here.
	if snap.KitLease.Line != "" && snap.KitLease.Unreachable {
		return snap.KitLease.Line
	}
	return ""
}

// heroFooterHint names the routes on the affinity device in full words.
func heroFooterHint(snap Snapshot) string {
	kind := snap.Affinity
	move := "D-pad move"
	switch kind {
	case InputKeyboard:
		move = "Arrows move"
	case InputMouse:
		move = "Click to choose"
	}
	return fmt.Sprintf("%s  ·  %s select  ·  %s details  ·  %s back  ·  %s settings",
		move, selectWord(kind), detailsWord(kind), backWord(kind), settingsWord(kind))
}

// drawHeroChrome replaces the debug-font footer line in a hero room.
func drawHeroChrome(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	layout, ok := rooms.HeroPanel(snap.Room.Width, snap.Room.Height)
	if !ok {
		return
	}
	f := layout.Footer
	x, y := snap.Room.OffsetX+f.X, snap.Room.OffsetY+f.Y
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(f.W), H: float32(f.H)}, heroBackground)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(f.W), H: 2}, heroDivider)
	margin := layout.Card.X
	textY := y + (f.H-gfx.TextHeight(heroFooterPx))/2
	hint := heroFooterHint(snap)
	hintW := min(gfx.MeasureText(hint, heroFooterPx), f.W*2/3)
	heroLine(dev, labels, used, x+f.W-margin-hintW, textY, hintW, heroFooterPx, false, heroSecondary, hint)
	if status := heroFooterStatus(snap); status != "" {
		heroLine(dev, labels, used, x+margin, textY, f.W-2*margin-hintW-24, heroFooterPx, true, heroText, status)
	}
}
