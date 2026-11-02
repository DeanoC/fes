package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/corenames"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// userFirmwareFiles says which file a user supplies when FES cannot ship a
// model's firmware. A model with neither this nor bundled firmware gets no
// card.
var userFirmwareFiles = map[string]string{
	"fes.zx81":           "an 8 KiB ZX81 BASIC ROM",
	"fes.zx80":           "a 4 KiB ZX80 BASIC ROM",
	"fes.ts1000":         "an 8 KiB ZX81 or TS1000 ROM",
	"fes.ts1500":         "an 8 KiB TS1500 ROM",
	"fes.spectrum":       "a 16 KiB 48K ROM",
	"fes.spectrum128":    "a 32 KiB 128K or +2 ROM (ROM 0, then ROM 1)",
	"fes.spectrum-plus3": "a 64 KiB +2A or +3 ROM (ROMs 0 to 3)",
	"fes.pentagon":       "a 48 KiB image: the 128K ROMs, then TR-DOS",
	"fes.c64":            "one 36 KiB image: BASIC, KERNAL and character ROMs, then the 1541 drive ROM",
	"fes.atari-st":       "a 192 KiB TOS image",
}

// firmwareCardContent is the card's copy for one family.
type firmwareCardContent struct {
	Kicker    string
	Title     string
	Body      []string
	Licence   []string
	Footnote  []string
	Primary   string
	Secondary string
	// LicenceView is the Licence page: notices, sources and licence text.
	LicenceView []string
}

// firmwareCardState is the open card, or the request waiting for the host.
type firmwareCardState struct {
	Open, Loading, FirstUse, LicenceView bool
	Gen                                  int
	Cancel                               context.CancelFunc
	Family, FamilyLabel, CoreID          string
	Content                              firmwareCardContent
	Scroll                               int
}

// FirmwareCardSnapshot is the renderer's copy of the card.
type FirmwareCardSnapshot struct {
	Open        bool
	LicenceView bool
	Scroll      int
	Content     firmwareCardContent
}

type firmwareCardModel struct {
	CoreID, Label, ROMID string
	Size                 int64
}

// bundledFirmwareContracts mirrors the named ROM IDs and exact source sizes
// used by the corresponding package producers. A core ID alone is not enough
// to claim that a host's bundled image fits a model.
var bundledFirmwareContracts = map[string]struct {
	ROMID string
	Size  int64
}{
	"fes.spectrum":       {ROMID: "spectrum-firmware", Size: 16 * 1024},
	"fes.spectrum128":    {ROMID: "spectrum128-firmware", Size: 32 * 1024},
	"fes.spectrum-plus3": {ROMID: "spectrum-plus3-firmware", Size: 64 * 1024},
	"fes.atari-st":       {ROMID: "atari-st-firmware", Size: 192 * 1024},
}

func firmwareCardModelForCore(coreID, label string) firmwareCardModel {
	model := firmwareCardModel{CoreID: coreID, Label: label}
	if contract, ok := bundledFirmwareContracts[coreID]; ok {
		model.ROMID, model.Size = contract.ROMID, contract.Size
	}
	return model
}

