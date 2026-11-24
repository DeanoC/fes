// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func TestNativeLifecycleTrace(t *testing.T) {
	f, b, client := backendFixture(t)
	ctx := context.Background()
	var output strings.Builder
	serial, err := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false },
		func(_ context.Context, payload []byte) error {
			_, err := output.Write(payload)
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	serial.EnableLifecycleTrace()
	source := &programKeyboardReports{}
	p, err := NewProgramInputPlatform(serial, b, source)
	if err != nil {
		t.Fatal(err)
	}
	play, err := client.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	stages := []string{"loading", "menu-closed", "preferences-complete", "retention-complete",
		"preflight-complete", "storage-contained", "configuration-complete", "complete"}
	trace := output.String()
	for _, stage := range stages {
		_, next, found := strings.Cut(trace, "FES_NATIVE_LAUNCH_STAGE stage="+stage+" ")
		if !found {
			t.Fatalf("missing or out-of-order launch stage %s:\n%s", stage, output.String())
		}
		trace = next
	}
	output.Reset()
	source.reports = [][]byte{
		make([]byte, 8),
		{0, 0, 0x04, 0, 0, 0, 0, 0}, // ordinary typing
		make([]byte, 8),
		{0x05, 0, 0, 0, 0, 0, 0, 0},
		{0x05, 0, 0, 0, 0, 0, 0, 0}, // duplicate report
		{0x05, 0, 0x29, 0, 0, 0, 0, 0},
	}
	frame, err := p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
		t.Fatalf("trace changed shortcut behavior: %+v, %v", frame, err)
	}
	trace = output.String()
	if strings.Count(trace, "FES_NATIVE_KEYBOARD_SHORTCUT") != 2 ||
		!strings.Contains(trace, "armed=true control=true alt=true escape=true quit=false") ||
		!strings.Contains(trace, "source=usb-boot-keyboard action=stop") {
		t.Fatalf("shortcut states differ:\n%s", trace)
	}
	if _, err := client.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	source.reports = [][]byte{make([]byte, 8), {0, 0, 0x14, 0, 0, 0, 0, 0}}
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdQuit {
		t.Fatalf("trace changed Quit: %+v, %v", frame, err)
	}
	trace = output.String()
	if !strings.Contains(trace, "state=ready") ||
		!strings.Contains(trace, "armed=true control=false alt=false escape=false quit=true") ||
		!strings.Contains(trace, "source=usb-boot-keyboard action=quit") {
		t.Fatalf("Quit evidence differs:\n%s", trace)
	}
}
