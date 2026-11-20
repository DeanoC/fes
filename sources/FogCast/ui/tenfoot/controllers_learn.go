package tenfoot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// learnStep is one prompt of step-by-step mapping: a GameControllerDB
// target and the name FES gives it.
type learnStep struct {
	target  string
	control gfx.PromptControl
	label   string
	// note says what FES menus use the control for.
	note string
	// optional steps can be skipped with the button just mapped to B.
	optional bool
}

// learnSteps are asked in order. The directions, A and B come first and
// cannot be skipped: FES menus need them, and once B is known the player
// can skip the rest with it.
var learnSteps = []learnStep{
	{"dpup", gfx.PromptDPad, "Up", "", false},
	{"dpdown", gfx.PromptDPad, "Down", "", false},
	{"dpleft", gfx.PromptDPad, "Left", "", false},
	{"dpright", gfx.PromptDPad, "Right", "", false},
	{"a", gfx.PromptSouth, "A", "Confirm", false},
	{"b", gfx.PromptEast, "B", "Back", false},
	{"x", gfx.PromptWest, "X", "", true},
	{"y", gfx.PromptNorth, "Y", "", true},
	{"leftshoulder", gfx.PromptL, "L", "Left shoulder", true},
	{"rightshoulder", gfx.PromptR, "R", "Right shoulder", true},
	{"back", gfx.PromptSelect, "Select", "", true},
	{"start", gfx.PromptStart, "Start", "", true},
}

// learnIdle is how long mapping waits for a press before it stops and
// changes nothing. It is the pad-only way out before B is known.
const learnIdle = 20 * time.Second

// Review choices, left to right.
const (
	learnSave = iota
	learnAgain
	learnCancel
	learnChoices
)

// padLearn is step-by-step mapping of the shown pad.
type padLearn struct {
	active bool
	step   int
	// sources is each mapped target's raw control, as controller.Learner
	// names it.
	sources  map[string]string
	review   bool
	choice   int
	deadline time.Time
	note     string
}

// LearnRow is one step as the mapping screen lists it.
type LearnRow struct {
	Label, Note string
	// Source is the raw control mapped to it, "" before it is asked and
	// "Skipped" for one the player skipped.
	Source string
}

// LearnSnapshot is step-by-step mapping as the renderer draws it.
type LearnSnapshot struct {
	Open bool
	// Step is the step being asked, from 0, while Review is false.
	Step     int
	Label    string
	Note     string
	Control  gfx.PromptControl
	Optional bool
	// Skip is the raw control that skips an optional step.
	Skip   string
	Review bool
	Choice int
	Rows   []LearnRow
	// Message says why the last press was not taken.
	Message string
	// Seconds is how long is left before mapping stops by itself.
	Seconds int
}

// PadLearning reports whether the input layer should read pad id's raw
// controls for step-by-step mapping instead of its mapping.
func (a *App) PadLearning(id int) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.controllers
	return c.open && c.learn.active && c.pad == id
}

// LearnPadSource takes a raw control the pad being mapped has just pressed.
func (a *App) LearnPadSource(id int, source string, now time.Time) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := &a.controllers
	if !c.open || !c.learn.active || c.pad != id {
		return
	}
	l := &c.learn
	l.deadline = now.Add(learnIdle)
	l.note = ""
	if l.review {
		// The review is driven by the buttons just mapped.
		switch l.targetOf(source) {
		case "dpleft", "dpup":
			l.choice = max(0, l.choice-1)
		case "dpright", "dpdown":
			l.choice = min(learnChoices-1, l.choice+1)
		case "a":
			a.chooseLearnLocked(now)
		case "b":
			a.cancelLearnLocked("Mapping cancelled. Nothing changed.")
		}
		return
	}
	step := learnSteps[l.step]
	if step.optional && source == l.sources["b"] {
		l.advance()
		return
	}
	if used := l.targetOf(source); used != "" {
		l.note = fmt.Sprintf("That control is already %s. Press another.", learnLabel(used))
		return
	}
	l.sources[step.target] = source
	l.advance()
}

func (l *padLearn) targetOf(source string) string {
	for target, s := range l.sources {
		if s == source {
			return target
		}
	}
	return ""
}

func (l *padLearn) advance() {
	l.step++
	if l.step >= len(learnSteps) {
		l.review, l.choice = true, learnSave
	}
}

func learnLabel(target string) string {
	for _, s := range learnSteps {
		if s.target == target {
			return s.label
		}
	}
	return target
}

func (a *App) startLearnLocked(now time.Time) {
	a.controllers.learn = padLearn{active: true, sources: map[string]string{}, deadline: now.Add(learnIdle)}
	a.controllers.status = ""
}

