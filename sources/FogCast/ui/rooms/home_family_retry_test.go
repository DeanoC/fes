package rooms

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
)

func TestLibraryQueryWithoutServicesReturnsCancelAndTerminalError(t *testing.T) {
	pack := memPack(t, "query-no-service", `
local answered = false
function load()
  local cancel = library.query({}, function(games, err, transient)
    assert(games == nil and err ~= nil and transient == false)
    answered = true
  end)
  assert(type(cancel) == "function")
  cancel()
end
function draw()
  if answered then gfx.text("terminal", 1, 1, {size=20}) end
end
`, nil)
	r := newRoom(t, pack, Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "terminal") })
}

// retryHomeServices faults only the named family, keeping recents and the
// other family queries independent. Calls and the script are protected by mu.
type retryHomeServices struct {
	homeServices
	coreID      string
	failures    []error
	familyCalls int
	blockCall   int
	entered     chan struct{}
	canceled    chan struct{}
}

func (s *retryHomeServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	match := q.Platform == "fpga" && q.Collection == ""
	if match {
		match = false
		for _, id := range q.CoreIDs {
			if id == s.coreID {
				match = true
			}
		}
	}
	if !match {
		return s.homeServices.QueryGames(ctx, q, max)
	}
	s.mu.Lock()
	s.familyCalls++
	call := s.familyCalls
	var err error
	if call <= len(s.failures) {
		err = s.failures[call-1]
	}
	block := call == s.blockCall
	s.mu.Unlock()
	if block {
		close(s.entered)
		<-ctx.Done()
		close(s.canceled)
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return s.homeServices.QueryGames(ctx, q, max)
}

func (s *retryHomeServices) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.familyCalls
}

// Simulate 100 ms per step so tests cover backoff without sleeping through it.
// Yield between steps so the real asynchronous service goroutines can run.
func stepHomeRetryUntil(t *testing.T, r *Instance, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		r.Step(r.last.Add(100 * time.Millisecond))
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		if pred() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Home retry condition not reached")
}

func stepHomeRetryFor(t *testing.T, r *Instance, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		r.Step(r.last.Add(100 * time.Millisecond))
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}

func retryHome(t *testing.T, failures ...error) (*Instance, *retryHomeServices) {
	t.Helper()
	s := &retryHomeServices{coreID: "fes.zx81", failures: failures}
	s.games = []hostclient.Game{machineEntry("zx81-machine", "fes.zx81", ""), titleEntry("zx81-game", "fes.zx81")}
	local := &fakeLocalCores{cores: []localcores.Core{{
		PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true,
	}}}
	return loadHome(t, Options{Services: s, Local: local, ReleaseOnly: true}), s
}

func TestHomeRetriesTransientFamilyReadsWhileOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"timeout", context.DeadlineExceeded},
		{"wrapped timeout", &url.Error{Op: "Get", URL: "https://example.invalid", Err: context.DeadlineExceeded}},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		{"unexpected EOF", io.ErrUnexpectedEOF},
		{"service unavailable", hostclient.APIStatusError(503, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, s := retryHome(t, tc.err)
			stepHomeRetryUntil(t, r, func() bool {
				d := r.Destination()
				return s.count() >= 2 && d.Label == "Sinclair ZX81" && d.GameID == "zx81-machine" && d.Status == "1 game"
			})
			if n := s.count(); n != 2 {
				t.Fatalf("family reads = %d, want one retry", n)
			}
		})
	}
}

func TestHomeRetryDoesNotStealFocus(t *testing.T) {
	r, s := retryHome(t, context.DeadlineExceeded)
	r.Input("right")
	if d := r.Destination(); d.Label != "ZX Spectrum" {
		t.Fatalf("did not select Spectrum while Home was checking: %+v", d)
	}
	stepHomeRetryUntil(t, r, func() bool { return s.count() >= 2 && homeSettled(r) })
	if d := r.Destination(); d.Label != "ZX Spectrum" {
		t.Fatalf("recovering ZX81 moved focus: %+v", d)
	}
	r.Input("left")
	stepHomeRetryUntil(t, r, func() bool { return r.Destination().GameID == "zx81-machine" })
}

