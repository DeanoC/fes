package misterruntime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type mediaKeyboardControl struct {
	mediaControl
	keysMu           sync.Mutex
	busy             bool
	matrix           uint64
	during           chan struct{}
	keyboardResponse *misterruntime.Protocol2Response
	failNeutral      bool
	dispatchErr      error
}

type blockingMatrixControl struct {
	mediaControl
	entered, release chan struct{}
	response         misterruntime.Protocol2Response
}

func (c *blockingMatrixControl) SetKeyboard(context.Context, string, string, uint64, uint64) (misterruntime.Protocol2Response, error) {
	close(c.entered)
	<-c.release
	return c.response, nil
}

func testMatrixPost(instance, packageID string, generation, sequence uint64) localhid.MatrixPost {
	var sink [16]byte
	sink[0] = 1
	return localhid.MatrixPost{Witness: localhid.MatrixWitness{Sink: sink, Sequence: sequence, Source: 1, RuntimeInstance: instance, PackageID: packageID, Generation: generation}, Validate: func(context.Context) error { return nil }}
}

func (c *mediaKeyboardControl) SetKeyboard(_ context.Context, instance, packageID string, generation, matrix uint64) (misterruntime.Protocol2Response, error) {
	c.keysMu.Lock()
	defer c.keysMu.Unlock()
	if c.dispatchErr != nil {
		return misterruntime.Protocol2Response{}, c.dispatchErr
	}
	if c.failNeutral && matrix == zx81keys.Neutral {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "busy", Phase: "input", Message: "neutralization failed"}}, nil
	}
	if c.busy {
		select {
		case c.during <- struct{}{}:
		default:
		}
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "busy", Phase: "input", Message: "media in progress"}}, nil
	}
	c.matrix = matrix
	if c.keyboardResponse != nil {
		return *c.keyboardResponse, nil
	}
	return mediaResponse(), nil
}

func TestPostDispatchErrorCannotClaimNoWrite(t *testing.T) {
	response := mediaResponse()
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response, dispatchErr: localhid.ErrMatrixNotDispatched}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	post := testMatrixPost(*response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation, 1)
	if err := r.SetKeyboard(context.Background(), post, 0); err == nil || errors.Is(err, localhid.ErrMatrixNotDispatched) {
		t.Fatalf("downstream error claimed no write: %v", err)
	}
	c.dispatchErr = nil
	next := testMatrixPost(*response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation, 2)
	if err := r.SetKeyboard(context.Background(), next, 0); !errors.Is(err, localhid.ErrMatrixNotDispatched) {
		t.Fatalf("uncertain operation did not fence next input: %v", err)
	}
	next.NeutralOf = &post.Witness
	if err := r.SetKeyboard(context.Background(), next, zx81keys.Neutral); err != nil {
		t.Fatalf("exact neutral: %v", err)
	}
}

func TestDevelopmentMediaDoesNotRestoreKeysFromPreviousGeneration(t *testing.T) {
	response := mediaResponse()
	response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	keys := input.NewKeyboardSink()
	var failed localhid.MatrixWitness
	if err := keys.BindInitial(input.MatrixBinding{RuntimeInstance: *response.RuntimeInstance, PackageID: response.ActivePackage.PackageID, Generation: 9}); err != nil {
		t.Fatal(err)
	}
	keys.SetPoster(func(ctx context.Context, post localhid.MatrixPost, matrix uint64) error {
		err := r.SetKeyboard(ctx, post, matrix)
		if err != nil {
			failed = post.Witness
		}
		return err
	})
	if err := keys.Apply(protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Code: uint16(zx81keys.Letter('J')), Action: uint8(remoteinput.ActionPress)}); err != nil {
		t.Fatal(err)
	}
	c.failNeutral = true
	if err := keys.ReleaseAll(); err == nil {
		t.Fatal("failed neutralization was not reported")
	}
	c.failNeutral = false
	*response.Generation = 10 // Successful replacement has a fresh target-local generation.
	proof := localhid.MatrixRetirement{RetireOf: failed, ObservedRuntimeInstance: *response.RuntimeInstance, ObservedPackageID: response.ActivePackage.PackageID, ObservedGeneration: 10, Validate: func(context.Context) error { return nil }}
	if err := r.RetireMatrixUncertainty(context.Background(), proof); err != nil {
		t.Fatal(err)
	}
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
		c.matrix = zx81keys.Neutral
		return response, nil
	}
	err := r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 10})
	if err != nil {
		t.Fatal(err)
	}
	if c.matrix != zx81keys.Neutral {
		t.Fatalf("previous generation's held key restored: %x", c.matrix)
	}
}