func (a *App) cancelLearnLocked(status string) {
	a.controllers.learn = padLearn{}
	a.controllers.status = status
}

// handleLearnLocked takes a command from another pad or the keyboard while
// the shown pad is being mapped. Back cancels; X skips an optional step;
// the review takes moves and Confirm.
func (a *App) handleLearnLocked(cmd Command, now time.Time) {
	l := &a.controllers.learn
	switch cmd {
	case CmdBack:
		a.cancelLearnLocked("Mapping cancelled. Nothing changed.")
		return
	case CmdSettings:
		a.closeSettingsLocked()
		return
	}
	l.deadline = now.Add(learnIdle)
	if !l.review {
		if cmd == CmdSortCycle && learnSteps[l.step].optional {
			l.note = ""
			l.advance()
		}
		return
	}
	switch cmd {
	case CmdLeft, CmdUp:
		l.choice = max(0, l.choice-1)
	case CmdRight, CmdDown:
		l.choice = min(learnChoices-1, l.choice+1)
	case CmdSelect:
		a.chooseLearnLocked(now)
	}
}

func (a *App) chooseLearnLocked(now time.Time) {
	l := &a.controllers.learn
	switch l.choice {
	case learnAgain:
		a.startLearnLocked(now)
	case learnCancel:
		a.cancelLearnLocked("Mapping cancelled. Nothing changed.")
	case learnSave:
		mapping := l.mapping()
		info := a.pads[a.controllers.pad]
		profile := a.padProfilesLocked()[info.GUID]
		profile.Mapping = mapping
		a.controllers.learn = padLearn{}
		if a.savePadProfileLocked(info.GUID, profile) {
			a.controllers.status = "Saved. This pad now reads the buttons you pressed."
		}
	}
}

// mapping is the learned layout in GameControllerDB's binding form, in
// step order.
func (l *padLearn) mapping() string {
	var fields []string
	for _, s := range learnSteps {
		if source, ok := l.sources[s.target]; ok {
			fields = append(fields, s.target+":"+source)
		}
	}
	return strings.Join(fields, ",")
}

// tickLearnLocked stops mapping that has had no press for learnIdle.
func (a *App) tickLearnLocked(now time.Time) {
	l := a.controllers.learn
	if a.controllers.open && l.active && now.After(l.deadline) {
		a.cancelLearnLocked(fmt.Sprintf("Mapping stopped: nothing was pressed for %d seconds. Nothing changed.", int(learnIdle/time.Second)))
	}
}

func (a *App) learnSnapshotLocked(now time.Time) LearnSnapshot {
	l := a.controllers.learn
	if !l.active {
		return LearnSnapshot{}
	}
	snap := LearnSnapshot{Open: true, Step: l.step, Review: l.review, Choice: l.choice, Message: l.note,
		Skip: describeSource(l.sources["b"])}
	if left := l.deadline.Sub(now); left > 0 {
		snap.Seconds = int((left + time.Second - 1) / time.Second)
	}
	if !l.review {
		step := learnSteps[l.step]
		snap.Label, snap.Note, snap.Control, snap.Optional = step.label, step.note, step.control, step.optional
	}
	for i, s := range learnSteps {
		row := LearnRow{Label: s.label, Note: s.note}
		if source, ok := l.sources[s.target]; ok {
			row.Source = describeSource(source)
		} else if l.review || i < l.step {
			row.Source = "Skipped"
		}
		snap.Rows = append(snap.Rows, row)
	}
	return snap
}

// describeSource names a raw control for the player: keys and axes are
// counted from 1, as most pad testers count them.
func describeSource(source string) string {
	if source == "" {
		return ""
	}
	number := func(s string) string {
		n, err := strconv.Atoi(s)
		if err != nil {
			return s
		}
		return strconv.Itoa(n + 1)
	}
	switch {
	case strings.HasPrefix(source, "b"):
		return "Button " + number(source[1:])
	case strings.HasPrefix(source, "h"):
		hat, mask, _ := strings.Cut(source[1:], ".")
		dir := map[string]string{"1": "up", "2": "right", "4": "down", "8": "left"}[mask]
		if hat == "0" {
			return "Hat " + dir
		}
		return fmt.Sprintf("Hat %s %s", number(hat), dir)
	case strings.HasPrefix(source, "+a"):
		return "Axis " + number(source[2:]) + " +"
	case strings.HasPrefix(source, "-a"):
		return "Axis " + number(source[2:]) + " −"
	case strings.HasSuffix(source, "~"):
		return "Axis " + number(strings.TrimSuffix(source[1:], "~")) + " (reversed)"
	case strings.HasPrefix(source, "a"):
		return "Axis " + number(source[1:])
	}
	return source
}
