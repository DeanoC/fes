package tenfoot

import (
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// libraryBlockTag is the short name of what stops a title playing, for a
// cell tag and the Details headline. The full sentence stays
// rooms.LaunchBlockCopy.
func libraryBlockTag(block hostclient.LaunchBlock) string {
	switch block {
	case "":
		return ""
	case hostclient.LaunchBrowseOnly:
		return "Browse only"
	case hostclient.LaunchSourceOffline:
		return "Offline"
	case hostclient.LaunchUnreadable:
		return "Can't be read"
	case hostclient.LaunchMissingFirmware:
		return "Needs BIOS"
	case hostclient.LaunchMissingROM:
		return "Needs a cartridge"
	case hostclient.LaunchMissingExpansion:
		return "Needs an expansion"
	case hostclient.LaunchLeaseHeld:
		return rooms.InUseStatus
	case hostclient.LaunchDistant:
		return "Not on this machine"
	case hostclient.LaunchEnsureProgress:
		return heroStateChecking
	default:
		return "Can't play here"
	}
}

func libraryGameBlockTag(game hostclient.Game) string {
	if game.LaunchBlock() == hostclient.LaunchMissingFirmware && game.ROMRole == protocol.FirmwareRole {
		return "Needs firmware"
	}
	return libraryBlockTag(game.LaunchBlock())
}

// readinessCopy is the readiness row of Details: the state name, a short
// headline and the sentence that explains it or names the remedy.
type readinessCopy struct {
	State    string
	Headline string
	Explain  string
}

func detailReadiness(d rooms.Destination) readinessCopy {
	if d.Status == colecoRunningDestinationStatus {
		return readinessCopy{State: heroStateUnavailable, Headline: d.Status, Explain: d.Action}
	}
	c := readinessCopy{State: heroState(d.Availability)}
	switch d.Kind {
	case rooms.KindCore:
		if d.Confirm() == rooms.ConfirmLaunchCore {
			return readinessCopy{State: heroStateReady, Headline: heroPlaysOnThisTV}
		}
		if d.Confirm() == rooms.ConfirmWait {
			return readinessCopy{State: heroStateChecking, Headline: strings.TrimSpace(d.Status)}
		}
		c.State = heroStateUnavailable
		c.Headline, c.Explain = heroCoreBlocked(d)
		return c
	case rooms.KindRoom, rooms.KindLibrary, rooms.KindAction, rooms.KindUnresolved:
		if d.Availability != rooms.AvailChecking {
			return readinessCopy{}
		}
	}
	switch c.State {
	case heroStateReady:
		// Only a paired kit's own launch has a known picture: the
		// classifier says so in Status. Elsewhere the pill stands alone.
		if d.Status == heroPlaysOnThisTV {
			c.Headline = heroPlaysOnThisTV
		}
	case heroStateChecking:
		c.Headline = strings.TrimSpace(d.Status)
	case heroStateNeedsChoice:
		c.Headline = strings.TrimSpace(d.Status)
		c.Explain = strings.TrimSpace(d.Action)
	case heroStateUnavailable:
		switch {
		case d.LeaseHeld || d.Status == rooms.InUseStatus:
			c.Headline, c.Explain = rooms.InUseStatus, rooms.InUseDetail
		case d.Status == machineStatusUnknown:
			// The title itself can play. The headline names the unknown kit
			// status; the remedy is the same sentence Confirm shows.
			c.Headline = machineStatusUnknown
			c.Explain = strings.TrimSpace(d.Action)
			if c.Explain == "" || c.Explain == c.Headline {
				c.Explain = "Wait for the kit to answer before starting anything."
			}
		case d.Availability == rooms.AvailMissing:
			c.Headline, c.Explain = "Not in the library", strings.TrimSpace(d.Status)
		default:
			block := hostclient.LaunchBlock("")
			if len(d.Matches) > 0 {
				block = d.Matches[0].LaunchBlock()
			}
			if len(d.Matches) > 0 {
				c.Headline = libraryGameBlockTag(d.Matches[0])
			} else {
				c.Headline = libraryBlockTag(block)
			}
			if c.Headline == "" {
				c.Headline = "Can't play here"
			}
			c.Explain = strings.TrimSpace(d.Status)
		}
	}
	return c
}

// drawDetailReadiness draws the readiness row on the Library Details pane and
// returns the height it used. maxY keeps it inside a collapsed pane.
func drawDetailReadiness(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y, w, maxY int, d rooms.Destination) int {
	c := detailReadiness(d)
	if c.State == "" && c.Headline == "" {
		return 0
	}
	text := heroText
	secondary := heroSecondary
	rowH := gfx.TextHeight(20)
	lineX := x
	if c.State != "" {
		if y+gfx.TextHeightWeight(detailPill.px, gfx.WeightBold)+detailPill.px/2 > maxY {
			return 0
		}
		pw, h := statePill(dev, labels, used, x, y, c.State, detailPill)
		lineX += pw + 12
		rowH = max(rowH, h)
	}
	heroLine(dev, labels, used, lineX, y+(rowH-gfx.TextHeight(20))/2, x+w-lineX, 20, false, text, c.Headline)
	height := rowH + 6
	if c.Explain != "" && y+height+gfx.TextHeight(20) <= maxY {
		height += heroLine(dev, labels, used, x, y+height, w, 20, false, secondary, c.Explain) + 4
	}
	return height
}

// libraryCellTag is the cover-cell plate. It uses classifyLibraryGame, the
// same classification as the header, list rows and Details. A catalog
// reason such as Needs BIOS stays that short name. A playable title the
// classifier refuses because the kit status is unknown, or another session
// holds the lease, uses the Details headline.
func libraryCellTag(game hostclient.Game, k kitPlayState) string {
	d := classifyLibraryGame(game, k)
	if tag := libraryGameBlockTag(game); tag != "" {
		return tag
	}
	switch d.Availability {
	case rooms.AvailUnavailable, rooms.AvailMissing:
		return strings.TrimSpace(detailReadiness(d).Headline)
	default:
		return ""
	}
}

// cellBlockTag labels a library cell whose title cannot play: text on a
// solid plate with a leading bar, so it does not rely on colour.
func drawCellBlockTag(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, x, y, maxW int, game hostclient.Game, k kitPlayState) {
	tag := libraryCellTag(game, k)
	if tag == "" {
		return
	}
	px := 14
	w := min(maxW, gfx.MeasureTextWeight(tag, px, gfx.WeightBold)+22)
	h := gfx.TextHeightWeight(px, gfx.WeightBold) + 8
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y - h), W: float32(w), H: float32(h)}, detailPill.unavailable)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y - h), W: 5, H: float32(h)}, detailPill.bar)
	heroLine(dev, labels, used, x+11, y-h+4, w-14, px, true, detailPill.text, tag)
}

