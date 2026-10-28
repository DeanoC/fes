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
	Details   []string
	Primary   string
	Filled    bool
	Secondary string
}

// heroPlaysOnThisTV is the destination of a kit-local core: the kit's own
// HDMI output, the screen this menu is on.
const heroPlaysOnThisTV = "Plays on this TV"

// heroStopCopy names every way to stop a kit-local core, before Play: the
// menu cannot draw once the core owns HDMI. It is one line so the one-second
// hold stays visible in the hero column at 1280×720 with the kit's default
// 5% safe area, and at 1024×600.
const heroStopCopy = "Stop: F12, Guide, or hold Select+Start for one second."

// waitAckCopy answers a Confirm received while Checking.
func waitAckCopy(kind InputKind) string {
	return "Still checking. Press " + selectWord(kind) + " again once it has finished."
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
		switch d.Confirm() {
		case rooms.ConfirmLaunchCore:
			c.State = heroStateReady
			c.Headline = heroPlaysOnThisTV
			c.Explain = heroStopCopy
			c.Primary, c.Filled = "Play", true
			c.Secondary = "Details"
		case rooms.ConfirmWait:
			c.State = heroStateChecking
			c.Headline = strings.TrimSpace(d.Status)
		default:
			c.State = heroStateUnavailable
			c.Headline, c.Explain = heroCoreBlocked(d)
			c.Primary = "Details"
		}
		if snap.Room.CoreDetail {
			c.Kicker = "Details"
			c.Explain = ""
			c.Details = heroCoreDetails(d)
			if c.Primary == "Details" {
				c.Primary = ""
			}
			c.Secondary = ""
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
			c.Secondary = "Details"
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
	return c
}

// kitSaysPrefix marks the Details line that quotes the kit. planHero drops
// the identity lines ahead of it before this one.
const kitSaysPrefix = "Kit says: "

// heroCoreDetails is the inspectable identity of a selected core: its
// core id and the kit's verbatim local status. An installed package also
// shows a short package id. An omitted optional core has only a synthetic
// id and no package in the image, so that line is left out.
func heroCoreDetails(d rooms.Destination) []string {
	var lines []string
	if id := strings.TrimSpace(d.CoreID); id != "" {
		lines = append(lines, "Core: "+id)
	}
	// An omitted core's package id is synthetic. It is not in the image, so
	// Details names the core and the kit's block without a package line.
	if pkg := strings.TrimSpace(d.PackageID); pkg != "" && d.CoreNeeds != "unavailable" {
		if len(pkg) > 12 {
			pkg = pkg[:12] + "…"
		}
		lines = append(lines, "Package: "+pkg)
	}
	status := strings.TrimSpace(d.CoreBlock)
	if status == "" {
		status = strings.TrimSpace(d.Status)
	}
	if status != "" {
		lines = append(lines, kitSaysPrefix+status)
	}
	return lines
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
	line, ok := roomText(labels, used, rooms.Op{Kind: rooms.OpText, Text: text, Size: size, Bold: bold})
	if !ok {
		return gfx.TextHeightWeight(size, weight)
	}
	drawMask(dev, line, x, y, color)
	return line.h
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
	w := heroButtonWidth(prompt, label)
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

// pillStyle is the readiness pill's palette and size. The Lounge hero sits
// on light paper; the Library Details pane is dark.
type pillStyle struct {
	px                     int
	ready, onReady         gfx.Color
	unavailable, bar, text gfx.Color
	outline                gfx.Color
}

var heroPill = pillStyle{px: heroStatePx, ready: heroAccent, onReady: heroOnAccent,
	unavailable: heroRaised, bar: heroText, text: heroText, outline: heroSecondary}

// Details and its labelled cover plates share the warm Library surfaces.
var detailPill = heroPill

// heroStatePill draws the readiness name. Ready is filled; the others are
// outlined, and Unavailable carries a solid leading bar, so shape and text
// carry the state as well as colour.
func heroStatePill(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y int, state string) (int, int) {
	return statePill(dev, labels, used, x, y, state, heroPill)
}

func statePill(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y int, state string, st pillStyle) (int, int) {
	th := gfx.TextHeightWeight(st.px, gfx.WeightBold)
	h := th + st.px/2
	lead := st.px * 7 / 10
	if state == heroStateUnavailable {
		lead = st.px * 6 / 5
	}
	w := gfx.MeasureTextWeight(state, st.px, gfx.WeightBold) + lead + st.px*7/10
	fg := st.text
	switch state {
	case heroStateReady:
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, st.ready)
		fg = st.onReady
	case heroStateUnavailable:
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, st.unavailable)
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(st.px * 2 / 5), H: float32(h)}, st.bar)
	default:
		heroOutline(dev, x, y, w, h, 2, st.outline)
	}
	heroLine(dev, labels, used, x+lead, y+(h-th)/2, w-lead, st.px, true, fg, state)
	return w, h
}

