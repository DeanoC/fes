package hostapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type leasedService struct {
	*fakeService
	releases    int
	releaseHook func()
}

func (s *leasedService) ReleaseKitLease(context.Context) error {
	s.releases++
	if s.releaseHook != nil {
		s.releaseHook()
	}
	return nil
}
func TestExplicitStopReleasesKitOnlyAfterCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			order := []string{}
			base := &fakeService{status: protocol.Status{State: protocol.StateIdle}, stopped: protocol.Status{State: protocol.StateIdle}, order: &order}
			if fail {
				base.stopErr = errors.New("cleanup failed")
			}
			input := &fakeRemoteInput{order: &order, status: host.RemoteInputStatus{State: host.RemoteInputAttached}}
			service := &leasedService{fakeService: base, releaseHook: func() {
				if len(input.detach) != 1 {
					t.Error("lease released before input detach")
				}
				order = append(order, "lease.release")
			}}
			response := serve(t, hostapi.New(service, hostapi.WithRemoteInput(input)), http.MethodPost, "/api/v1/session/stop")
			if fail {
				if service.releases != 0 {
					t.Fatal("released after failed cleanup")
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("stop: %d %s", response.Code, response.Body.String())
			}
			if got := strings.Join(order, ","); got != "input.detach,service.stop,lease.release" {
				t.Fatalf("order %q", got)
			}
		})
	}
}
