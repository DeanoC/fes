package rooms

import (
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/libraryuser"
)

// Availability is one of the five destination states in rooms-experience §4.
type Availability string

const (
	AvailChecking    Availability = "checking"
	AvailMissing     Availability = "missing"
	AvailNeedsChoice Availability = "needs_choice"
	AvailUnavailable Availability = "unavailable"
	AvailReady       Availability = "ready"
)

// Kind is the selected location the compact info panel identifies.
type Kind string

const (
	KindGame       Kind = "game"
	KindRoom       Kind = "room"
	KindLibrary    Kind = "library"
	KindUnresolved Kind = "unresolved"
)

// ConfirmIntent is what Confirm must do so it never silently no-ops.
type ConfirmIntent int

const (
	ConfirmNone ConfirmIntent = iota
	ConfirmWait
	ConfirmOpenLibrary
	ConfirmChoose
	ConfirmExplain
	ConfirmLaunch
	ConfirmEnterRoom
	ConfirmOpenLibraryBrowse
)

// Destination is the selected location a room publishes to the launcher.
type Destination struct {
	Kind         Kind
	Label        string
	System       string
	GameID       string
	RoomID       string
	Availability Availability
	Status       string
	Action       string
	Note         string
	NoteBy       string
	Query        string
	Platform     string
	Matches      []hostclient.Game
	History      History
}

// ClassifyGames maps a library result set onto Missing, Needs a choice,
// Unavailable, or Ready. The caller reports Checking while the query is
// still in flight. query, when set, filters titles; an exact title match
// wins over looser contains-matches.
func ClassifyGames(games []hostclient.Game, query string) (Availability, []hostclient.Game) {
	matches := matchingGames(games, query)
	if len(matches) == 0 {
		return AvailMissing, nil
	}
	if exact := exactTitleMatches(matches, query); len(exact) > 0 {
		matches = exact
	}
	if len(matches) > 1 {
		return AvailNeedsChoice, matches
	}
	if !matches[0].LaunchEligible() {
		return AvailUnavailable, matches
	}
	return AvailReady, matches
}

func matchingGames(games []hostclient.Game, query string) []hostclient.Game {
	q := normalizeTitle(query)
	out := make([]hostclient.Game, 0, len(games))
	for _, g := range games {
		if q == "" || strings.Contains(normalizeTitle(g.Title), q) {
			out = append(out, g)
		}
	}
	return out
}

func exactTitleMatches(games []hostclient.Game, query string) []hostclient.Game {
	q := normalizeTitle(query)
	if q == "" {
		return nil
	}
	var out []hostclient.Game
	for _, g := range games {
		if normalizeTitle(g.Title) == q {
			out = append(out, g)
		}
	}
	return out
}

func normalizeTitle(s string) string {
	return libraryuser.CanonicalEditionQuery(s)
}

// DestinationPreferenceKey is the household edition-preference key for a
// published location (query, else label, plus platform).
func DestinationPreferenceKey(d Destination) string {
	q := strings.TrimSpace(d.Query)
	if q == "" {
		q = strings.TrimSpace(d.Label)
	}
	return libraryuser.EditionKey(q, d.Platform)
}

// ApplyEditionPreference collapses Needs a choice onto the saved edition when
// that game is still in the current match set. Unknown or stale IDs leave the
// destination unchanged so Confirm still forces a choice.
func ApplyEditionPreference(d Destination, gameID string) Destination {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" || d.Availability != AvailNeedsChoice {
		return d
	}
	for _, g := range d.Matches {
		if g.ID != gameID {
			continue
		}
		d.GameID = g.ID
		d.Matches = []hostclient.Game{g}
		d.Label = g.Title
		d.System = g.System
		if g.LaunchEligible() {
			d.Availability = AvailReady
		} else {
			d.Availability = AvailUnavailable
		}
		d.FillCopy()
		d.FillHistory()
		return d
	}
	return d
}