// Pointer keys for the hero's buttons.
const (
	heroPrimaryKey   = "hero-primary"
	heroSecondaryKey = "hero-secondary"
	heroButtonH      = 48
	heroButtonGap    = 14
)

func heroButtonWidth(prompt, label string) int {
	return gfx.MeasureTextWeight(strings.TrimSpace(prompt+"  "+label), heroButtonPx, gfx.WeightBold) + 40
}

// heroButtonRects is where the primary and secondary actions are drawn;
// pointer hit-testing uses the same rectangles.
func heroButtonRects(snap Snapshot, pane rectI) (primary, secondary rectI) {
	c := heroDestinationCopy(snap)
	kind := snap.Affinity
	y := pane.Y + pane.H - heroButtonH
	x := pane.X
	if c.Primary != "" {
		primary = rectI{X: x, Y: y, W: heroButtonWidth(selectWord(kind), c.Primary), H: heroButtonH}
		x += primary.W + heroButtonGap
	}
	if c.Secondary != "" {
		prompt := detailsWord(kind)
		if snap.BrowseActions.Focused && snap.BrowseActions.Index == 1 {
			prompt = selectWord(kind)
		}
		secondary = rectI{X: x, Y: y, W: heroButtonWidth(prompt, c.Secondary), H: heroButtonH}
	}
	return primary, secondary
}

// heroPlan fits the text column above the buttons. Long titles and
// explanations shrink before anything can run into an action.
type heroPlan struct {
	kicker       bool
	titleLines   []string
	explainLines []string
	details      []string
	ack          bool
}

func lineStep(size int, w gfx.Weight) int { return gfx.TextHeightWeight(size, w) + size/6 }

func planHero(c heroCopy, w, avail int) heroPlan {
	fixed := 0
	kickerH := gfx.TextHeightWeight(heroKickerPx, gfx.WeightBold) + 6
	if c.Meta != "" {
		fixed += gfx.TextHeight(heroMetaPx)
	}
	fixed += 12
	if c.State != "" || c.Headline != "" {
		fixed += max(gfx.TextHeight(heroLinePx), gfx.TextHeightWeight(heroStatePx, gfx.WeightBold)+10) + 8
	}
	p := heroPlan{
		titleLines:   gfx.WrapTextWeight(c.Title, heroTitlePx, w, 2, gfx.WeightBold),
		explainLines: gfx.WrapTextWeight(c.Explain, heroExplainPx, w, 2, gfx.WeightRegular),
		details:      c.Details,
		ack:          c.Ack != "",
		kicker:       c.Kicker != "",
	}
	need := func() int {
		h := fixed
		if p.kicker {
			h += kickerH
		}
		h += len(p.titleLines) * lineStep(heroTitlePx, gfx.WeightBold)
		h += (len(p.explainLines) + len(p.details)) * lineStep(heroExplainPx, gfx.WeightRegular)
		if p.ack {
			h += lineStep(heroExplainPx, gfx.WeightBold) + 4
		}
		return h
	}
	if need() > avail && len(p.explainLines) > 1 {
		p.explainLines = gfx.WrapTextWeight(c.Explain, heroExplainPx, w, 1, gfx.WeightRegular)
	}
	if need() > avail && len(p.titleLines) > 1 {
		p.titleLines = gfx.WrapTextWeight(c.Title, heroTitlePx, w, 1, gfx.WeightBold)
	}
	if need() > avail {
		p.kicker = false
	}
	// The kit's block is why Details was opened. Identity lines (core id,
	// package id) go before it does; the explanation goes before it too.
	trimHeroDetails(&p, need, avail, true)
	if need() > avail {
		p.explainLines = nil
	}
	trimHeroDetails(&p, need, avail, false)
	return p
}

