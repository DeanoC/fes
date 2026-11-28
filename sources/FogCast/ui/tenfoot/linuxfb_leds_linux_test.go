//go:build linux

package tenfoot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupSysfsPadLEDs(t *testing.T, sysClassInput, eventBase string, ledNames ...string) string {
	t.Helper()
	devDir := filepath.Join(t.TempDir(), "usb-pad")
	ledsDir := filepath.Join(devDir, "leds")
	for _, ledName := range ledNames {
		dir := filepath.Join(ledsDir, ledName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "brightness"), []byte("0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(ledName, ":rgb:indicator") {
			if err := os.WriteFile(filepath.Join(dir, "max_brightness"), []byte("255\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "multi_intensity"), []byte("0 0 0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	inputDevDir := filepath.Join(sysClassInput, eventBase, "device")
	if err := os.MkdirAll(inputDevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(devDir, filepath.Join(inputDevDir, "device")); err != nil {
		t.Fatal(err)
	}
	return ledsDir
}

func readSysfsTrim(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.TrimSpace(string(raw))
}

func TestSyncEvdevPlayerLEDs(t *testing.T) {
	t.Run("dualsense 5-led and rgb indicator", func(t *testing.T) {
		sysRoot := t.TempDir()
		ledsDir := setupSysfsPadLEDs(t, sysRoot, "event1",
			"input1:rgb:indicator",
			"input1:white:player-1",
			"input1:white:player-2",
			"input1:white:player-3",
			"input1:white:player-4",
			"input1:white:player-5",
		)

		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event1", 1)
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:white:player-3", "brightness")); got != "1" {
			t.Fatalf("P1 player-3 = %q, want 1", got)
		}
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:white:player-1", "brightness")); got != "0" {
			t.Fatalf("P1 player-1 = %q, want 0", got)
		}
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:rgb:indicator", "multi_intensity")); got != "0 32 128" {
			t.Fatalf("P1 rgb = %q, want 0 32 128", got)
		}
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:rgb:indicator", "brightness")); got != "255" {
			t.Fatalf("P1 rgb brightness = %q, want 255", got)
		}

		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event1", 2)
		for _, tc := range []struct {
			slot string
			want string
		}{
			{"input1:white:player-1", "0"},
			{"input1:white:player-2", "1"},
			{"input1:white:player-3", "0"},
			{"input1:white:player-4", "1"},
			{"input1:white:player-5", "0"},
		} {
			if got := readSysfsTrim(t, filepath.Join(ledsDir, tc.slot, "brightness")); got != tc.want {
				t.Fatalf("P2 %s = %q, want %q", tc.slot, got, tc.want)
			}
		}
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:rgb:indicator", "multi_intensity")); got != "128 16 16" {
			t.Fatalf("P2 rgb = %q, want 128 16 16", got)
		}

		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event1", 0)
		for _, slot := range []string{"player-1", "player-2", "player-3", "player-4", "player-5"} {
			if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:white:"+slot, "brightness")); got != "0" {
				t.Fatalf("unassigned %s = %q, want 0", slot, got)
			}
		}
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input1:rgb:indicator", "brightness")); got != "0" {
			t.Fatalf("unassigned rgb brightness = %q, want 0", got)
		}
	})

	t.Run("switch pro 4-led", func(t *testing.T) {
		sysRoot := t.TempDir()
		ledsDir := setupSysfsPadLEDs(t, sysRoot, "event2",
			"input2:green:player-1",
			"input2:green:player-2",
			"input2:green:player-3",
			"input2:green:player-4",
		)

		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event2", 2)
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input2:green:player-1", "brightness")); got != "0" {
			t.Fatalf("P2 player-1 = %q, want 0", got)
		}
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input2:green:player-2", "brightness")); got != "1" {
			t.Fatalf("P2 player-2 = %q, want 1", got)
		}
		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event2", 0)
		if got := readSysfsTrim(t, filepath.Join(ledsDir, "input2:green:player-2", "brightness")); got != "0" {
			t.Fatalf("unassigned player-2 = %q, want 0", got)
		}
	})

	t.Run("xpad and discrete rgb", func(t *testing.T) {
		sysRoot := t.TempDir()
		xpadDir := setupSysfsPadLEDs(t, sysRoot, "event3", "xpad0")
		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event3", 1)
		if got := readSysfsTrim(t, filepath.Join(xpadDir, "xpad0", "brightness")); got != "2" {
			t.Fatalf("xpad P1 brightness = %q, want 2", got)
		}
		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event3", 4)
		if got := readSysfsTrim(t, filepath.Join(xpadDir, "xpad0", "brightness")); got != "5" {
			t.Fatalf("xpad P4 brightness = %q, want 5", got)
		}
		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event3", 0)
		if got := readSysfsTrim(t, filepath.Join(xpadDir, "xpad0", "brightness")); got != "0" {
			t.Fatalf("xpad unassigned brightness = %q, want 0", got)
		}

		ds4Dir := setupSysfsPadLEDs(t, sysRoot, "event4", "0003:054C:09CC.0001:red", "0003:054C:09CC.0001:green", "0003:054C:09CC.0001:blue")
		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event4", 3)
		if got := readSysfsTrim(t, filepath.Join(ds4Dir, "0003:054C:09CC.0001:green", "brightness")); got != "112" {
			t.Fatalf("DS4 P3 green = %q, want 112", got)
		}
		syncEvdevPlayerLEDs(sysRoot, "/dev/input/event4", 0)
		if got := readSysfsTrim(t, filepath.Join(ds4Dir, "0003:054C:09CC.0001:green", "brightness")); got != "0" {
			t.Fatalf("DS4 unassigned green = %q, want 0", got)
		}
	})
}