// firmwareCardCopy builds the card for a family from the host's bundled
// list. ok is false when the family has no firmware to explain.
func firmwareCardCopy(familyLabel string, models []firmwareCardModel, bundled []protocol.BundledFirmware, firstUse bool) (firmwareCardContent, bool) {
	var builtIn []protocol.BundledFirmware
	var builtInLabels, needs []string
	for _, m := range models {
		if fw, ok := bundledForModel(bundled, m); ok {
			builtIn = append(builtIn, fw)
			builtInLabels = append(builtInLabels, m.Label)
			continue
		}
		if file, ok := userFirmwareFiles[m.CoreID]; ok {
			needs = append(needs, m.Label+" needs "+file+".")
		}
	}
	if len(builtIn) == 0 && len(needs) == 0 {
		return firmwareCardContent{}, false
	}
	c := firmwareCardContent{Kicker: familyLabel + " firmware", Primary: "Close"}
	if firstUse {
		c.Kicker = "First time on the " + familyLabel
		c.Primary = "Continue to " + familyLabel
	}
	switch {
	case len(needs) == 0 && len(builtIn) == 1:
		c.Title = "Built-in firmware: " + builtIn[0].Name
		c.Body = append(c.Body, fmt.Sprintf("The %s starts with %s %s. It comes with FES, so there is nothing to add.", familyLabel, builtIn[0].Name, builtIn[0].Version))
	case len(needs) == 0:
		c.Title = "Built-in firmware"
		c.Body = append(c.Body, "Every "+familyLabel+" model here starts with firmware that comes with FES, so there is nothing to add: "+strings.Join(builtInLabels, ", ")+".")
	case len(builtIn) == 0:
		c.Title = "The " + familyLabel + " needs your firmware"
		c.Body = append(c.Body, "FES can't include this firmware. Put your own file on a USB drive or the SD card, then choose it in "+familyLabel+" › "+firmwareSetupAction(familyLabel)+".")
	default:
		c.Title = "Built-in firmware for most models"
		c.Body = append(c.Body, "Firmware for "+strings.Join(builtInLabels, ", ")+" comes with FES. FES can't include the rest; choose your own file for them in "+familyLabel+" › "+firmwareSetupAction(familyLabel)+".")
	}
	c.Body = append(c.Body, needs...)
	seen := map[string]bool{}
	for _, fw := range builtIn {
		line := "Licence: " + fw.LicenceName + ". " + fw.Notice
		if !seen[line] {
			seen[line] = true
			c.Licence = append(c.Licence, line)
		}
		c.LicenceView = append(c.LicenceView, fw.Name+" "+fw.Version, "Licence: "+fw.LicenceName+" ("+fw.Licence+")", fw.Notice, "Firmware from: "+fw.OriginURL)
		if fw.SourceURL != "" {
			c.LicenceView = append(c.LicenceView, "Source code: "+fw.SourceURL+" (SHA-256 "+fw.SourceSHA256+")")
		}
		if text := strings.TrimSpace(fw.LicenceText); text != "" {
			c.LicenceView = append(c.LicenceView, strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")...)
		}
		c.LicenceView = append(c.LicenceView, "")
	}
	if len(builtIn) > 0 {
		c.Secondary = "Licence"
		c.Footnote = append(c.Footnote, "Prefer your own firmware? Choose it in "+familyLabel+" › "+firmwareSetupAction(familyLabel)+".")
	} else {
		c.Secondary = firmwareSetupAction(familyLabel)
	}
	if firstUse {
		c.Footnote = append(c.Footnote, "Shown once. Open it again with Firmware in the "+familyLabel+" workbench.")
	}
	return c, true
}

func bundledForModel(list []protocol.BundledFirmware, model firmwareCardModel) (protocol.BundledFirmware, bool) {
	if model.CoreID == "" || model.ROMID == "" || model.Size < 1 {
		return protocol.BundledFirmware{}, false
	}
	for _, fw := range list {
		if fw.CoreID == model.CoreID && fw.ROMID == model.ROMID && fw.Size == model.Size {
			return fw, true
		}
	}
	return protocol.BundledFirmware{}, false
}

func firmwareSetupAction(familyLabel string) string {
	if strings.TrimSpace(familyLabel) == "ZX81" {
		return "Set up ZX81"
	}
	return "Set up system"
}

// openFirmwareCardLocked handles a workbench's firmware_info request. A
// first-use request is dropped when the family's card was already seen, a
// machine is running, or the host cannot be asked; it is shown again on the
// next opening rather than recorded as seen.
func (a *App) openFirmwareCardLocked(action rooms.Action) {
	family := strings.TrimSpace(action.FamilyID)
	if family == "" || a.client == nil || a.firmwareCard.Open || a.firmwareCard.Loading {
		return
	}
	if action.FirstUse && (a.session.State == "active" || a.firmwareCardSeenLocked(family)) {
		return
	}
	label := strings.TrimSpace(action.FamilyLabel)
	if label == "" {
		label = corenames.Name(action.CoreID, "")
	}
	coreIDs := action.CoreIDs
	if len(coreIDs) == 0 {
		coreIDs = []string{action.CoreID}
	}
	models := make([]firmwareCardModel, 0, len(coreIDs))
	for _, id := range coreIDs {
		models = append(models, firmwareCardModelForCore(id, corenames.Name(id, "")))
	}
	a.firmwareCard.Gen++
	gen := a.firmwareCard.Gen
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	room := a.room
	a.firmwareCard = firmwareCardState{Loading: true, Gen: gen, Cancel: cancel, FirstUse: action.FirstUse, Family: family, FamilyLabel: label, CoreID: action.CoreID}
	go func() {
		defer cancel()
		bundled, err := a.client.BundledFirmware(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.firmwareCard.Gen != gen {
			return
		}
		a.firmwareCard.Cancel = nil
		if a.room != room {
			a.closeFirmwareCardLocked()
			return
		}
		a.firmwareCard.Loading = false
		if err != nil {
			if !action.FirstUse {
				a.status = "Firmware details are unavailable: " + err.Error()
			}
			return
		}
		content, ok := firmwareCardCopy(label, models, bundled, action.FirstUse)
		if !ok {
			if !action.FirstUse {
				a.status = label + " needs no firmware."
			}
			return
		}
		a.firmwareCard = firmwareCardState{Open: true, Gen: gen, FirstUse: action.FirstUse, Family: family, FamilyLabel: label, CoreID: action.CoreID, Content: content}
	}()
}

func (a *App) closeFirmwareCardLocked() {
	if a.firmwareCard.Cancel != nil {
		a.firmwareCard.Cancel()
	}
	a.firmwareCard = firmwareCardState{Gen: a.firmwareCard.Gen + 1}
}

func (a *App) firmwareCardSnapshotLocked() FirmwareCardSnapshot {
	if !a.firmwareCard.Open {
		return FirmwareCardSnapshot{}
	}
	return FirmwareCardSnapshot{Open: true, LicenceView: a.firmwareCard.LicenceView, Scroll: a.firmwareCard.Scroll, Content: a.firmwareCard.Content}
}

// handleFirmwareCardLocked: A continues and records the family as seen; B
// leaves the workbench without recording it, so the card shows on the next
// opening; Y opens the licence page, or system setup when the family has no
// built-in firmware.
func (a *App) handleFirmwareCardLocked(cmd Command) {
	card := &a.firmwareCard
	if card.LicenceView {
		switch cmd {
		case CmdUp:
			card.Scroll = max(0, card.Scroll-1)
		case CmdDown:
			card.Scroll = min(max(0, len(card.Content.LicenceView)-1), card.Scroll+1)
		case CmdLeft:
			card.Scroll = max(0, card.Scroll-10)
		case CmdRight:
			card.Scroll = min(max(0, len(card.Content.LicenceView)-1), card.Scroll+10)
		case CmdBack, CmdDetails, CmdSelect:
			card.LicenceView = false
			card.Scroll = 0
		}
		return
	}
	switch cmd {
	case CmdSelect:
		if card.FirstUse {
			a.markFirmwareCardSeenLocked(card.Family)
		}
		a.closeFirmwareCardLocked()
	case CmdBack:
		firstUse := card.FirstUse
		a.closeFirmwareCardLocked()
		if firstUse && a.room != nil {
			a.leaveRoomLocked()
		}
	case CmdDetails:
		switch card.Content.Secondary {
		case "Licence":
			card.LicenceView = true
			card.Scroll = 0
		default:
			if !strings.HasPrefix(card.Content.Secondary, "Set up ") {
				return
			}
			coreID, firstUse, family := card.CoreID, card.FirstUse, card.Family
			if firstUse {
				a.markFirmwareCardSeenLocked(family)
			}
			a.closeFirmwareCardLocked()
			if a.session.State != "active" {
				a.openInstalledCoreSetupLocked(coreID)
			}
		}
	}
}

func (a *App) firmwareCardPrefsPathLocked() string {
	if a.prefsPath != "" {
		return a.prefsPath
	}
	return defaultPrefsPath()
}

func (a *App) firmwareCardSeenLocked(family string) bool {
	if a.firmwareCardSeen == nil {
		a.firmwareCardSeen = map[string]bool{}
		if path := a.firmwareCardPrefsPathLocked(); path != "" {
			prefs, err := loadTenfootPrefs(path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				warnPrefsWrite(path, err)
			}
			for _, id := range prefs.FirmwareInfoSeen {
				a.firmwareCardSeen[id] = true
			}
		}
	}
	return a.firmwareCardSeen[family]
}

func (a *App) markFirmwareCardSeenLocked(family string) {
	if family == "" || a.firmwareCardSeenLocked(family) {
		return
	}
	a.firmwareCardSeen[family] = true
	a.persistPrefsLocked("firmware-info")
}

func (a *App) firmwareCardSeenListLocked() []string {
	out := make([]string, 0, len(a.firmwareCardSeen))
	for id, seen := range a.firmwareCardSeen {
		if seen {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func firmwareCardHint(kind InputKind, snap FirmwareCardSnapshot) string {
	if snap.LicenceView {
		return "D-pad scroll  ·  " + backWord(kind) + " back"
	}
	hint := selectWord(kind) + " continue"
	if snap.Content.Secondary != "" {
		hint += "  ·  " + detailsWord(kind) + " " + strings.ToLower(snap.Content.Secondary)
	}
	return hint + "  ·  " + backWord(kind) + " back"
}

const (
	firmwareCardPad     = 28
	firmwareCardTitlePx = 30
	firmwareCardBodyPx  = 22
	firmwareCardNotePx  = 18
)

// firmwareCardPlan is the card's wrapped copy for one width.
type firmwareCardPlan struct {
	title          []string
	body, licences [][]string
	notes          [][]string
}

func planFirmwareCard(c firmwareCardContent, inner int) firmwareCardPlan {
	p := firmwareCardPlan{title: gfx.WrapTextWeight(c.Title, firmwareCardTitlePx, inner, 2, gfx.WeightBold)}
	for _, text := range c.Body {
		p.body = append(p.body, gfx.WrapText(text, firmwareCardBodyPx, inner, 3))
	}
	for _, text := range c.Licence {
		p.licences = append(p.licences, gfx.WrapText(text, firmwareCardNotePx, inner-24, 4))
	}
	for _, text := range c.Footnote {
		p.notes = append(p.notes, gfx.WrapText(text, firmwareCardNotePx, inner, 2))
	}
	return p
}

// height is the card's paper height for this plan.
func (p firmwareCardPlan) height() int {
	h := 2*firmwareCardPad + gfx.TextHeightWeight(heroKickerPx, gfx.WeightBold) + 6
	h += len(p.title)*(gfx.TextHeightWeight(firmwareCardTitlePx, gfx.WeightBold)+2) + 10
	for _, lines := range p.body {
		h += len(lines)*lineStep(firmwareCardBodyPx, gfx.WeightRegular) + 8
	}
	for _, lines := range p.licences {
		h += len(lines)*lineStep(firmwareCardNotePx, gfx.WeightRegular) + 30
	}
	for _, lines := range p.notes {
		h += len(lines)*lineStep(firmwareCardNotePx, gfx.WeightRegular) + 8
	}
	return h + 12 + heroButtonH
}

// firmwareCardRect is the card's paper inside the safe content area: as tall
// as its copy, and full height for the licence page.
func firmwareCardRect(g Grid, card FirmwareCardSnapshot) rectI {
	w := min(840, g.contentWidth()-48)
	h := g.contentHeight() - 32
	if !card.LicenceView {
		h = min(h, planFirmwareCard(card.Content, w-2*firmwareCardPad).height())
	}
	return rectI{X: g.contentLeft() + (g.contentWidth()-w)/2, Y: g.contentTop() + (g.contentHeight()-h)/2, W: w, H: h}
}

// drawFirmwareCard draws the paper dialog over a dimmed screen.
func drawFirmwareCard(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	card := snap.FirmwareCard
	if !card.Open {
		return
	}
	g := snap.Grid
	r := firmwareCardRect(g, card)
	x, y, w, h := r.X, r.Y, r.W, r.H
	dimOutsidePanel(dev, g, rectI{x - 4, y - 4, w + 8, h + 8})
	dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(w + 8), H: float32(h + 8)}, heroDivider)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroSurface)
	c := card.Content
	inner := w - 2*firmwareCardPad
	cx, cy := x+firmwareCardPad, y+firmwareCardPad
	bottom := y + h - firmwareCardPad - heroButtonH - 12
	cy += heroLine(dev, labels, used, cx, cy, inner, heroKickerPx, true, heroSecondary, strings.ToUpper(c.Kicker)) + 6
	if card.LicenceView {
		cy += heroLine(dev, labels, used, cx, cy, inner, firmwareCardTitlePx, true, heroText, "Licences and sources") + 12
		var lines []string
		for _, line := range c.LicenceView[min(card.Scroll, len(c.LicenceView)):] {
			if strings.TrimSpace(line) == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, gfx.WrapText(line, firmwareCardNotePx, inner, 8)...)
		}
		step := lineStep(firmwareCardNotePx, gfx.WeightRegular)
		for i, line := range lines {
			if cy+step > y+h-firmwareCardPad-step-8 {
				break
			}
			drawFESLabel(dev, labels, used, fmt.Sprintf("fwc-lic-%d", i), cx, cy, inner, firmwareCardNotePx, line)
			cy += step
		}
		heroLine(dev, labels, used, cx, y+h-firmwareCardPad-step, inner, firmwareCardNotePx, false, heroSecondary, firmwareCardHint(snap.Affinity, card))
		return
	}
	p := planFirmwareCard(c, inner)
	for _, line := range p.title {
		cy += heroLine(dev, labels, used, cx, cy, inner, firmwareCardTitlePx, true, heroText, line) + 2
	}
	cy += 10
	paragraph := func(lines []string, px int, color gfx.Color) {
		for _, line := range lines {
			if cy+lineStep(px, gfx.WeightRegular) > bottom {
				return
			}
			heroLine(dev, labels, used, cx, cy, inner, px, false, color, line)
			cy += lineStep(px, gfx.WeightRegular)
		}
		cy += 8
	}
	for _, lines := range p.body {
		paragraph(lines, firmwareCardBodyPx, heroText)
	}
	for _, lines := range p.licences {
		boxH := len(lines)*lineStep(firmwareCardNotePx, gfx.WeightRegular) + 20
		if cy+boxH > bottom {
			break
		}
		dev.FillRect(gfx.Rect{X: float32(cx), Y: float32(cy), W: float32(inner), H: float32(boxH)}, heroRaised)
		ly := cy + 10
		for _, line := range lines {
			heroLine(dev, labels, used, cx+12, ly, inner-24, firmwareCardNotePx, false, heroText, line)
			ly += lineStep(firmwareCardNotePx, gfx.WeightRegular)
		}
		cy += boxH + 10
	}
	for _, lines := range p.notes {
		paragraph(lines, firmwareCardNotePx, heroSecondary)
	}
	bx, by := cx, y+h-firmwareCardPad-heroButtonH
	primaryW := heroButton(dev, labels, used, bx, by, heroButtonH, selectWord(snap.Affinity), c.Primary, true)
	heroOutline(dev, bx-4, by-4, primaryW+8, heroButtonH+8, 2, heroAccent)
	if c.Secondary != "" {
		heroButton(dev, labels, used, bx+primaryW+heroButtonGap, by, heroButtonH, detailsWord(snap.Affinity), c.Secondary, false)
	}
}
