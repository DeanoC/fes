package kitlauncher

import (
	"errors"
	"testing"
	"time"
)

func TestLocalCorePresenceBoundsProbeFailures(t *testing.T) {
	var presence localCorePresence
	now := time.Unix(10, 0)
	probeErr := errors.New("runtime unavailable")
	presence.observe(LocalCoreSample{}, probeErr, now, 0)
	if presence.bound(now) {
		t.Fatal("failed initial probe admitted play input")
	}
	if presence.menuIdle() {
		t.Fatal("failed initial probe confirmed an idle menu")
	}
	presence.observe(LocalCoreSample{Bound: true}, nil, now, 0)
	presence.observe(LocalCoreSample{}, probeErr, now.Add(localCorePositiveTTL/2), 0)
	if !presence.bound(now.Add(localCorePositiveTTL / 2)) {
		t.Fatal("brief probe failure interrupted play input")
	}
	if presence.menuIdle() {
		t.Fatal("probe failure confirmed an idle menu")
	}
	if presence.bound(now.Add(localCorePositiveTTL)) {
		t.Fatal("probe failure retained stale core presence beyond the TTL")
	}
	if presence.menuIdle() {
		t.Fatal("aged core presence confirmed an idle menu")
	}
	presence.observe(LocalCoreSample{Bound: true}, nil, now.Add(2*localCorePositiveTTL), 0)
	if !presence.bound(now.Add(2 * localCorePositiveTTL)) {
		t.Fatal("successful probe did not restore play input")
	}
	if presence.menuIdle() {
		t.Fatal("successful bound probe confirmed an idle menu")
	}
	presence.observe(LocalCoreSample{}, nil, now.Add(2*localCorePositiveTTL), 0)
	if presence.bound(now.Add(2 * localCorePositiveTTL)) {
		t.Fatal("in-flight status did not clear core presence immediately")
	}
	if presence.menuIdle() {
		t.Fatal("starting or recovery status confirmed an idle menu")
	}
	presence.observe(LocalCoreSample{MenuIdle: true}, nil, now.Add(3*localCorePositiveTTL), 0)
	if presence.bound(now.Add(3 * localCorePositiveTTL)) {
		t.Fatal("idle menu confirmed a bound core")
	}
	if !presence.menuIdle() {
		t.Fatal("completed idle menu was not confirmed")
	}
	presence.observe(LocalCoreSample{Bound: true, MenuIdle: true}, nil, now.Add(3*localCorePositiveTTL), 0)
	if !presence.bound(now.Add(3*localCorePositiveTTL)) || presence.menuIdle() {
		t.Fatal("bound core was treated as an idle menu")
	}
}

func TestLocalCorePresenceRefreshesPositiveExpiry(t *testing.T) {
	var presence localCorePresence
	now := time.Unix(10, 0)
	presence.observe(LocalCoreSample{Bound: true}, nil, now, 0)
	presence.observe(LocalCoreSample{Bound: true}, nil, now.Add(localCorePositiveTTL/2), 0)
	if !presence.bound(now.Add(localCorePositiveTTL)) {
		t.Fatal("fresh successful observation expired with the earlier sample")
	}
	if presence.bound(now.Add(localCorePositiveTTL + localCorePositiveTTL/2)) {
		t.Fatal("refreshed observation never expired")
	}
}

func TestLocalCorePresenceIdleFollowsProbeGeneration(t *testing.T) {
	var presence localCorePresence
	now := time.Unix(10, 0)
	const baseline = uint64(4)
	presence.observe(LocalCoreSample{MenuIdle: true, MenuGeneration: baseline}, nil, now, 1)
	if presence.menuIdleAt(2, baseline) {
		t.Fatal("idle sample from before the hold confirmed the new hold")
	}
	presence.observe(LocalCoreSample{MenuIdle: true, MenuGeneration: baseline}, nil, now, 2)
	if got := presence.adoptShelfGeneration(2, 0); got != baseline {
		t.Fatalf("shelf generation = %d", got)
	}
	if presence.menuIdleAt(2, baseline) {
		t.Fatal("pre-launch idle generation confirmed the hold")
	}
	presence.observe(LocalCoreSample{MenuIdle: true, MenuGeneration: 9}, nil, now, 2)
	if !presence.menuIdleAt(2, baseline) {
		t.Fatal("new menu generation did not confirm the hold")
	}
	presence.observe(LocalCoreSample{}, errors.New("stale"), now, 1)
	if !presence.menuIdleAt(2, baseline) {
		t.Fatal("probe error from an earlier generation cleared the hold")
	}
	presence.observe(LocalCoreSample{}, errors.New("runtime unavailable"), now, 2)
	if presence.menuIdle() || presence.menuIdleAt(2, baseline) {
		t.Fatal("probe error kept a stale idle confirmation")
	}
	presence.observe(LocalCoreSample{}, nil, now, 2)
	if presence.menuIdle() || presence.menuIdleAt(2, baseline) {
		t.Fatal("starting or recovery status confirmed an idle menu")
	}
	presence.observe(LocalCoreSample{MenuIdle: true, MenuGeneration: baseline}, nil, now, 2)
	if !presence.menuIdleAt(2, baseline) || presence.bound(now) {
		t.Fatal("idle menu after leaving the shelf was not confirmed")
	}
	presence.observe(LocalCoreSample{Bound: true}, nil, now, 2)
	if presence.menuIdleAt(2, baseline) {
		t.Fatal("bound core confirmed an idle menu")
	}
}