func TestHomeDoesNotRetryPermanentFamilyErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"bad request", hostclient.APIStatusError(400, nil)},
		{"unauthorized", hostclient.APIStatusError(401, nil)},
		{"internal error", hostclient.APIStatusError(500, nil)},
		{"malformed response", errors.New("invalid JSON")},
		{"certificate error", &url.Error{Op: "Get", URL: "https://example.invalid", Err: errors.New("x509: certificate signed by unknown authority")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, s := retryHome(t, tc.err)
			stepHomeRetryUntil(t, r, func() bool { return s.count() == 1 && homeSettled(r) })
			stepHomeRetryFor(t, r, 150) // 15 simulated seconds, past all retry delays.
			if n := s.count(); n != 1 {
				t.Fatalf("permanent error retried: family reads = %d", n)
			}
		})
	}
}

func TestHomeStopsRetryingAfterItsBudget(t *testing.T) {
	err := context.DeadlineExceeded
	r, s := retryHome(t, err, err, err, err, err, err)
	stepHomeRetryUntil(t, r, func() bool { return s.count() == 4 && homeSettled(r) })
	settledCalls := s.count()
	stepHomeRetryFor(t, r, 200)
	if n := s.count(); n != settledCalls || n > 4 {
		t.Fatalf("retry did not stop after its budget: settled=%d final=%d", settledCalls, n)
	}
}

// A terminal error in one family must not discard or continually reload a
// different family's successful retry.
type mixedRetryHomeServices struct {
	homeServices
	zxReads  int
	c64Reads int
}

func (s *mixedRetryHomeServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	var family string
	if q.Platform == "fpga" && q.Collection == "" {
		for _, id := range q.CoreIDs {
			if id == "fes.zx81" || id == "fes.c64" {
				family = id
				break
			}
		}
	}
	s.mu.Lock()
	var n int
	switch family {
	case "fes.zx81":
		s.zxReads++
		n = s.zxReads
	case "fes.c64":
		s.c64Reads++
		n = s.c64Reads
	}
	s.mu.Unlock()
	if family == "fes.zx81" && n == 1 {
		return nil, hostclient.APIStatusError(503, nil)
	}
	if family == "fes.c64" {
		return nil, hostclient.APIStatusError(400, nil)
	}
	return s.homeServices.QueryGames(ctx, q, max)
}

func (s *mixedRetryHomeServices) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zxReads, s.c64Reads
}

func TestHomeRetriesOnlyFailedTransientFamily(t *testing.T) {
	s := &mixedRetryHomeServices{}
	s.games = []hostclient.Game{machineEntry("zx81-machine", "fes.zx81", ""), titleEntry("zx81-game", "fes.zx81")}
	r := loadHome(t, Options{Services: s, ReleaseOnly: true})
	stepHomeRetryUntil(t, r, func() bool {
		zx, c64 := s.counts()
		d := r.Destination()
		return zx >= 2 && c64 >= 1 && d.Label == "Sinclair ZX81" && d.GameID == "zx81-machine" && d.Status == "1 game"
	})
	stepHomeRetryFor(t, r, 150)
	if zx, c64 := s.counts(); zx != 2 || c64 != 1 {
		t.Fatalf("retry counts after recovery: ZX81=%d C64=%d", zx, c64)
	}
}

func TestHomeCloseCancelsRetryInFlight(t *testing.T) {
	s := &retryHomeServices{coreID: "fes.zx81", failures: []error{context.DeadlineExceeded},
		blockCall: 2, entered: make(chan struct{}), canceled: make(chan struct{})}
	r := loadHome(t, Options{Services: s, ReleaseOnly: true})
	stepHomeRetryUntil(t, r, func() bool {
		select {
		case <-s.entered:
			return true
		default:
			return false
		}
	})
	r.Close()
	select {
	case <-s.canceled:
	case <-time.After(testwait.Timeout):
		t.Fatal("closing Home did not cancel its in-flight retry")
	}
	if n := s.count(); n != 2 {
		t.Fatalf("closing Home issued another retry: %d calls", n)
	}
}

