package rooms

import "github.com/DeanoC/FogCast/hostclient"

// History is household play-history chrome. It is separate from focus and
// from destination availability (rooms-experience §7).
type History struct {
	Played    bool
	Completed bool
}

// ClassifyHistory maps recorded play facts onto Played and Completed.
//
// Played is true when FES recorded play activity: play_count > 0 or
// last_played_at > 0 (libraryuser.RecordPlay after a successful launch).
// Completed is true only when an explicit completion record is supplied.
// Returning from a launch, play_count, last_played_at, and presentation
// metadata `completion` never imply Completed.
func ClassifyHistory(playCount, lastPlayedAt int64, completed bool) History {
	return History{
		Played:    playCount > 0 || lastPlayedAt > 0,
		Completed: completed,
	}
}

// GameHistory is household history from a catalog row. Host games carry
// play_count and last_played_at only; there is no completion store yet, so
// Completed stays false.
func GameHistory(g hostclient.Game) History {
	return ClassifyHistory(g.PlayCount, g.LastPlayedAt, false)
}

// Line is compact panel copy. Empty when there is nothing honest to show.
// Completed never appears without an explicit record.
func (h History) Line() string {
	switch {
	case h.Played && h.Completed:
		return "Played  ·  Completed"
	case h.Completed:
		return "Completed"
	case h.Played:
		return "Played"
	default:
		return ""
	}
}

// FillHistory sets History from the selected catalog row. No selected game
// means no play-history chrome (and never Completed).
func (d *Destination) FillHistory() {
	if d == nil {
		return
	}
	if g, ok := d.Game(); ok {
		d.History = GameHistory(g)
		return
	}
	d.History = History{}
}
