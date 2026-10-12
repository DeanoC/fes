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
	presence.observe(false, probeErr, now)
	if presence.bound(now) {
		t.Fatal("failed initial probe admitted play input")
	}
	if presence.observedUnbound() {
		t.Fatal("failed initial probe counted as the core leaving")
	}
	presence.observe(true, nil, now)
	presence.observe(false, probeErr, now.Add(localCorePositiveTTL/2))
	if !presence.bound(now.Add(localCorePositiveTTL / 2)) {
		t.Fatal("brief probe failure interrupted play input")
	}
	if presence.observedUnbound() {
		t.Fatal("probe failure counted as the core leaving")
	}
	if presence.bound(now.Add(localCorePositiveTTL)) {
		t.Fatal("probe failure retained stale core presence beyond the TTL")
	}
	if presence.observedUnbound() {
		t.Fatal("aged core presence counted as the core leaving")
	}
	presence.observe(true, nil, now.Add(2*localCorePositiveTTL))
	if !presence.bound(now.Add(2 * localCorePositiveTTL)) {
		t.Fatal("successful probe did not restore play input")
	}
	if presence.observedUnbound() {
		t.Fatal("successful bound probe counted as the core leaving")
	}
	presence.observe(false, nil, now.Add(2*localCorePositiveTTL))
	if presence.bound(now.Add(2 * localCorePositiveTTL)) {
		t.Fatal("successful idle probe did not clear core presence immediately")
	}
	if !presence.observedUnbound() {
		t.Fatal("successful idle probe did not record the core leaving")
	}
}

func TestLocalCorePresenceRefreshesPositiveExpiry(t *testing.T) {
	var presence localCorePresence
	now := time.Unix(10, 0)
	presence.observe(true, nil, now)
	presence.observe(true, nil, now.Add(localCorePositiveTTL/2))
	if !presence.bound(now.Add(localCorePositiveTTL)) {
		t.Fatal("fresh successful observation expired with the earlier sample")
	}
	if presence.bound(now.Add(localCorePositiveTTL + localCorePositiveTTL/2)) {
		t.Fatal("refreshed observation never expired")
	}
}