func TestNativeInputsSyncsPlayerLEDsOnYieldAndOverflow(t *testing.T) {
	dir := t.TempDir()
	sysRoot := t.TempDir()
	for _, ev := range []string{"event0", "event1", "event2"} {
		if err := os.WriteFile(filepath.Join(dir, ev), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	leds0 := setupSysfsPadLEDs(t, sysRoot, "event0", "xpad0")
	leds1 := setupSysfsPadLEDs(t, sysRoot, "event1",
		"input1:white:player-1", "input1:white:player-2", "input1:white:player-3", "input1:white:player-4", "input1:white:player-5",
	)
	leds2 := setupSysfsPadLEDs(t, sysRoot, "event2",
		"input2:green:player-1", "input2:green:player-2", "input2:green:player-3", "input2:green:player-4",
	)

	nextFD := 100
	fds := map[int]string{}
	ins := &nativeInputs{
		automatic:     true,
		label:         "test",
		dir:           dir,
		sysClassInput: sysRoot,
		openFile: func(path string) (int, error) {
			nextFD++
			fds[nextFD] = filepath.Base(path)
			return nextFD, nil
		},
		kindOf: func(int) InputKind { return InputGamepad },
		padID: func(fd int) (uint16, uint16) {
			if fds[fd] == "event0" {
				return 0x081f, 0xe401 // yielding fixture
			}
			return 0x054c, 0x0ce6
		},
	}

	if err := ins.openPath(nil, filepath.Join(dir, "event0")); err != nil {
		t.Fatal(err)
	}
	app := NewApp(nil, 1280, 720, 100)
	ins.publishPads(app)
	if got := readSysfsTrim(t, filepath.Join(leds0, "xpad0", "brightness")); got != "2" {
		t.Fatalf("fixture alone = %q, want P1 (2)", got)
	}

	// Plugging DualSense on event1 takes Player 1 from fixture, moving fixture to Player 2.
	if err := ins.openPath(app, filepath.Join(dir, "event1")); err != nil {
		t.Fatal(err)
	}
	if got := readSysfsTrim(t, filepath.Join(leds1, "input1:white:player-3", "brightness")); got != "1" {
		t.Fatalf("DualSense should take P1 (player-3=1), got %q", got)
	}
	if got := readSysfsTrim(t, filepath.Join(leds0, "xpad0", "brightness")); got != "3" {
		t.Fatalf("fixture should move to P2 (3), got %q", got)
	}

	// Third pad stays menu-only (player 0 -> all LEDs off).
	_ = os.WriteFile(filepath.Join(leds2, "input2:green:player-1", "brightness"), []byte("1\n"), 0o644)
	if err := ins.openPath(app, filepath.Join(dir, "event2")); err != nil {
		t.Fatal(err)
	}
	if got := readSysfsTrim(t, filepath.Join(leds2, "input2:green:player-1", "brightness")); got != "0" {
		t.Fatalf("3rd overflow pad should have LED turned off, got %q", got)
	}
}
