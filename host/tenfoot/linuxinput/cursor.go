package linuxinput

// Cursor is the spike highlight. Apply consumes mapped input; Tick repeats
// a held d-pad or stick direction.
type Cursor struct {
	X, Y, W, H int
	Size       int
	Step       int
	Last       string
	Quit       bool
	holdX      int
	holdY      int
	analogX    bool
	analogY    bool
}

// NewCursor places a Size×Size highlight in the centre of w×h.
func NewCursor(w, h, size, step int) Cursor {
	if size < 1 {
		size = 1
	}
	if step < 1 {
		step = 1
	}
	if w < size {
		w = size
	}
	if h < size {
		h = size
	}
	return Cursor{
		X:    (w - size) / 2,
		Y:    (h - size) / 2,
		W:    w,
		H:    h,
		Size: size,
		Step: step,
	}
}

// Apply updates hold state and steps once on a rising edge. Extra analog
// samples and key repeats latch hold for Tick instead of jumping.
func (c *Cursor) Apply(m Mapped) {
	if m.Action == ActionNone {
		return
	}
	c.Last = m.String()
	if m.Action == ActionQuit {
		if m.Active {
			c.Quit = true
		}
		return
	}
	if m.Action == ActionConfirm {
		return
	}
	switch m.Action {
	case ActionLeft, ActionRight:
		dir := 1
		if m.Action == ActionLeft {
			dir = -1
		}
		c.applyAxis(&c.holdX, &c.analogX, dir, m, true)
	case ActionUp, ActionDown:
		dir := 1
		if m.Action == ActionUp {
			dir = -1
		}
		c.applyAxis(&c.holdY, &c.analogY, dir, m, false)
	}
}

func (c *Cursor) applyAxis(hold *int, analog *bool, dir int, m Mapped, horizontal bool) {
	if m.Active {
		rising := *hold != dir
		*hold = dir
		*analog = m.Analog
		if rising && !m.Repeat {
			if horizontal {
				c.step(dir, 0)
			} else {
				c.step(0, dir)
			}
		}
		return
	}
	if m.Analog {
		if *analog {
			*hold = 0
			*analog = false
		}
		return
	}
	if *hold == dir {
		*hold = 0
		*analog = false
	}
}

// Tick repeats motion while a direction is held.
func (c *Cursor) Tick() {
	if c.holdX == 0 && c.holdY == 0 {
		return
	}
	c.step(c.holdX, c.holdY)
}

func (c *Cursor) step(dx, dy int) {
	c.X += dx * c.Step
	c.Y += dy * c.Step
	maxX := c.W - c.Size
	maxY := c.H - c.Size
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	if c.X < 0 {
		c.X = 0
	}
	if c.Y < 0 {
		c.Y = 0
	}
	if c.X > maxX {
		c.X = maxX
	}
	if c.Y > maxY {
		c.Y = maxY
	}
}

// SampleX is the cursor centre X for framebuffer sampling.
func (c Cursor) SampleX() int { return c.X + c.Size/2 }

// SampleY is the cursor centre Y for framebuffer sampling.
func (c Cursor) SampleY() int { return c.Y + c.Size/2 }