// placeholderSystem is the missing-art label: the platform as the catalog
// names it, else its id.
func placeholderSystem(snap Snapshot, game hostclient.Game) string {
	id := strings.TrimSpace(game.System)
	for _, p := range snap.Platforms {
		if p.ID == id && strings.TrimSpace(p.Label) != "" {
			return strings.TrimSpace(p.Label)
		}
	}
	if s := strings.ToUpper(id); s != "" {
		return s
	}
	return strings.TrimSpace(game.Title)
}

// kitPlayState is what the launcher knows about the kit for a Library
// launch: a paired client, whether that kit's status is unknown, and
// whether another session holds it.
type kitPlayState struct {
	Paired  bool
	Unknown bool
	Foreign bool
}

// classifyLibraryGame classifies one title as startLaunchGameLocked treats
// it: a paired kit whose status is unknown, or that another session holds,
// refuses before the catalog block or host-only exemption is considered.
// The header, list rows, cover cells, Details and Confirm all use it.
//
// A paired client launches on its own target, so a Ready title that is not
// host-only plays on this kit's HDMI output; Status then says so. A
// host-only title plays on the host and makes no claim about the picture.
func classifyLibraryGame(game hostclient.Game, k kitPlayState) rooms.Destination {
	d := libraryPlayDestination(game)
	if d.Availability != rooms.AvailReady || missingFirmwareGame(game) {
		return d
	}
	switch {
	case k.Unknown:
		d.Availability = rooms.AvailUnavailable
		d.Status = machineStatusUnknown
		d.Action = "Wait for the kit to answer before starting anything."
	case k.Foreign && (k.Paired || !game.HostOnly()):
		d.Availability = rooms.AvailUnavailable
		d.LeaseHeld = true
		d.Status = rooms.InUseStatus
		d.Action = rooms.InUseDetail
	case k.Paired && !game.HostOnly():
		d.Status = heroPlaysOnThisTV
	}
	return d
}
