package tenfoot

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
)

func activeHardwareStatusSnapshot() Snapshot {
	return Snapshot{
		Room:     RoomSnapshot{DuringPlay: true},
		Session:  SessionSnapshot{State: "active"},
		Health:   HealthSnapshot{Ready: true, TargetReachable: true, TargetReady: false, Line: "kit not ready"},
		KitLease: KitLeaseSnapshot{State: "held", Owner: "fogcast@host", Line: "lease held · fogcast@host"},
		Status:   "Tape armed.",
	}
}

func TestHardwareRoomStatusShowsLiveTapeResults(t *testing.T) {
	for _, connection := range []string{"", "active", "ready"} {
		for _, status := range []string{"Tape armed.", "Tape ejected."} {
			t.Run(connection+"/"+status, func(t *testing.T) {
				snap := activeHardwareStatusSnapshot()
				snap.Health.Connection = hostclient.TargetConnection{State: connection}
				snap.Health.Line = kitHealthLine(false, true, hostclient.HealthResult{
					Ready: true, TargetReachable: true, TargetReady: false, Connection: snap.Health.Connection,
				})
				snap.Status = status
				if got := hardwareRoomStatus(snap); got != status {
					t.Fatalf("active tape result hidden by launch readiness: got %q, want %q", got, status)
				}
			})
		}
	}
}

func TestHardwareRoomStatusKeepsPrelaunchReadiness(t *testing.T) {
	for _, state := range []string{"idle", "launching", "failed", "active"} {
		t.Run(state, func(t *testing.T) {
			snap := activeHardwareStatusSnapshot()
			snap.Session.State = state
			snap.Room.DuringPlay = state != "active"
			if got := hardwareRoomStatus(snap); got != "kit not ready" {
				t.Fatalf("prelaunch readiness hidden: %q", got)
			}
		})
	}
}

func TestHardwareRoomStatusKeepsTransportLeaseAndNoticePriority(t *testing.T) {
	type statusCase struct {
		name   string
		change func(*Snapshot)
		want   string
	}
	cases := []statusCase{
		{"host unreachable", func(s *Snapshot) { s.Health.HostUnreachable = true; s.Health.Line = "host unreachable" }, "host unreachable"},
		{"host not ready", func(s *Snapshot) { s.Health.Ready = false; s.Health.Line = "host not ready" }, "host not ready"},
		{"target unreachable", func(s *Snapshot) { s.Health.TargetReachable = false; s.Health.Line = "kit unreachable" }, "kit unreachable"},
		{"lease unreachable", func(s *Snapshot) { s.KitLease.Unreachable = true; s.KitLease.Line = "kit status unavailable" }, "kit status unavailable"},
		{"foreign held lease", func(s *Snapshot) {
			s.KitLease.Owner = "hardware-operator"
			s.KitLease.Line = "lease held · hardware-operator"
		}, "lease held · hardware-operator"},
		{"notice", func(s *Snapshot) {
			s.Room.Notice = "The running machine changed. Review the refreshed setup."
			s.Health.HostUnreachable, s.Health.Line = true, "host unreachable"
			s.KitLease.Unreachable = true
		}, "The running machine changed. Review the refreshed setup."},
	}
	for _, state := range []string{"connecting", "disconnected", "version_mismatch", "busy", "recovery-required", "unknown"} {
		cases = append(cases, statusCase{"connection " + state, func(s *Snapshot) {
			s.Health.TargetReady = true
			s.Health.Connection = hostclient.TargetConnection{State: state, Message: "connection needs attention"}
			s.Health.Line = connectionLine(s.Health.Connection)
		}, connectionLine(hostclient.TargetConnection{State: state, Message: "connection needs attention"})})
	}
	for _, state := range []string{"busy", "blocked", "recovery-required", "revoking"} {
		cases = append(cases, statusCase{"lease " + state, func(s *Snapshot) {
			s.KitLease = KitLeaseSnapshot{State: state, Owner: "hardware-operator", Line: "lease " + state}
		}, "lease " + state})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := activeHardwareStatusSnapshot()
			tc.change(&snap)
			if got := hardwareRoomStatus(snap); got != tc.want {
				t.Fatalf("failure or notice lost priority: got %q, want %q", got, tc.want)
			}
		})
	}
}