// FillCopy sets distinct Status and Action strings for the compact panel.
func (d *Destination) FillCopy() {
	if d == nil {
		return
	}
	switch d.Kind {
	case KindRoom:
		d.Status = "Enter room."
		d.Action = "Enter room."
		return
	case KindLibrary:
		d.Status = "Browse the full library."
		d.Action = "Open library."
		return
	case KindUnresolved:
		if d.Availability == "" {
			d.Status = "Choose a title from this location."
			d.Action = "Open the title list."
			return
		}
	}
	switch d.Availability {
	case AvailChecking:
		d.Status = "Matching this title in your library…"
		d.Action = "Wait — still checking."
	case AvailMissing:
		label := strings.TrimSpace(d.Query)
		if label == "" {
			label = strings.TrimSpace(d.Label)
		}
		if label != "" {
			d.Status = "Not in this household's library (" + label + ")."
		} else {
			d.Status = "Not in this household's library."
		}
		d.Action = "Open the library to add it."
	case AvailNeedsChoice:
		d.Status = "Several editions match. Choose one."
		d.Action = "Choose an edition."
	case AvailUnavailable:
		d.Status = "This title cannot play on the current setup."
		d.Action = "See why this title cannot play."
		if len(d.Matches) > 0 {
			if reason := LaunchBlockCopy(d.Matches[0]); reason != "" {
				d.Status = reason
			}
			if d.Matches[0].LaunchBlock() == hostclient.LaunchMissingFirmware {
				d.Action = "Import Coleco BIOS."
			}
		}
	case AvailReady:
		d.Status = "Ready to play."
		d.Action = "Play"
	default:
		d.Status = "Matching this location…"
		d.Action = "Wait — still checking."
	}
}

// Confirm reports the required Confirm behaviour for this destination.
func (d Destination) Confirm() ConfirmIntent {
	switch d.Kind {
	case KindRoom:
		return ConfirmEnterRoom
	case KindLibrary:
		return ConfirmOpenLibraryBrowse
	}
	switch d.Availability {
	case AvailChecking:
		return ConfirmWait
	case AvailMissing:
		return ConfirmOpenLibrary
	case AvailNeedsChoice:
		return ConfirmChoose
	case AvailUnavailable:
		return ConfirmExplain
	case AvailReady:
		return ConfirmLaunch
	default:
		if d.Kind == KindUnresolved && d.Availability != "" {
			return ConfirmWait
		}
		return ConfirmNone
	}
}

// LaunchBlockCopy is sofa copy for a catalog-side launch block.
func LaunchBlockCopy(game hostclient.Game) string {
	switch game.LaunchBlock() {
	case hostclient.LaunchBrowseOnly:
		return "This platform is browse-only on this host."
	case hostclient.LaunchSourceOffline:
		return "This game's source is offline."
	case hostclient.LaunchUnreadable:
		return "This ROM can't be read."
	case hostclient.LaunchNotReady:
		return "This game isn't ready to launch."
	case hostclient.LaunchMissingFirmware:
		return "Coleco BIOS required. Import household firmware before Play."
	case "":
		return ""
	default:
		return "This game isn't ready to launch."
	}
}

// CuratorAttribution is the Details-panel credit for a room-authored note.
func (d Destination) CuratorAttribution() string {
	note := strings.TrimSpace(d.Note)
	if note == "" {
		return ""
	}
	by := strings.TrimSpace(d.NoteBy)
	if by == "" {
		by = "this room"
	}
	return "Note from " + by
}

// Game returns the single matched catalog row when one is selected.
func (d Destination) Game() (hostclient.Game, bool) {
	if id := strings.TrimSpace(d.GameID); id != "" {
		for _, g := range d.Matches {
			if g.ID == id {
				return g, true
			}
		}
	}
	if len(d.Matches) == 1 {
		return d.Matches[0], true
	}
	return hostclient.Game{}, false
}

// Set reports whether the room has published a selected location.
func (d Destination) Set() bool {
	return d.Kind != "" || d.Availability != "" || strings.TrimSpace(d.Label) != "" || strings.TrimSpace(d.GameID) != "" || strings.TrimSpace(d.RoomID) != ""
}