func TestDevelopmentMediaRefusesChangedRuntimeInstance(t *testing.T) {
	first := mediaResponse()
	second := first
	instance := strings.Repeat("2", 32)
	second.RuntimeInstance = &instance
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2Script: []protocol2StatusResult{{response: first}, {response: second}}}}, during: make(chan struct{}, 1)}
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
		t.Fatal("media dispatched after runtime changed")
		return misterruntime.Protocol2Response{}, nil
	}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	err := r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
	if err == nil {
		t.Fatal("runtime change accepted")
	}
}

func TestDevelopmentMediaDoesNotRestoreAfterUnconfirmedRelease(t *testing.T) {
	response := mediaResponse()
	response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	instance, id, generation := *response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation
	press := testMatrixPost(instance, id, generation, 1)
	if err := r.SetKeyboard(context.Background(), press, 0); err != nil {
		t.Fatal(err)
	}
	c.failNeutral = true
	release := testMatrixPost(instance, id, generation, 2)
	if err := r.SetKeyboard(context.Background(), release, zx81keys.Neutral); err == nil {
		t.Fatal("failed release was accepted")
	}
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
		t.Fatal("media dispatched after unconfirmed release")
		return misterruntime.Protocol2Response{}, nil
	}
	err := r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: id, Generation: generation})
	if err == nil || err.Phase != "input" {
		t.Fatalf("unconfirmed release did not fence transfer: %v", err)
	}
	c.failNeutral = false
	retry := testMatrixPost(instance, id, generation, 3)
	if err := r.SetKeyboard(context.Background(), retry, zx81keys.Neutral); err == nil {
		t.Fatal("ordinary post cleared uncertainty")
	}
	wrong := release.Witness
	wrong.Source++
	retry.NeutralOf = &wrong
	if err := r.SetKeyboard(context.Background(), retry, zx81keys.Neutral); err == nil {
		t.Fatal("another source cleared uncertainty")
	}
	retry.NeutralOf = &release.Witness
	if err := r.SetKeyboard(context.Background(), retry, zx81keys.Neutral); err != nil {
		t.Fatalf("confirmed source neutral: %v", err)
	}
}

func TestMatrixRetirementRequiresExactWitnessAndPositiveObservation(t *testing.T) {
	response := mediaResponse()
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response, failNeutral: true}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	instance, id, generation := *response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation
	failed := testMatrixPost(instance, id, generation, 1)
	if err := r.SetKeyboard(context.Background(), failed, zx81keys.Neutral); err == nil {
		t.Fatal("failed matrix was accepted")
	}
	proof := localhid.MatrixRetirement{RetireOf: failed.Witness, ObservedRuntimeInstance: instance, ObservedPackageID: id, ObservedGeneration: generation, Validate: func(context.Context) error { return nil }}
	if err := r.RetireMatrixUncertainty(context.Background(), proof); err == nil {
		t.Fatal("same generation retired")
	}
	proof.ObservedGeneration++
	proof.RetireOf.Source++
	if err := r.RetireMatrixUncertainty(context.Background(), proof); err == nil {
		t.Fatal("wrong source retired")
	}
	proof.RetireOf = failed.Witness
	proof.Validate = func(context.Context) error { return context.Canceled }
	if err := r.RetireMatrixUncertainty(context.Background(), proof); err == nil {
		t.Fatal("invalidated observation retired")
	}
	proof.Validate = func(context.Context) error { return nil }
	if err := r.RetireMatrixUncertainty(context.Background(), proof); err != nil {
		t.Fatalf("proven retirement: %v", err)
	}
}

