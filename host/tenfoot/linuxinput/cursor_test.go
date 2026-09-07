package linuxinput

import "testing"

func TestCursorMoveAndQuit(t *testing.T) {
	t.Parallel()
	c := NewCursor(640, 480, 24, 16)
	startX, startY := c.X, c.Y
	c.Apply(Mapped{Action: ActionRight, Active: true, Source: "test"})
	if c.X != startX+16 || c.Y != startY {
		t.Fatalf("right %d,%d from %d,%d", c.X, c.Y, startX, startY)
	}
	c.Tick()
	if c.X != startX+32 {
		t.Fatalf("held tick %d", c.X)
	}
	c.Apply(Mapped{Action: ActionRight, Active: false})
	held := c.X
	c.Tick()
	if c.X != held {
		t.Fatalf("released still moved %d -> %d", held, c.X)
	}
	c.Apply(Mapped{Action: ActionUp, Active: true})
	if c.Y >= startY {
		t.Fatalf("up y %d", c.Y)
	}
	c.Apply(Mapped{Action: ActionQuit, Active: true, Source: "js0"})
	if !c.Quit {
		t.Fatal("quit")
	}
}

func TestCursorClamps(t *testing.T) {
	t.Parallel()
	c := NewCursor(40, 40, 24, 16)
	for i := 0; i < 20; i++ {
		c.Apply(Mapped{Action: ActionRight, Active: true})
	}
	if c.X != 16 {
		t.Fatalf("clamp x %d", c.X)
	}
	if c.SampleX() != c.X+12 || c.SampleY() != c.Y+12 {
		t.Fatalf("sample %d,%d", c.SampleX(), c.SampleY())
	}
}

func TestCursorAnalogDoesNotOverstep(t *testing.T) {
	t.Parallel()
	c := NewCursor(640, 480, 24, 16)
	start := c.X
	right := Mapped{Action: ActionRight, Active: true, Analog: true, Source: "abs"}
	c.Apply(right)
	c.Apply(right)
	c.Apply(right)
	if c.X != start+16 {
		t.Fatalf("analog stream stepped %d -> %d", start, c.X)
	}
	c.Tick()
	if c.X != start+32 {
		t.Fatalf("tick %d", c.X)
	}
	c.Apply(Mapped{Action: ActionRight, Active: true, Repeat: true})
	if c.X != start+32 {
		t.Fatalf("repeat stepped %d", c.X)
	}
}

func TestCursorAnalogIdleDoesNotClearDpad(t *testing.T) {
	t.Parallel()
	c := NewCursor(640, 480, 24, 16)
	c.Apply(Mapped{Action: ActionRight, Active: true, Source: "dpad"})
	x := c.X
	c.Apply(Mapped{Action: ActionLeft, Active: false, Analog: true, Source: "stick"})
	c.Tick()
	if c.X != x+16 {
		t.Fatalf("analog idle cancelled d-pad hold %d -> %d", x, c.X)
	}
	c.Apply(Mapped{Action: ActionLeft, Active: false, Source: "dpad"})
	held := c.X
	c.Tick()
	if c.X != held+16 {
		t.Fatalf("unrelated left release cleared right hold %d -> %d", held, c.X)
	}
	c.Apply(Mapped{Action: ActionRight, Active: false, Source: "dpad"})
	held = c.X
	c.Tick()
	if c.X != held {
		t.Fatalf("right release still moving %d -> %d", held, c.X)
	}
}

func TestCursorIgnoresNone(t *testing.T) {
	t.Parallel()
	c := NewCursor(100, 100, 8, 8)
	x, y := c.X, c.Y
	c.Apply(Mapped{})
	if c.X != x || c.Y != y || c.Quit {
		t.Fatal("none mutated")
	}
}