func TestHomeReloadDiscardsScheduledRetry(t *testing.T) {
	r, s := retryHome(t, context.DeadlineExceeded)
	// Drain the initial results without advancing room time: Home must have
	// settled on the failed read before Resume discards its retry schedule.
	deadline := time.Now().Add(testwait.Timeout)
	for !homeSettled(r) && time.Now().Before(deadline) {
		r.Step(r.last)
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if !homeSettled(r) || s.count() != 1 {
		t.Fatalf("initial failed read did not settle: settled=%v reads=%d", homeSettled(r), s.count())
	}
	r.Resume()
	stepHomeRetryUntil(t, r, func() bool { return s.count() == 2 && r.Destination().GameID == "zx81-machine" })
	stepHomeRetryFor(t, r, 100)
	if n := s.count(); n != 2 {
		t.Fatalf("old retry survived Home reload: %d family reads", n)
	}
}

func TestHomeReloadCancelsInFlightRetry(t *testing.T) {
	s := &retryHomeServices{coreID: "fes.zx81", failures: []error{context.DeadlineExceeded},
		blockCall: 2, entered: make(chan struct{}), canceled: make(chan struct{})}
	s.games = []hostclient.Game{machineEntry("zx81-machine", "fes.zx81", "")}
	r := loadHome(t, Options{Services: s, ReleaseOnly: true})
	stepHomeRetryUntil(t, r, func() bool {
		select {
		case <-s.entered:
			return true
		default:
			return false
		}
	})
	r.Resume()
	select {
	case <-s.canceled:
	case <-time.After(testwait.Timeout):
		t.Fatal("Home reload did not cancel its old in-flight retry")
	}
	stepHomeRetryUntil(t, r, func() bool { return s.count() == 3 && r.Destination().GameID == "zx81-machine" })
	stepHomeRetryFor(t, r, 100)
	if n := s.count(); n != 3 {
		t.Fatalf("old in-flight retry continued after reload: %d reads", n)
	}
}

// A service that has already committed to a response can still complete
// after its context is canceled. Its old retry must not replace new data.
type staleRetryHomeServices struct {
	homeServices
	reads    int
	entered  chan struct{}
	release  chan struct{}
	returned chan struct{}
	once     sync.Once
}

func (s *staleRetryHomeServices) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	zx := false
	if q.Platform == "fpga" && q.Collection == "" {
		for _, id := range q.CoreIDs {
			zx = zx || id == "fes.zx81"
		}
	}
	if !zx {
		return s.homeServices.QueryGames(ctx, q, max)
	}
	s.mu.Lock()
	s.reads++
	n := s.reads
	s.mu.Unlock()
	if n == 1 {
		return nil, context.DeadlineExceeded
	}
	if n == 2 {
		close(s.entered)
		<-s.release // Deliberately ignore ctx to model a late completed response.
		defer close(s.returned)
		return []hostclient.Game{machineEntry("stale-machine", "fes.zx81", "")}, nil
	}
	return s.homeServices.QueryGames(ctx, q, max)
}

func (s *staleRetryHomeServices) unblock() { s.once.Do(func() { close(s.release) }) }

func TestHomeReloadIgnoresLateOldRetryResult(t *testing.T) {
	s := &staleRetryHomeServices{entered: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	s.games = []hostclient.Game{machineEntry("new-machine", "fes.zx81", "")}
	defer s.unblock()
	r := loadHome(t, Options{Services: s, ReleaseOnly: true})
	stepHomeRetryUntil(t, r, func() bool {
		select {
		case <-s.entered:
			return true
		default:
			return false
		}
	})
	r.Resume()
	stepHomeRetryUntil(t, r, func() bool { return r.Destination().GameID == "new-machine" })
	s.unblock()
	select {
	case <-s.returned:
	case <-time.After(testwait.Timeout):
		t.Fatal("old retry did not return")
	}
	stepHomeRetryFor(t, r, 50)
	if d := r.Destination(); d.GameID != "new-machine" {
		t.Fatalf("old retry overwrote the newer family: %+v", d)
	}
}