func TestMediaRestoreUncertaintyRequiresOwnRetirement(t *testing.T) {
	response := mediaResponse()
	response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response, failNeutral: true}
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) { return response, nil }
	r := misterruntime.NewRuntime(c, "", 0, 0)
	instance, id, generation := *response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation
	if err := r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: id, Generation: generation}); err == nil || err.Phase != "input" {
		t.Fatalf("failed media restoration accepted: %v", err)
	}
	witness, ok, err := r.MatrixMediaUncertainty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || witness.Operation == ([16]byte{}) {
		t.Fatal("missing media uncertainty witness")
	}
	if err := r.CheckMatrixAdmission(context.Background(), instance, id, generation, func(context.Context) error { return nil }); err == nil {
		t.Fatal("uncertain adapter admitted a new matrix source")
	}
	post := testMatrixPost(instance, id, generation, 1)
	if err := r.SetKeyboard(context.Background(), post, 0); !errors.Is(err, localhid.ErrMatrixNotDispatched) {
		t.Fatalf("post dispatched under media uncertainty: %v", err)
	}
	proof := localhid.MatrixMediaRetirement{RetireOf: witness, ObservedRuntimeInstance: instance, ObservedPackageID: id, ObservedGeneration: generation, Validate: func(context.Context) error { return nil }}
	if err := r.RetireMatrixMediaUncertainty(context.Background(), proof); err == nil {
		t.Fatal("same generation retired")
	}
	proof.ObservedGeneration++
	proof.RetireOf.Operation[0] ^= 1
	if err := r.RetireMatrixMediaUncertainty(context.Background(), proof); err == nil {
		t.Fatal("stale operation retired")
	}
	proof.RetireOf = witness
	proof.Validate = func(context.Context) error { return context.Canceled }
	if err := r.RetireMatrixMediaUncertainty(context.Background(), proof); err == nil {
		t.Fatal("invalidated proof retired")
	}
	proof.Validate = func(context.Context) error { return nil }
	if err := r.RetireMatrixMediaUncertainty(context.Background(), proof); err != nil {
		t.Fatalf("proven retirement: %v", err)
	}
	if err := r.CheckMatrixAdmission(context.Background(), instance, id, generation+1, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("clean adapter rejected new source: %v", err)
	}
}

func TestMediaRestoreNeutralRequiresConfirmedMatchingOperation(t *testing.T) {
	response := mediaResponse()
	response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response, failNeutral: true}
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) { return response, nil }
	r := misterruntime.NewRuntime(c, "", 0, 0)
	id, generation := response.ActivePackage.PackageID, *response.Generation
	if err := r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: id, Generation: generation}); err == nil {
		t.Fatal("failed restore accepted")
	}
	witness, ok, err := r.MatrixMediaUncertainty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("missing media witness")
	}
	neutral := localhid.MatrixMediaNeutral{MediaOf: witness, Validate: func(context.Context) error { return nil }}
	if err := r.NeutralMatrixMediaUncertainty(context.Background(), neutral); err == nil {
		t.Fatal("failed neutral accepted")
	}
	if _, ok, err := r.MatrixMediaUncertainty(context.Background()); err != nil || !ok {
		t.Fatal("failed neutral cleared uncertainty")
	}
	c.failNeutral = false
	wrong := neutral
	wrong.MediaOf.Operation[0] ^= 1
	if err := r.NeutralMatrixMediaUncertainty(context.Background(), wrong); err == nil {
		t.Fatal("wrong witness accepted")
	}
	calls := 0
	neutral.Validate = func(context.Context) error {
		calls++
		if calls == 2 {
			return context.Canceled
		}
		return nil
	}
	if err := r.NeutralMatrixMediaUncertainty(context.Background(), neutral); err == nil {
		t.Fatal("invalidated post accepted")
	}
	if _, ok, err := r.MatrixMediaUncertainty(context.Background()); err != nil || !ok {
		t.Fatal("invalidated post cleared uncertainty")
	}
	neutral.Validate = func(context.Context) error { return nil }
	if err := r.NeutralMatrixMediaUncertainty(context.Background(), neutral); err != nil {
		t.Fatalf("confirmed neutral: %v", err)
	}
	if _, ok, err := r.MatrixMediaUncertainty(context.Background()); err != nil || ok {
		t.Fatal("confirmed neutral did not clear uncertainty")
	}
}

