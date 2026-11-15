// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"errors"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"sync"
	"testing"
)

type callFunc func([7]uint64) ([4]uint64, error)

func (f callFunc) Invoke(a [7]uint64) ([4]uint64, error) { return f(a) }
func wireReply(flags uint32, generation uint64) [4]uint64 {
	return [4]uint64{0, uint64(g.FesEmbeddedSdTag)<<32 | uint64(flags), 17, generation}
}
func TestReceiptMismatchRetainsLastOwnership(t *testing.T) {
	tests := map[string]func(*[4]uint64){
		"tag":               func(r *[4]uint64) { r[1] ^= 1 << 63 },
		"owner":             func(r *[4]uint64) { r[2]++ },
		"generation-stale":  func(r *[4]uint64) { r[3]-- },
		"generation-skip":   func(r *[4]uint64) { r[3]++ },
		"reserved":          func(r *[4]uint64) { r[1] |= 1 << 31 },
		"contained-active":  func(r *[4]uint64) { r[1] |= uint64(g.FesEmbeddedSdContained) },
		"prepared-too-soon": func(r *[4]uint64) { r[1] |= uint64(g.FesEmbeddedSdPrepared) },
		"error-state":       func(r *[4]uint64) { r[0] = 4 },
		"unknown-status":    func(r *[4]uint64) { *r = [4]uint64{7, uint64(g.FesEmbeddedSdTag) << 32} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c, err := NewClient(callFunc(func(a [7]uint64) ([4]uint64, error) {
				calls++
				if calls == 1 {
					if a != [7]uint64{uint64(g.FesEmbeddedSdCall), 0, 0, uint64(g.FesEmbeddedSdCookie)} {
						t.Fatalf("query %#v", a)
					}
					return wireReply(0x39, 1), nil
				}
				if a != [7]uint64{uint64(g.FesEmbeddedSdCall), 1, 17, uint64(g.FesEmbeddedSdCookie), 1} {
					t.Fatalf("resume %#v", a)
				}
				r := wireReply(0x33, 2)
				mutate(&r)
				return r, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Resume(); !errors.Is(err, ErrRetired) {
				t.Fatal(err)
			}
			r, valid := c.LastReceipt()
			if valid || r != (Receipt{Owner: 17, Generation: 1}) {
				t.Fatalf("%+v %v", r, valid)
			}
			if !errors.Is(c.Stop(), ErrRetired) || !errors.Is(c.Resume(), ErrRetired) || calls != 2 {
				t.Fatalf("retired calls=%d", calls)
			}
		})
	}
}
func TestQueryRejectsUnreadyOrActiveService(t *testing.T) {
	for _, r := range [][4]uint64{
		{5, uint64(g.FesEmbeddedSdTag) << 32}, wireReply(0x33, 2),
		{5, uint64(g.FesEmbeddedSdTag) << 32, 17, 1}, wireReply(0x3d, 1),
		{0, uint64(g.FesEmbeddedSdTag)<<32 | 0x39, 0, 1},
	} {
		c, err := NewClient(callFunc(func([7]uint64) ([4]uint64, error) { return r, nil }))
		if err == nil || c != nil {
			t.Fatalf("adopted %#v", r)
		}
	}
}
func TestConcurrentResumeIssuesOneGrant(t *testing.T) {
	calls := 0
	c, err := NewClient(callFunc(func(a [7]uint64) ([4]uint64, error) {
		calls++
		if a[1] == 0 {
			return wireReply(0x39, 1), nil
		}
		return wireReply(0x33, 2), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			err := c.Resume()
			if err != nil && !errors.Is(err, ErrTransition) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls != 2 {
		t.Fatal(calls)
	}
	if !errors.Is(c.Stop(), ErrRetired) {
		t.Fatal("incorrect Stop receipt accepted")
	}
}

func TestResumeAfterVerifiedReturnBindsLiveClosureBeforeOneGrant(t *testing.T) {
	calls := 0
	c := &Client{last: Receipt{Owner: 17, Generation: 4}}
	c.cpu = callFunc(func(a [7]uint64) ([4]uint64, error) {
		calls++
		requests := [][7]uint64{
			{uint64(g.FesEmbeddedSdCall), 0, 0, uint64(g.FesEmbeddedSdCookie)},
			{uint64(g.FesEmbeddedSdCall), 1, 17, uint64(g.FesEmbeddedSdCookie), 6},
			{uint64(g.FesEmbeddedSdCall), 2, 17, uint64(g.FesEmbeddedSdCookie), 7},
			{uint64(g.FesEmbeddedSdCall), 3, 17, uint64(g.FesEmbeddedSdCookie), 8},
		}
		if calls > len(requests) || a != requests[calls-1] {
			t.Fatalf("unexpected transition %d: %#v", calls, a)
		}
		return wireReply([]uint32{0x39, 0x33, 0x37, 0x39}[calls-1], uint64(calls+5)), nil
	})
	if err := c.ResumeAfterReturn(17, 6); err != nil {
		t.Fatal(err)
	}
	if err := c.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if receipt, ok := c.LastReceipt(); !ok || receipt != (Receipt{Owner: 17, Generation: 9}) {
		t.Fatal(receipt, ok)
	}
	if err := c.ResumeAfterReturn(17, 6); !errors.Is(err, ErrTransition) || calls != 4 {
		t.Fatal("stale return was reused", err, calls)
	}
}

func TestReturnResumeRejectsChangedQueryOrUncertainGrantWithoutReplay(t *testing.T) {
	for _, fault := range []string{"query-error", "query-tag", "query-owner", "query-stale", "query-ahead", "query-active", "grant-error", "grant-owner", "grant-generation"} {
		t.Run(fault, func(t *testing.T) {
			calls := 0
			c := &Client{last: Receipt{Owner: 17, Generation: 4}}
			c.cpu = callFunc(func(a [7]uint64) ([4]uint64, error) {
				calls++
				r := wireReply(0x39, 6)
				if a[1] == 0 {
					switch fault {
					case "query-error":
						return [4]uint64{}, errors.New("uncertain query")
					case "query-tag":
						r[1] ^= 1 << 63
					case "query-owner":
						r[2]++
					case "query-stale":
						r[3]--
					case "query-ahead":
						r[3]++
					case "query-active":
						r = wireReply(0x33, 6)
					}
				} else {
					r = wireReply(0x33, 7)
					switch fault {
					case "grant-error":
						return [4]uint64{}, errors.New("unknown grant")
					case "grant-owner":
						r[2]++
					case "grant-generation":
						r[3]++
					}
				}
				return r, nil
			})
			if err := c.ResumeAfterReturn(17, 6); !errors.Is(err, ErrRetired) {
				t.Fatal(err)
			}
			before := calls
			if !errors.Is(c.ResumeAfterReturn(17, 6), ErrRetired) || !errors.Is(c.Resume(), ErrRetired) || calls != before {
				t.Fatal("retired grant was replayed", calls)
			}
			last, ok := c.LastReceipt()
			wanted := uint64(4)
			if fault[:5] == "grant" {
				wanted = 6
			}
			if ok || last != (Receipt{Owner: 17, Generation: wanted}) {
				t.Fatal(last, ok)
			}
		})
	}
}

func TestInvalidLocalReturnDoesNotQueryOrRetireHealthyContainment(t *testing.T) {
	for _, returned := range []Receipt{{Owner: 18, Generation: 6}, {Owner: 17, Generation: 4},
		{Owner: 17, Generation: 3}, {Owner: 17, Generation: ^uint64(0) - 2}} {
		c := &Client{last: Receipt{Owner: 17, Generation: 4}, cpu: callFunc(func([7]uint64) ([4]uint64, error) {
			t.Fatal("invalid return reached firmware")
			return [4]uint64{}, nil
		})}
		if err := c.ResumeAfterReturn(returned.Owner, returned.Generation); !errors.Is(err, ErrTransition) {
			t.Fatal(returned, err)
		}
		if _, ok := c.LastReceipt(); !ok {
			t.Fatal("invalid local request retired an intact lease")
		}
	}
}
