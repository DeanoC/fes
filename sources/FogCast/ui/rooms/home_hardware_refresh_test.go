package rooms

import (
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/testwait"
)

func TestHomeQueuesHardwareRefreshAcrossRepeatedResume(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "current admission"
		if failed {
			name = "current failure discards superseded admission"
		}
		t.Run(name, func(t *testing.T) {
			fixture := &hardwareFixture{value: hostclient.HardwareSnapshot{
				Computers: []hostclient.ShelfComputer{computerEntry("superseded", "fes.zx81", "")},
			}}
			svc := &delayedFirstHardwareServices{
				hardwareFixture: fixture,
				started:         make(chan struct{}),
				release:         make(chan struct{}),
			}
			defer svc.releaseFirst()
			r := loadHome(t, Options{Services: svc, ReleaseOnly: true})
			select {
			case <-svc.started:
			case <-time.After(testwait.Timeout):
				t.Fatal("initial Home hardware read did not start")
			}

			// Returning from play can resume Home while its previous read is
			// still in flight. Repeated resumes should request one fresh read,
			// without publishing the superseded response or a busy refusal.
			r.Resume()
			r.Resume()
			fixture.lock.Lock()
			fixture.value.Computers = []hostclient.ShelfComputer{computerEntry("current", "fes.zx81", "")}
			if failed {
				fixture.err = errors.New("host unreachable")
			}
			fixture.lock.Unlock()
			svc.releaseFirst()
			stepUntil(t, r, func(Frame) bool { return homeSettled(r) && !r.hardwareReading })

			svc.callMu.Lock()
			calls := svc.calls
			svc.callMu.Unlock()
			if calls != 2 {
				t.Fatalf("hardware reads = %d, want initial plus one queued refresh", calls)
			}
			d := r.Destination()
			if failed {
				if d.Availability == AvailReady || d.GameID != "" {
					t.Fatalf("failed current read reused superseded admission: %+v", d)
				}
			} else if d.Availability != AvailReady || d.GameID != "current" {
				t.Fatalf("current admitted computer did not return after resume: %+v", d)
			}
		})
	}
}