func TestKeyboardAdmissionCancelsWhileMediaOwnsLock(t *testing.T) {
	response := mediaResponse()
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response}
	entered, release := make(chan struct{}), make(chan struct{})
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
		close(entered)
		<-release
		return response, nil
	}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	done := make(chan *protocol.APIError, 1)
	go func() {
		done <- r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: response.ActivePackage.PackageID, Generation: *response.Generation})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	post := testMatrixPost(*response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation, 1)
	if err := r.SetKeyboard(ctx, post, 0); !errors.Is(err, localhid.ErrMatrixNotDispatched) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting input: %v", err)
	}
	if _, _, err := r.MatrixMediaUncertainty(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting uncertainty query: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMediaAdmissionCancelsWhileKeyboardOwnsLock(t *testing.T) {
	response := mediaResponse()
	c := &blockingMatrixControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, entered: make(chan struct{}), release: make(chan struct{}), response: response}
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
		t.Fatal("canceled media dispatched")
		return response, nil
	}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	post := testMatrixPost(*response.RuntimeInstance, response.ActivePackage.PackageID, *response.Generation, 1)
	postDone := make(chan error, 1)
	go func() { postDone <- r.SetKeyboard(context.Background(), post, 0) }()
	<-c.entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := r.LoadDevelopmentMedia(ctx, 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: response.ActivePackage.PackageID, Generation: *response.Generation})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("media wait: %v", err)
	}
	close(c.release)
	if err := <-postDone; err != nil {
		t.Fatal(err)
	}
}
func TestDevelopmentMediaSerializesKeyboardAndRestoresHeldMatrix(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "held key", true: "event during transfer"}[during], func(t *testing.T) {
			response := mediaResponse()
			response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
			c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1)}
			entered, release := make(chan struct{}), make(chan struct{})
			c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
				c.keysMu.Lock()
				c.busy = true
				c.matrix = zx81keys.Neutral
				c.keysMu.Unlock()
				close(entered)
				<-release
				c.keysMu.Lock()
				c.busy = false
				c.keysMu.Unlock()
				return response, nil
			}
			r := misterruntime.NewRuntime(c, "", 0, 0)
			keys := input.NewKeyboardSink()
			if err := keys.BindInitial(input.MatrixBinding{RuntimeInstance: *response.RuntimeInstance, PackageID: response.ActivePackage.PackageID, Generation: 9}); err != nil {
				t.Fatal(err)
			}
			keys.SetPoster(func(ctx context.Context, post localhid.MatrixPost, matrix uint64) error {
				return r.SetKeyboard(ctx, post, matrix)
			})
			frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Code: uint16(zx81keys.Letter('J')), Action: uint8(remoteinput.ActionPress)}
			if err := keys.Apply(frame); err != nil {
				t.Fatal(err)
			}
			c.keysMu.Lock()
			held := c.matrix
			c.keysMu.Unlock()
			done := make(chan *protocol.APIError, 1)
			go func() {
				done <- r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
			}()
			<-entered
			var keyDone chan error
			if during {
				keyDone = make(chan error, 1)
				frame.Action = uint8(remoteinput.ActionRelease)
				go func() { keyDone <- keys.Apply(frame) }()
				select {
				case <-c.during:
					t.Error("keyboard reached busy runtime during media")
				case <-time.After(20 * time.Millisecond):
				}
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			want := held
			if during {
				if err := <-keyDone; err != nil {
					t.Fatalf("input stream would close: %v", err)
				}
				want = zx81keys.Neutral
			}
			c.keysMu.Lock()
			got := c.matrix
			c.keysMu.Unlock()
			if got != want {
				t.Fatalf("keyboard matrix=%x want=%x", got, want)
			}
		})
	}
}