// trimHeroDetails drops detail lines while they overflow. keepKit retains
// the kitSaysPrefix line until a later pass.
func trimHeroDetails(p *heroPlan, need func() int, avail int, keepKit bool) {
	for need() > avail && len(p.details) > 0 {
		kit := -1
		for i, line := range p.details {
			if strings.HasPrefix(line, kitSaysPrefix) {
				kit = i
				break
			}
		}
		switch {
		case kit >= 0 && kit < len(p.details)-1:
			p.details = p.details[:len(p.details)-1]
		case kit > 0:
			p.details = p.details[1:]
		case kit == 0 && keepKit:
			return
		default:
			p.details = p.details[:len(p.details)-1]
		}
	}
}

// heroTextBudget is the height planHero may use inside the text column.
// The button row is reserved only when a button is drawn, so a Details
// view with no actions can use that space for the kit's block.
func heroTextBudget(c heroCopy, paneH int) int {
	bottom := 8
	if c.Primary != "" || c.Secondary != "" {
		bottom += heroButtonH
	}
	if paneH <= bottom {
		return 0
	}
	return paneH - bottom
}

// drawHeroDestination is the FES-owned selected-destination panel of a
// hero room. Readiness, destination and actions come from the launcher's
// classification, never from the room script's drawing.
func drawHeroDestination(dev gfx.Device, snap Snapshot, pane rectI, labels map[string]gpuTexture, used map[string]struct{}) {
	c := heroDestinationCopy(snap)
	kind := snap.Affinity
	x, y, w := pane.X, pane.Y, pane.W
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(pane.H)}, heroSurface)
	plan := planHero(c, w, heroTextBudget(c, pane.H))
	cursor := y
	if plan.kicker {
		cursor += heroLine(dev, labels, used, x, cursor, w, heroKickerPx, true, heroSecondary, strings.ToUpper(c.Kicker)) + 6
	}
	for _, line := range plan.titleLines {
		heroLine(dev, labels, used, x, cursor, w, heroTitlePx, true, heroText, line)
		cursor += lineStep(heroTitlePx, gfx.WeightBold)
	}
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
	for _, line := range append(append([]string{}, plan.explainLines...), plan.details...) {
		heroLine(dev, labels, used, x, cursor, w, heroExplainPx, false, heroSecondary, line)
		cursor += lineStep(heroExplainPx, gfx.WeightRegular)
	}
	if plan.ack {
		heroLine(dev, labels, used, x, cursor+4, w, heroExplainPx, true, heroAccent, c.Ack)
	}
	primary, secondary := heroButtonRects(snap, pane)
	if c.Primary != "" {
		heroButton(dev, labels, used, primary.X, primary.Y, primary.H, selectWord(kind), c.Primary, c.Filled)
	}
	if c.Secondary != "" {
		prompt := detailsWord(kind)
		if snap.BrowseActions.Focused && snap.BrowseActions.Index == 1 {
			prompt = selectWord(kind)
		}
		heroButton(dev, labels, used, secondary.X, secondary.Y, secondary.H, prompt, c.Secondary, false)
	}
	if snap.BrowseActions.Focused {
		focused := primary
		if snap.BrowseActions.Index == 1 {
			focused = secondary
		}
		if focused.W > 0 {
			heroOutline(dev, focused.X-4, focused.Y-4, focused.W+8, focused.H+8, 3, heroAccent)
		}
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
	if snap.BrowseActions.Focused {
		return browseActionsHint(snap)
	}
	kind := snap.Affinity
	move := "D-pad move"
	switch kind {
	case InputKeyboard:
		move = "Arrows move"
	case InputMouse:
		move = "Click to choose"
	}
	if snap.Room.CoreDetail {
		if snap.Room.Destination.Confirm() == rooms.ConfirmLaunchCore {
			return fmt.Sprintf("%s play  ·  %s close details", selectWord(kind), backWord(kind))
		}
		return backWord(kind) + " close details"
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
	status := heroFooterStatus(snap)
	hintMaxW := f.W - 2*margin
	if status != "" {
		hintMaxW = f.W * 2 / 3
	}
	hintW := min(gfx.MeasureText(hint, heroFooterPx), hintMaxW)
	heroLine(dev, labels, used, x+f.W-margin-hintW, textY, hintW, heroFooterPx, false, heroSecondary, hint)
	if status != "" {
		heroLine(dev, labels, used, x+margin, textY, f.W-2*margin-hintW-24, heroFooterPx, true, heroText, status)
	}
}
