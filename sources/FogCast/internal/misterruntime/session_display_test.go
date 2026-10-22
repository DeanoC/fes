package misterruntime_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayControl struct {
	packageControl
	display func(string, uint64, protocol.SessionDisplay) (misterruntime.Protocol2Response, error)
}

func (c *sessionDisplayControl) SetSessionDisplay(_ context.Context, id string, gen uint64, d protocol.SessionDisplay) (misterruntime.Protocol2Response, error) {
	return c.display(id, gen, d)
}

type displayFocus struct {
	focused bool
	calls   int
}

func (f *displayFocus) BeginSessionDisplay(context.Context) (func(bool), error) {
	f.calls++
	f.focused = true
	return func(focused bool) { f.focused = focused }, nil
}

func TestRuntimeDisplayUsesCapturedGenerationAndRetainsFocusOnUnconfirmedClose(t *testing.T) {
	response := liveMediaResponse()
	response.Capabilities.ActiveInterfaces = append(response.Capabilities.ActiveInterfaces,
		misterruntime.Protocol2Interface{ID: "fes.memory.hps-ddr", Major: 1}, misterruntime.Protocol2Interface{ID: "fes.video.session-display", Major: 1})
	id := strings.Repeat("a", 64)
	response.MenuDisplay = &misterruntime.Protocol2MenuDisplay{Session: true, PackageID: &id, CoreGeneration: 9, Generation: 27}
	control := &sessionDisplayControl{packageControl: packageControl{status2: &response}}
	calls := 0
	reportedFocus := protocol.SessionFocus("")
	control.display = func(gotID string, generation uint64, d protocol.SessionDisplay) (misterruntime.Protocol2Response, error) {
		calls++
		if gotID != id || generation != 9 {
			t.Fatal("display followed a replacement binding")
		}
		result := response
		menu := *response.MenuDisplay
		menu.Available, menu.Focus = d.Visible, d.Focus
		if reportedFocus != "" {
			menu.Focus = reportedFocus
		}
		result.MenuDisplay = &menu
		return result, nil
	}
	r := misterruntime.NewRuntime(control, "", 0, 0)
	focus := &displayFocus{}
	r.ConfigureSessionDisplayFocus(focus)
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9}
	if err := r.SetSessionDisplay(context.Background(), protocol.SessionDisplay{Focus: protocol.SessionFocusLauncher}, b); err == nil || calls != 0 || focus.calls != 0 {
		t.Fatal("closed plane with launcher focus reached the runtime")
	}
	// A shown plane with machine focus leaves the fence open for machine input.
	if err := r.SetSessionDisplay(context.Background(), protocol.SessionDisplayShown, b); err != nil || focus.focused || focus.calls != 1 {
		t.Fatalf("shown: %v focus=%v", err, focus.focused)
	}
	if err := r.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err != nil || !focus.focused {
		t.Fatalf("open: %v focus=%v", err, focus.focused)
	}
	// A reply that disagrees about focus is not a confirmed return of input.
	reportedFocus = protocol.SessionFocusLauncher
	if err := r.SetSessionDisplay(context.Background(), protocol.SessionDisplayShown, b); err == nil || !focus.focused {
		t.Fatal("mismatched focus reply restored machine input")
	}
	reportedFocus = ""
	control.display = func(string, uint64, protocol.SessionDisplay) (misterruntime.Protocol2Response, error) {
		calls++
		return misterruntime.Protocol2Response{}, errors.New("lost reply")
	}
	if err := r.SetSessionDisplay(context.Background(), protocol.SessionDisplayClosed, b); err == nil || !focus.focused {
		t.Fatal("lost close restored input")
	}
	b.Generation++
	if err := r.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err == nil || calls != 4 || focus.calls != 4 {
		t.Fatal("stale open crossed input or physical admission")
	}
	if control.loadCalls != 0 {
		t.Fatal("display error loaded core")
	}
}
