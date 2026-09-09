package fogcast

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostexec"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
)

const serviceDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestServiceLaunchRememberedCacheHitDoesNotReadSource(t *testing.T) {
	for _, rootOnline := range []bool{true, false} {
		name := "online"
		if !rootOnline {
			name = "offline"
		}
		t.Run(name, func(t *testing.T) {
			content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
			game := serviceGame(content)
			game.RootOnline = rootOnline
			store := &fakeServiceCatalog{games: []catalog.Game{game}}
			preparer := &fakeServicePreparer{}
			client := &fakeServiceClient{activeGame: "megadrive-current"}
			client.probe = func(_ context.Context, system protocol.System, got protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
				if system != game.System || got != contentIdentity(content) {
					t.Fatalf("probe = %q %+v", system, got)
				}
				identity := got
				return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
			}
			client.launch = exactLaunchResponse(t, game, contentIdentity(content))
			service := newTestService(store, preparer, client)

			response, err := service.Launch(context.Background(), game.ID, nil)
			if err != nil {
				t.Fatalf("Launch: %v", err)
			}
			if response.Content != contentIdentity(content) || response.Status.State != protocol.StateActive {
				t.Fatalf("response = %+v", response)
			}
			if store.gameCalls != 1 || store.matchCalls != 1 || store.updateCalls != 0 {
				t.Fatalf("catalog calls = game:%d match:%d update:%d", store.gameCalls, store.matchCalls, store.updateCalls)
			}
			if preparer.calls != 0 || client.uploadCalls != 0 || client.launchCalls != 1 {
				t.Fatalf("source/target calls = prepare:%d upload:%d launch:%d", preparer.calls, client.uploadCalls, client.launchCalls)
			}
		})
	}
}

func TestServiceLaunchCacheHitRetriesWhenScanReplacesContent(t *testing.T) {
	oldContent := catalog.Content{SHA256: strings.Repeat("a", 64), Size: 3, Extension: "sfc"}
	newContent := catalog.Content{SHA256: strings.Repeat("b", 64), Size: 4, Extension: "sfc"}
	oldGame := serviceGame(oldContent)
	newGame := serviceGame(newContent)
	newGame.Fingerprint.SourceSize = newContent.Size
	newGame.Fingerprint.ModifiedNS++

	store := &fakeServiceCatalog{games: []catalog.Game{oldGame, newGame}}
	store.match = func(_ context.Context, game catalog.Game, _ catalog.Root, content catalog.Content) (bool, error) {
		if store.matchCalls == 1 {
			if game.Fingerprint != oldGame.Fingerprint || content != oldContent {
				t.Fatalf("old snapshot check = game %+v content %+v", game, content)
			}
			return false, nil
		}
		if game.Fingerprint != newGame.Fingerprint || content != newContent {
			t.Fatalf("new snapshot check = game %+v content %+v", game, content)
		}
		return true, nil
	}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.launch = exactLaunchResponse(t, newGame, contentIdentity(newContent))
	service := newTestService(store, &fakeServicePreparer{}, client)

	response, err := service.Launch(context.Background(), oldGame.ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if response.Content != contentIdentity(newContent) {
		t.Fatalf("launched content = %+v, want %+v", response.Content, contentIdentity(newContent))
	}
	if store.gameCalls != 2 || store.matchCalls != 2 || store.updateCalls != 0 || client.probeCalls != 2 || client.launchCalls != 1 {
		t.Fatalf("calls = game:%d match:%d update:%d probe:%d launch:%d", store.gameCalls, store.matchCalls, store.updateCalls, client.probeCalls, client.launchCalls)
	}
}

func TestServiceLaunchAdmissionBlocksFolderReconcileAfterContentValidation(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	launchStarted := make(chan struct{})
	launchReturned := make(chan struct{})
	releaseLaunch := make(chan struct{})
	client.launch = func(ctx context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		close(launchStarted)
		select {
		case <-releaseLaunch:
			response, err := exactLaunchResponse(t, game, contentIdentity(content))(ctx, request)
			close(launchReturned)
			return response, err
		case <-ctx.Done():
			return protocol.CachedLaunchResponse{}, ctx.Err()
		}
	}
	service := newTestService(store, &fakeServicePreparer{}, client)
	scanStarted := make(chan struct{})
	scanAttempted := make(chan struct{})
	scanAdmittedEarly := make(chan struct{}, 1)
	scanner := &fakeServiceScanner{attempted: scanAttempted, started: scanStarted, onAdmitted: func() {
		select {
		case <-launchReturned:
		default:
			scanAdmittedEarly <- struct{}{}
		}
	}}
	scanner.SetAdmissionGate(service.acquireCatalogAdmission)
	service.scanner = scanner

	launchDone := make(chan error, 1)
	go func() {
		_, err := service.Launch(context.Background(), game.ID, nil)
		launchDone <- err
	}()
	<-launchStarted

	reconcileDone := make(chan error, 1)
	go func() {
		_, err := service.ReconcileFolderWatch(context.Background())
		reconcileDone <- err
	}()
	<-scanAttempted

	close(releaseLaunch)
	if err := <-launchDone; err != nil {
		t.Fatalf("Launch: %v", err)
	}
	select {
	case <-scanStarted:
	case <-time.After(time.Second):
		t.Fatal("folder scan did not start after launch admission completed")
	}
	select {
	case <-scanAdmittedEarly:
		t.Fatal("folder scan acquired admission before cached launch returned")
	default:
	}
	if err := <-reconcileDone; err != nil {
		t.Fatalf("ReconcileFolderWatch: %v", err)
	}
}

func TestServiceLaunchAdmissionBlocksExternalCatalogWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite3")
	launchStore, err := catalog.Open(path)
	if err != nil {
		t.Fatalf("Open(launch): %v", err)
	}
	externalStore, err := catalog.Open(path)
	if err != nil {
		_ = launchStore.Close()
		t.Fatalf("Open(external): %v", err)
	}
	defer externalStore.Close()

	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	candidate := catalog.Candidate{
		ID: "snes-synthetic", Title: "Synthetic", RelativePath: "game.sfc",
		System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 123},
	}
	session, err := launchStore.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatalf("BeginRootScan: %v", err)
	}
	if _, err := session.Observe(ctx, candidate); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if _, err := session.Complete(ctx); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	game, err := launchStore.Game(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("Game: %v", err)
	}
	if updated, err := launchStore.CompareAndSetContent(ctx, game, root, content); err != nil || !updated {
		t.Fatalf("CompareAndSetContent = %v, %v, want true, nil", updated, err)
	}
	game, err = launchStore.Game(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("Game(content): %v", err)
	}

	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	launchStarted := make(chan struct{})
	releaseLaunch := make(chan struct{})
	client.launch = func(callCtx context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		close(launchStarted)
		select {
		case <-releaseLaunch:
			return exactLaunchResponse(t, game, contentIdentity(content))(callCtx, request)
		case <-callCtx.Done():
			return protocol.CachedLaunchResponse{}, callCtx.Err()
		}
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: 2 * time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: t.TempDir()}, launchStore, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	defer service.Close()

	launchDone := make(chan error, 1)
	go func() {
		_, err := service.Launch(ctx, game.ID, nil)
		launchDone <- err
	}()
	<-launchStarted

	newContent := catalog.Content{SHA256: strings.Repeat("b", 64), Size: 3, Extension: "sfc"}
	writerDone := make(chan error, 1)
	go func() {
		updated, err := externalStore.CompareAndSetContent(ctx, game, root, newContent)
		if err == nil && !updated {
			err = errors.New("external content update did not match remembered snapshot")
		}
		writerDone <- err
	}()
	select {
	case err := <-writerDone:
		t.Fatalf("external catalog writer completed during target launch: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseLaunch)
	if err := <-launchDone; err != nil {
		t.Fatalf("Launch: %v", err)
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("external catalog writer after launch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("external catalog writer remained blocked after target launch")
	}
}

func TestServiceCatalogAdmissionWaitHonorsCancellation(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	release, err := service.acquireCatalogAdmission(context.Background())
	if err != nil {
		t.Fatalf("acquireCatalogAdmission: %v", err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.acquireCatalogAdmission(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire error = %v, want context.Canceled", err)
	}
}

func TestServiceLaunchProgressCanReenterFolderReconcileBeforeAdmission(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.launch = exactLaunchResponse(t, game, contentIdentity(content))
	service := newTestService(store, &fakeServicePreparer{}, client)
	var reconcileErr error
	var once sync.Once
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := service.Launch(ctx, game.ID, func(progress Progress) {
		if progress.Stage == "launch" {
			once.Do(func() {
				_, reconcileErr = service.ReconcileFolderWatch(ctx)
			})
		}
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if reconcileErr != nil {
		t.Fatalf("reentrant ReconcileFolderWatch: %v", reconcileErr)
	}
}

func TestServiceLaunchRejectsMismatchedCacheHitResponse(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{activeGame: "megadrive-current"}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		other := identity
		other.Size++
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &other}, nil
	}
	service := newTestService(store, &fakeServicePreparer{}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInternal)
	if client.launchCalls != 0 || client.uploadCalls != 0 || client.activeGame != "megadrive-current" {
		t.Fatalf("invalid probe changed target: launch=%d upload=%d active=%q", client.launchCalls, client.uploadCalls, client.activeGame)
	}
}

func TestServiceLaunchRejectsHostileCoreAndLastErrorWithoutReflection(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	privateToken := "synthetic-private-token"
	privatePath := "/private/library/game.sfc"
	tests := []struct {
		name   string
		mutate func(*protocol.CachedLaunchResponse)
	}{
		{name: "missing expected core", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.ExpectedCore = nil }},
		{name: "wrong expected core", mutate: func(response *protocol.CachedLaunchResponse) {
			value := "MegaDrive-" + privateToken + privatePath
			response.Status.ExpectedCore = &value
		}},
		{name: "oversized observed core", mutate: func(response *protocol.CachedLaunchResponse) {
			value := "SNES-" + privateToken + strings.Repeat("x", 64<<10)
			response.Status.ObservedCore = &value
		}},
		{name: "missing observed core", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.ObservedCore = nil }},
		{name: "last error", mutate: func(response *protocol.CachedLaunchResponse) {
			response.Status.LastError = &protocol.APIError{
				Code: protocol.ErrorCode("PRIVATE_" + privateToken), Message: privatePath,
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeServiceCatalog{games: []catalog.Game{game}}
			client := &fakeServiceClient{}
			client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
				return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
			}
			client.launch = func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				gameID, system, coreName := request.GameID, request.System, "SNES"
				response := protocol.CachedLaunchResponse{
					Status: protocol.Status{
						State: protocol.StateActive, GameID: &gameID, System: &system,
						ExpectedCore: &coreName, ObservedCore: &coreName,
					},
					Content: request.Content,
				}
				test.mutate(&response)
				return response, nil
			}
			service := newTestService(store, &fakeServicePreparer{}, client)

			_, err := service.Launch(context.Background(), game.ID, nil)
			assertServiceErrorCode(t, err, protocol.CodeInternal)
			if strings.Contains(err.Error(), privateToken) || strings.Contains(err.Error(), privatePath) {
				t.Fatalf("error reflects hostile response fields: %v", err)
			}
		})
	}
}

func TestServiceLaunchCacheMissRejectsUnavailableSourceBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*catalog.Game)
		code protocol.ErrorCode
	}{
		{name: "offline root", edit: func(game *catalog.Game) { game.RootOnline = false }, code: protocol.CodeSourceUnavailable},
		{name: "missing raw source", edit: func(game *catalog.Game) { game.State = catalog.SourceStateMissing }, code: protocol.CodeSourceUnavailable},
		{name: "invalid raw source", edit: func(game *catalog.Game) { game.State = catalog.SourceStateInvalid }, code: protocol.CodeSourceUnavailable},
		{name: "unreadable archive source", edit: func(game *catalog.Game) {
			game.State = catalog.SourceStateInvalid
			game.Kind = catalog.SourceKindZIP
			game.Reason = "source_unreadable"
		}, code: protocol.CodeSourceUnavailable},
		{name: "invalid archive", edit: func(game *catalog.Game) { game.State = catalog.SourceStateInvalid; game.Kind = catalog.SourceKindZIP }, code: protocol.CodeInvalidArchive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
			game := serviceGame(content)
			test.edit(&game)
			store := &fakeServiceCatalog{games: []catalog.Game{game}}
			preparer := &fakeServicePreparer{}
			client := &fakeServiceClient{activeGame: "megadrive-current"}
			client.probe = absentProbe
			service := newTestService(store, preparer, client)

			_, err := service.Launch(context.Background(), game.ID, nil)
			assertServiceErrorCode(t, err, test.code)
			if client.probeCalls != 1 || preparer.calls != 0 || store.updateCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 {
				t.Fatalf("calls = probe:%d prepare:%d update:%d upload:%d launch:%d", client.probeCalls, preparer.calls, store.updateCalls, client.uploadCalls, client.launchCalls)
			}
			if client.activeGame != "megadrive-current" {
				t.Fatalf("active game = %q, want preserved", client.activeGame)
			}
		})
	}
}

func TestServiceLaunchFirstTransferUsesApprovedOrderAndCleansStaging(t *testing.T) {
	body := []byte("rom")
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: int64(len(body)), Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, body, identity)
	var operations []string
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	store.update = func(_ context.Context, gotGame catalog.Game, _ catalog.Root, gotContent catalog.Content) (bool, error) {
		operations = append(operations, "catalog")
		if gotGame.ID != game.ID || gotGame.Fingerprint != game.Fingerprint || gotGame.Kind != game.Kind {
			t.Fatalf("CAS game = %+v", gotGame)
		}
		if gotContent != (catalog.Content{SHA256: identity.SHA256, Size: identity.Size, Extension: identity.Extension}) {
			t.Fatalf("CAS content = %+v", gotContent)
		}
		return true, nil
	}
	preparer := &fakeServicePreparer{prepare: func(_ context.Context, root catalog.Root, gotGame catalog.Game) (*romsource.Prepared, error) {
		operations = append(operations, "prepare")
		if root.ID != game.LibraryID || gotGame.ID != game.ID {
			t.Fatalf("prepare = root %+v game %+v", root, gotGame)
		}
		return prepared, nil
	}}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, got protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		operations = append(operations, "probe")
		if system != game.System || got != identity {
			t.Fatalf("probe = %q %+v", system, got)
		}
		return protocol.CacheProbeResponse{Present: false}, nil
	}
	client.upload = func(_ context.Context, system protocol.System, got protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, error) {
		operations = append(operations, "upload")
		if system != game.System || got != identity {
			t.Fatalf("upload = %q %+v", system, got)
		}
		uploaded, err := io.ReadAll(reader)
		if err != nil || !reflect.DeepEqual(uploaded, body) {
			t.Fatalf("uploaded = %q, err=%v", uploaded, err)
		}
		return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: got}, nil
	}
	client.launch = func(ctx context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		operations = append(operations, "launch")
		return exactLaunchResponse(t, game, identity)(ctx, request)
	}
	service := newTestService(store, preparer, client)
	var progress []Progress

	response, err := service.Launch(context.Background(), game.ID, func(event Progress) { progress = append(progress, event) })
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if response.Content != identity {
		t.Fatalf("response content = %+v, want %+v", response.Content, identity)
	}
	if want := []string{"prepare", "catalog", "probe", "upload", "launch"}; !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %v, want %v", operations, want)
	}
	assertProgressStages(t, progress, []string{"prepare", "cache", "cache", "upload", "upload-started", "launch"})
	assertPreparedRemoved(t, prepared)
}

func TestProgressReaderStartsAfterBodyBytesAndForwardsClose(t *testing.T) {
	var starts int
	reader := &progressReader{
		Reader:      &scriptedReadCloser{reads: []scriptedRead{{n: 0, err: io.ErrUnexpectedEOF}, {n: 1, err: io.EOF}}},
		onFirstRead: func() { starts++ },
	}
	buffer := make([]byte, 1)
	if _, err := reader.Read(buffer); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("first read error = %v", err)
	}
	if starts != 0 {
		t.Fatalf("upload-started emitted on empty/error read: %d", starts)
	}
	if _, err := reader.Read(buffer); !errors.Is(err, io.EOF) {
		t.Fatalf("second read error = %v", err)
	}
	if starts != 1 {
		t.Fatalf("upload-started count = %d, want 1", starts)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := reader.Reader.(*scriptedReadCloser).closeCount; got != 1 {
		t.Fatalf("underlying reader close count = %d, want 1", got)
	}
}

type scriptedRead struct {
	n   int
	err error
}

type scriptedReadCloser struct {
	reads      []scriptedRead
	closeCount int
}

func (r *scriptedReadCloser) Read([]byte) (int, error) {
	if len(r.reads) == 0 {
		return 0, io.EOF
	}
	next := r.reads[0]
	r.reads = r.reads[1:]
	return next.n, next.err
}

func (r *scriptedReadCloser) Close() error {
	r.closeCount++
	return nil
}

func TestServiceLaunchUploadsPreparedSnapshotAfterSourceReplacement(t *testing.T) {
	original := []byte("synthetic-original")
	replacement := []byte("private-replacement")
	library := t.TempDir()
	sourcePath := filepath.Join(library, "game.sfc")
	if err := os.WriteFile(sourcePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: library}
	game := catalog.Game{
		ID: "snes-synthetic", Title: "Synthetic", LibraryID: root.ID, RelativePath: "game.sfc",
		System: root.System, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
		Fingerprint: catalog.Fingerprint{SourceSize: info.Size(), ModifiedNS: info.ModTime().UnixNano()},
	}
	staging := filepath.Join(t.TempDir(), "staging")
	var prepared *romsource.Prepared
	preparer := &fakeServicePreparer{prepare: func(ctx context.Context, gotRoot catalog.Root, gotGame catalog.Game) (*romsource.Prepared, error) {
		var prepareErr error
		prepared, prepareErr = (romsource.Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(ctx, gotRoot, gotGame)
		if prepareErr != nil {
			return nil, prepareErr
		}
		if prepared.Path != "" {
			t.Fatalf("prepared snapshot exposed path %q", prepared.Path)
		}
		if err := os.WriteFile(sourcePath, replacement, 0o600); err != nil {
			t.Fatalf("replace source content: %v", err)
		}
		return prepared, nil
	}}
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{probe: absentProbe}
	var uploaded []byte
	client.upload = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, error) {
		uploaded, err = io.ReadAll(reader)
		return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: identity}, err
	}
	client.launch = func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		gameID, system, coreName := request.GameID, request.System, "SNES"
		return protocol.CachedLaunchResponse{
			Status: protocol.Status{
				State: protocol.StateActive, GameID: &gameID, System: &system,
				ExpectedCore: &coreName, ObservedCore: &coreName,
			},
			Content: request.Content,
		}, nil
	}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: staging}, store, &fakeServiceScanner{}, preparer, client,
	)

	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !reflect.DeepEqual(uploaded, original) {
		t.Fatalf("uploaded = %q, want held staged identity %q", uploaded, original)
	}
	if file, err := prepared.Open(); err == nil {
		_ = file.Close()
		t.Fatal("prepared snapshot remained openable after service cleanup")
	}
}

func TestServiceLaunchSynchronizesDelayedFailedUploadReadWithCleanup(t *testing.T) {
	body := bytes.Repeat([]byte("stable-upload-snapshot-"), 1<<15)
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: int64(len(body)), Extension: "sfc"}
	prepared := preparedServiceFixture(t, body, identity)
	game := serviceGame(catalog.Content{})
	game.Content = nil
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	preparer := &fakeServicePreparer{prepared: prepared}
	failure := errors.New("synthetic delayed transport failure /private/upload-token")
	transport := &delayedFailingUploadTransport{
		failure: failure,
		started: make(chan struct{}),
		result:  make(chan delayedUploadResult, 1),
	}
	baseURL, err := url.Parse("http://fogcast.invalid")
	if err != nil {
		t.Fatal(err)
	}
	client := host.NewClient(baseURL, "private-token", &http.Client{Transport: transport})
	root := catalog.Root{ID: game.LibraryID, System: game.System, Path: "/private/library"}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, preparer, client,
	)

	_, err = service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeTransferFailed)
	if strings.Contains(err.Error(), "upload-token") || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("error exposed private transport detail: %v", err)
	}

	var result delayedUploadResult
	select {
	case result = <-transport.result:
	case <-time.After(2 * time.Second):
		t.Fatal("delayed upload reader did not stop after service cleanup")
	}
	if !errors.Is(result.readErr, os.ErrClosed) {
		t.Fatalf("delayed body read error = %v, want closed source", result.readErr)
	}
	if result.closeErr != nil {
		t.Fatalf("delayed request body Close: %v", result.closeErr)
	}
	if len(result.data) == 0 || len(result.data) >= len(body) {
		t.Fatalf("delayed transport read %d of %d bytes, want a non-empty partial read", len(result.data), len(body))
	}
	if !bytes.Equal(result.data, body[:len(result.data)]) {
		t.Fatal("delayed transport observed corrupted snapshot bytes")
	}
	assertPreparedRemoved(t, prepared)
}

func TestServiceLaunchPreservesPrimaryFailureWhenCleanupRetainsContent(t *testing.T) {
	privatePath := filepath.Join(t.TempDir(), "private-token-prepared.rom")
	if err := os.WriteFile(privatePath, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	prepared := &romsource.Prepared{Path: privatePath, Content: identity}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{probe: func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{}, context.Canceled
	}}
	service := newTestService(store, &fakeServicePreparer{prepared: prepared}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeMiSTerUnavailable)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error lost primary cancellation: %v", err)
	}
	if !errors.Is(err, romsource.ErrCleanupRetained) {
		t.Fatalf("error lost cleanup-retained signal: %v", err)
	}
	if strings.Contains(err.Error(), privatePath) || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("error reflected private cleanup detail: %v", err)
	}
	if data, readErr := os.ReadFile(privatePath); readErr != nil || string(data) != "rom" {
		t.Fatalf("retained content = %q, err=%v", data, readErr)
	}
}

func TestServiceLaunchSecondProbeHitSkipsUpload(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	preparer := &fakeServicePreparer{prepared: prepared}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, got protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &got}, nil
	}
	client.launch = exactLaunchResponse(t, game, identity)
	service := newTestService(store, preparer, client)

	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.probeCalls != 1 || client.uploadCalls != 0 || client.launchCalls != 1 {
		t.Fatalf("calls = probe:%d upload:%d launch:%d", client.probeCalls, client.uploadCalls, client.launchCalls)
	}
	assertPreparedRemoved(t, prepared)
}

func TestServiceLaunchRemovesPreparedStagingOnEveryLaterFailure(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	tests := []struct {
		name string
		wire func(*fakeServiceCatalog, *fakeServiceClient)
		code protocol.ErrorCode
	}{
		{name: "catalog update", code: protocol.CodeInternal, wire: func(store *fakeServiceCatalog, _ *fakeServiceClient) {
			store.update = func(context.Context, catalog.Game, catalog.Root, catalog.Content) (bool, error) {
				return false, errors.New("database failed")
			}
		}},
		{name: "second probe", code: protocol.CodeMiSTerUnavailable, wire: func(_ *fakeServiceCatalog, client *fakeServiceClient) {
			client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
				return protocol.CacheProbeResponse{}, errors.New("transport failed")
			}
		}},
		{name: "upload", code: protocol.CodeTransferFailed, wire: func(_ *fakeServiceCatalog, client *fakeServiceClient) {
			client.upload = func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error) {
				return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "private upload detail"}
			}
		}},
		{name: "launch", code: protocol.CodeCoreTimeout, wire: func(_ *fakeServiceCatalog, client *fakeServiceClient) {
			client.launch = func(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "private launch detail"}
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			game := serviceGame(catalog.Content{})
			game.Content = nil
			prepared := preparedServiceFixture(t, []byte("rom"), identity)
			store := &fakeServiceCatalog{games: []catalog.Game{game}}
			preparer := &fakeServicePreparer{prepared: prepared}
			client := &fakeServiceClient{probe: absentProbe}
			client.upload = func(_ context.Context, system protocol.System, got protocol.ContentIdentity, _ io.Reader) (protocol.CacheUploadResponse, error) {
				return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: got}, nil
			}
			client.launch = exactLaunchResponse(t, game, identity)
			test.wire(store, client)
			service := newTestService(store, preparer, client)

			_, err := service.Launch(context.Background(), game.ID, nil)
			assertServiceErrorCode(t, err, test.code)
			assertPreparedRemoved(t, prepared)
		})
	}
}

func TestServiceLaunchRejectsMismatchedUploadConfirmation(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{probe: absentProbe}
	client.upload = func(_ context.Context, system protocol.System, got protocol.ContentIdentity, _ io.Reader) (protocol.CacheUploadResponse, error) {
		got.Size++
		return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: got}, nil
	}
	service := newTestService(store, &fakeServicePreparer{prepared: prepared}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInternal)
	if client.launchCalls != 0 {
		t.Fatalf("launch calls = %d, want 0", client.launchCalls)
	}
	assertPreparedRemoved(t, prepared)
}

func TestServiceLaunchStaleFingerprintReloadsAndRetriesOnce(t *testing.T) {
	oldIdentity := protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: 3, Extension: "sfc"}
	newIdentity := protocol.ContentIdentity{SHA256: strings.Repeat("b", 64), Size: 4, Extension: "sfc"}
	oldGame := serviceGame(catalog.Content{})
	oldGame.Content = nil
	newGame := oldGame
	newGame.Fingerprint.ModifiedNS++
	newGame.Fingerprint.SourceSize = 4
	oldPrepared := preparedServiceFixture(t, []byte("old"), oldIdentity)
	newPrepared := preparedServiceFixture(t, []byte("new!"), newIdentity)
	store := &fakeServiceCatalog{games: []catalog.Game{oldGame, newGame}}
	store.update = func(_ context.Context, game catalog.Game, _ catalog.Root, content catalog.Content) (bool, error) {
		if store.updateCalls == 1 {
			if game.Fingerprint != oldGame.Fingerprint || content.SHA256 != oldIdentity.SHA256 {
				t.Fatalf("old CAS = fingerprint %+v content %+v", game.Fingerprint, content)
			}
			return false, nil
		}
		if game.Fingerprint != newGame.Fingerprint || content.SHA256 != newIdentity.SHA256 {
			t.Fatalf("new CAS = fingerprint %+v content %+v", game.Fingerprint, content)
		}
		return true, nil
	}
	preparer := &fakeServicePreparer{prepare: func(_ context.Context, _ catalog.Root, game catalog.Game) (*romsource.Prepared, error) {
		if game.Fingerprint == oldGame.Fingerprint {
			return oldPrepared, nil
		}
		return newPrepared, nil
	}}
	client := &fakeServiceClient{probe: absentProbe}
	client.upload = func(_ context.Context, system protocol.System, content protocol.ContentIdentity, _ io.Reader) (protocol.CacheUploadResponse, error) {
		if content != newIdentity {
			t.Fatalf("uploaded stale identity %+v", content)
		}
		return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: content}, nil
	}
	client.launch = exactLaunchResponse(t, newGame, newIdentity)
	service := newTestService(store, preparer, client)

	if _, err := service.Launch(context.Background(), oldGame.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if store.gameCalls != 2 || store.updateCalls != 2 || preparer.calls != 2 || client.probeCalls != 1 || client.uploadCalls != 1 {
		t.Fatalf("calls = game:%d update:%d prepare:%d probe:%d upload:%d", store.gameCalls, store.updateCalls, preparer.calls, client.probeCalls, client.uploadCalls)
	}
	assertPreparedRemoved(t, oldPrepared)
	assertPreparedRemoved(t, newPrepared)
}

func TestServiceLaunchUnknownIdentityRequiresAvailableSource(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.Content = nil
	game.RootOnline = false
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{activeGame: "megadrive-current"}
	service := newTestService(store, &fakeServicePreparer{}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeSourceUnavailable)
	if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 || client.activeGame != "megadrive-current" {
		t.Fatalf("unavailable unknown identity touched target: probe=%d upload=%d launch=%d active=%q", client.probeCalls, client.uploadCalls, client.launchCalls, client.activeGame)
	}
}

func TestServiceLaunchInitialProbeFailureDoesNotReadOrMutate(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	preparer := &fakeServicePreparer{}
	client := &fakeServiceClient{activeGame: "megadrive-current"}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{}, errors.New("transport failed at /private/library with token-secret")
	}
	service := newTestService(store, preparer, client)
	var progress []Progress

	_, err := service.Launch(context.Background(), game.ID, func(event Progress) { progress = append(progress, event) })
	assertServiceErrorCode(t, err, protocol.CodeMiSTerUnavailable)
	if strings.Contains(err.Error(), "token-secret") || strings.Contains(err.Error(), "/private/library") {
		t.Fatalf("error exposed private configuration: %v", err)
	}
	for _, event := range progress {
		if strings.Contains(event.Message, "token-secret") || strings.Contains(event.Message, "/private/library") {
			t.Fatalf("progress exposed private configuration: %+v", event)
		}
	}
	if preparer.calls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 || client.activeGame != "megadrive-current" {
		t.Fatalf("failure touched source/target: prepare=%d upload=%d launch=%d active=%q", preparer.calls, client.uploadCalls, client.launchCalls, client.activeGame)
	}
}

func TestServiceLaunchCanonicalizesUnknownRemoteErrorCode(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{}, &protocol.APIError{
			Code: protocol.ErrorCode("/private/library token-secret"), Message: "private detail",
		}
	}
	service := newTestService(store, &fakeServicePreparer{}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInternal)
	if strings.Contains(err.Error(), "/private/library") || strings.Contains(err.Error(), "token-secret") || strings.Contains(err.Error(), "private detail") {
		t.Fatalf("unknown remote error was not sanitized: %v", err)
	}
}

func TestServiceLaunchPartialUploadIsNeverBlindlyReplayed(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{probe: absentProbe, activeGame: "megadrive-current"}
	client.upload = func(_ context.Context, _ protocol.System, _ protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, error) {
		one := make([]byte, 1)
		if _, err := io.ReadFull(body, one); err != nil || string(one) != "r" {
			t.Fatalf("partial read = %q, %v", one, err)
		}
		return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "partial transfer at /private/library token-secret"}
	}
	service := newTestService(store, &fakeServicePreparer{prepared: prepared}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeTransferFailed)
	if client.uploadCalls != 1 || client.launchCalls != 0 || client.activeGame != "megadrive-current" {
		t.Fatalf("calls/active = upload:%d launch:%d active:%q", client.uploadCalls, client.launchCalls, client.activeGame)
	}
	assertPreparedRemoved(t, prepared)
}

func TestServiceLaunchMapsOnlyUnknownGameToNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code protocol.ErrorCode
	}{
		{name: "unknown", err: sql.ErrNoRows, code: protocol.CodeROMNotFound},
		{name: "catalog failure", err: errors.New("database unavailable at /private/library token-secret"), code: protocol.CodeInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeServiceCatalog{gameErr: test.err}
			client := &fakeServiceClient{}
			service := newTestService(store, &fakeServicePreparer{}, client)

			_, err := service.Launch(context.Background(), "snes-unknown", nil)
			assertServiceErrorCode(t, err, test.code)
			if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 {
				t.Fatalf("unknown game reached target: probe=%d upload=%d launch=%d", client.probeCalls, client.uploadCalls, client.launchCalls)
			}
		})
	}
}

func TestServiceLaunchRejectsInvalidGameIDBeforeCatalogAccess(t *testing.T) {
	store := &fakeServiceCatalog{games: []catalog.Game{serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})}}
	service := newTestService(store, &fakeServicePreparer{}, &fakeServiceClient{})

	_, err := service.Launch(context.Background(), "../private", nil)
	assertServiceErrorCode(t, err, protocol.CodeBadRequest)
	if store.gameCalls != 0 {
		t.Fatalf("catalog calls = %d, want 0", store.gameCalls)
	}
}

func TestServiceLaunchPreparationFailureIsTypedPrivateAndTargetSafe(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.Content = nil
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	preparer := &fakeServicePreparer{err: &romsource.Error{GameID: game.ID, Code: protocol.CodeInvalidArchive}}
	client := &fakeServiceClient{activeGame: "megadrive-current"}
	service := newTestService(store, preparer, client)
	var progress []Progress

	_, err := service.Launch(context.Background(), game.ID, func(event Progress) { progress = append(progress, event) })
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 || client.activeGame != "megadrive-current" {
		t.Fatalf("preparation failure touched target: probe=%d upload=%d launch=%d active=%q", client.probeCalls, client.uploadCalls, client.launchCalls, client.activeGame)
	}
	assertProgressStages(t, progress, []string{"prepare"})
}

func TestServiceLaunchPreparationFailurePreservesCleanupRetentionAndCancellation(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.Content = nil
	privateDetail := filepath.Join(t.TempDir(), "private-token-retained.rom")
	prepareErr := errors.Join(
		&romsource.Error{GameID: game.ID, Code: protocol.CodeInvalidArchive},
		context.Canceled,
		romsource.ErrCleanupRetained,
		errors.New(privateDetail),
	)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{activeGame: "megadrive-current"}
	service := newTestService(store, &fakeServicePreparer{err: prepareErr}, client)

	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error lost primary cancellation: %v", err)
	}
	if !errors.Is(err, romsource.ErrCleanupRetained) {
		t.Fatalf("error lost cleanup-retained signal: %v", err)
	}
	if strings.Contains(err.Error(), privateDetail) || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("error reflected private preparation detail: %v", err)
	}
	if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 || client.activeGame != "megadrive-current" {
		t.Fatalf("preparation failure touched target: probe=%d upload=%d launch=%d active=%q", client.probeCalls, client.uploadCalls, client.launchCalls, client.activeGame)
	}
}

func TestServiceLaunchHonorsCancellationBeforeCatalogAccess(t *testing.T) {
	store := &fakeServiceCatalog{games: []catalog.Game{serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})}}
	service := newTestService(store, &fakeServicePreparer{}, &fakeServiceClient{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Launch(ctx, "snes-synthetic", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if store.gameCalls != 0 {
		t.Fatalf("catalog calls = %d, want 0", store.gameCalls)
	}
}

func TestThrottledReaderDelaysReads(t *testing.T) {
	reader := &throttledReader{Reader: strings.NewReader("x"), delay: 20 * time.Millisecond}
	started := time.Now()
	buf := make([]byte, 1)
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 15*time.Millisecond {
		t.Fatalf("read completed too quickly: %s", elapsed)
	}
}

func TestServiceLaunchAppliesRequestAndUploadTimeoutsSeparately(t *testing.T) {
	t.Run("probe uses request timeout", func(t *testing.T) {
		content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
		game := serviceGame(content)
		store := &fakeServiceCatalog{games: []catalog.Game{game}}
		client := &fakeServiceClient{}
		client.probe = func(ctx context.Context, _ protocol.System, _ protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
			<-ctx.Done()
			return protocol.CacheProbeResponse{}, ctx.Err()
		}
		service := newTestService(store, &fakeServicePreparer{}, client)
		service.requestTimeout = 10 * time.Millisecond
		service.uploadTimeout = time.Second

		started := time.Now()
		_, err := service.Launch(context.Background(), game.ID, nil)
		assertServiceErrorCode(t, err, protocol.CodeMiSTerUnavailable)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
			t.Fatalf("probe timeout error/duration = %v / %s", err, time.Since(started))
		}
	})

	t.Run("upload uses upload timeout", func(t *testing.T) {
		identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
		game := serviceGame(catalog.Content{})
		game.Content = nil
		prepared := preparedServiceFixture(t, []byte("rom"), identity)
		store := &fakeServiceCatalog{games: []catalog.Game{game}}
		client := &fakeServiceClient{probe: absentProbe}
		client.upload = func(ctx context.Context, _ protocol.System, _ protocol.ContentIdentity, _ io.Reader) (protocol.CacheUploadResponse, error) {
			<-ctx.Done()
			return protocol.CacheUploadResponse{}, ctx.Err()
		}
		service := newTestService(store, &fakeServicePreparer{prepared: prepared}, client)
		service.requestTimeout = time.Second
		service.uploadTimeout = 10 * time.Millisecond

		started := time.Now()
		_, err := service.Launch(context.Background(), game.ID, nil)
		assertServiceErrorCode(t, err, protocol.CodeTransferFailed)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
			t.Fatalf("upload timeout error/duration = %v / %s", err, time.Since(started))
		}
		assertPreparedRemoved(t, prepared)
	})
}

func TestServiceHostOnlyExecutionUsesInjectedResolverAndAdapter(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakeHostExecutor{}
	service := newTestServiceWithExecution(store, &fakeServicePreparer{prepared: prepared}, &fakeServiceClient{}, ExecutionPolicy{
		Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return "host_only", nil }),
		Host:     adapter,
	})

	if got, err := service.SessionExecution(context.Background(), game.ID); err != nil || got != "host_only" {
		t.Fatalf("SessionExecution = %q, %v", got, err)
	}
	response, err := service.Launch(context.Background(), game.ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if response.Status.State != protocol.StateActive || response.Status.GameID == nil || *response.Status.GameID != game.ID {
		t.Fatalf("host launch response = %+v", response)
	}
	if adapter.launchCalls != 1 || adapter.contentPath != "rom" {
		t.Fatalf("host adapter launch calls=%d content=%q", adapter.launchCalls, adapter.contentPath)
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != game.ID {
		t.Fatalf("host status = %+v, %v", status, err)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("host adapter stop calls=%d", adapter.stopCalls)
	}
}

func TestConfiguredExecutionResolverSelectsOnlyConfiguredSystems(t *testing.T) {
	adapter := &fakeHostExecutor{}
	resolver := NewConfiguredExecutionResolver([]protocol.System{protocol.SystemSNES}, adapter)
	game := serviceGame(catalog.Content{})
	if got, err := resolver.Resolve(context.Background(), game); err != nil || got != ExecutionHostOnly {
		t.Fatalf("SNES execution = %q, %v", got, err)
	}
	game.System = protocol.SystemMegaDrive
	if got, err := resolver.Resolve(context.Background(), game); err != nil || got != ExecutionFPGANative {
		t.Fatalf("Mega Drive execution = %q, %v", got, err)
	}
}

func TestConfiguredExecutionResolverFallsBackWithoutAdapter(t *testing.T) {
	resolver := NewConfiguredExecutionResolver([]protocol.System{protocol.SystemSNES}, nil)
	if got, err := resolver.Resolve(context.Background(), serviceGame(catalog.Content{})); err != nil || got != ExecutionFPGANative {
		t.Fatalf("execution = %q, %v", got, err)
	}
}

func TestServiceHostLaunchRetainsOwnershipWhenPreparedCleanupFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepared.sfc")
	if err := os.WriteFile(path, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	prepared := &romsource.Prepared{Path: path, Content: identity}
	adapter := &fakeHostExecutor{}
	service := newTestServiceWithExecution(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{prepared: prepared}, &fakeServiceClient{}, ExecutionPolicy{
		Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
		Host:     adapter,
	})
	if _, err := service.Launch(context.Background(), game.ID, nil); err == nil {
		t.Fatal("cleanup failure was not returned")
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != game.ID {
		t.Fatalf("owned host status = %+v, %v", status, err)
	}
	if adapter.launchCalls != 1 {
		t.Fatalf("launch calls = %d", adapter.launchCalls)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("stop calls = %d", adapter.stopCalls)
	}
}

func TestServiceDefaultExecutionRemainsFPGAAndUsesTargetLaunch(t *testing.T) {
	game := serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.launch = exactLaunchResponse(t, game, contentIdentity(*game.Content))
	service := newTestService(store, &fakeServicePreparer{}, client)
	if got, err := service.SessionExecution(context.Background(), game.ID); err != nil || got != "fpga_native" {
		t.Fatalf("SessionExecution = %q, %v", got, err)
	}
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.launchCalls != 1 {
		t.Fatalf("target launch calls=%d, want 1", client.launchCalls)
	}
}

func TestServiceHostOnlyThenFPGAOnKitStatusStopHitAgent(t *testing.T) {
	hostGame := serviceGame(catalog.Content{})
	hostGame.ID = "snes-host-only"
	hostGame.Content = nil
	fpgaGame := serviceGame(catalog.Content{})
	fpgaGame.ID = "snes-actraiser-test"
	fpgaGame.Title = "ActRaiser"
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	adapter := &fakeHostExecutor{}
	fpgaID, fpgaSystem := fpgaGame.ID, fpgaGame.System
	client := &fakeServiceClient{
		nativeLaunch: exactNativeLaunchResponse(t, fpgaGame, DefaultActRaiserROMPath),
		statusResult: protocol.Status{State: protocol.StateActive, GameID: &fpgaID, System: &fpgaSystem},
		stopResult:   protocol.Status{State: protocol.StateIdle},
	}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath},
		},
		Paths{Staging: "/private/staging"},
		&fakeServiceCatalog{games: []catalog.Game{hostGame, fpgaGame}},
		&fakeServiceScanner{},
		&fakeServicePreparer{prepared: prepared},
		client,
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(_ context.Context, game catalog.Game) (string, error) {
				if game.ID == hostGame.ID {
					return ExecutionHostOnly, nil
				}
				return ExecutionFPGANative, nil
			}),
			Host: adapter,
		}),
	)
	if _, err := service.Launch(context.Background(), hostGame.ID, nil); err != nil {
		t.Fatalf("host Launch: %v", err)
	}
	if adapter.launchCalls != 1 {
		t.Fatalf("host launch calls=%d", adapter.launchCalls)
	}
	if _, err := service.Launch(context.Background(), fpgaGame.ID, nil); err != nil {
		t.Fatalf("FPGA Launch: %v", err)
	}
	if client.nativeLaunchCalls != 1 {
		t.Fatalf("native launch calls=%d", client.nativeLaunchCalls)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("host executor stop calls=%d after FPGA launch, want 1", adapter.stopCalls)
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != fpgaGame.ID {
		t.Fatalf("FPGA status = %+v, %v", status, err)
	}
	if client.statusCalls != 1 || adapter.statusCalls != 0 {
		t.Fatalf("status routing client=%d host=%d", client.statusCalls, adapter.statusCalls)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatalf("FPGA Stop: %v", err)
	}
	if client.stopCalls != 1 {
		t.Fatalf("agent stop calls=%d, want 1", client.stopCalls)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("host executor stop calls=%d after FPGA Stop, want 1 (reconcile only)", adapter.stopCalls)
	}
}

func TestServiceFPGANativeOnKitLaunchUsesV1RequestAndSkipsUpload(t *testing.T) {
	game := serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "bin"})
	game.ID = "megadrive-sonic2"
	game.Title = "Sonic the Hedgehog 2"
	game.System = protocol.SystemMegaDrive
	game.LibraryID = "megadrive-main"
	game.RelativePath = "sonic2.bin"
	game.RootOnline = false
	game.State = catalog.SourceStateMissing
	romPath := "/media/fat/fogcast/cache/sonic2.bin"
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{
		nativeLaunch: exactNativeLaunchResponse(t, game, romPath),
	}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "megadrive-main", System: protocol.SystemMegaDrive, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{game.ID: romPath},
		},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{err: errors.New("source should not be prepared")}, client,
	)
	response, err := service.Launch(context.Background(), game.ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if response.Status.State != protocol.StateActive || response.Status.GameID == nil || *response.Status.GameID != game.ID {
		t.Fatalf("response = %+v", response)
	}
	if client.nativeLaunchCalls != 1 || client.launchCalls != 0 || client.probeCalls != 0 || client.uploadCalls != 0 {
		t.Fatalf("calls native=%d content=%d probe=%d upload=%d", client.nativeLaunchCalls, client.launchCalls, client.probeCalls, client.uploadCalls)
	}
	want := protocol.LaunchRequest{GameID: game.ID, System: protocol.SystemMegaDrive, ROMPath: romPath}
	if client.nativeLaunchReq != want {
		t.Fatalf("native launch = %+v, want %+v", client.nativeLaunchReq, want)
	}
}

func TestServiceSeededActRaiserAliasAcceptsDumpDecorations(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.ID = "snes-actraiser-usa"
	game.Title = "ActRaiser (USA)"
	client := &fakeServiceClient{nativeLaunch: exactNativeLaunchResponse(t, game, DefaultActRaiserROMPath)}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath},
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.nativeLaunchCalls != 1 || client.nativeLaunchReq.ROMPath != DefaultActRaiserROMPath {
		t.Fatalf("native launch calls=%d path=%q", client.nativeLaunchCalls, client.nativeLaunchReq.ROMPath)
	}
}

func TestServiceFPGANativeOnKitLaunchUsesExactGameIDMapping(t *testing.T) {
	game := serviceGame(catalog.Content{})
	romPath := "/media/fat/games/SNES/Exact.smc"
	client := &fakeServiceClient{nativeLaunch: exactNativeLaunchResponse(t, game, romPath)}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{game.ID: romPath},
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.nativeLaunchReq.ROMPath != romPath {
		t.Fatalf("rom_path = %q", client.nativeLaunchReq.ROMPath)
	}
}

func TestSeededActRaiserGameMatchesCanonicalTitleOnly(t *testing.T) {
	cases := []struct {
		title, canonical string
		want             bool
	}{
		{title: "ActRaiser", want: true},
		{title: "actraiser", want: true},
		{title: "ActRaiser (USA)", want: true},
		{title: "ActRaiser (USA) (Rev 1)", want: true},
		{title: "ActRaiser (USA) (Beta)", want: true},
		{canonical: "ActRaiser", title: "ignored dump title", want: true},
		{title: "ActRaiser 2", want: false},
		{title: "ActRaiser 2 (USA)", want: false},
		{title: "ActRaiser II", want: false},
		{canonical: "ActRaiser 2", title: "ActRaiser 2 (USA)", want: false},
		{title: "Synthetic", want: false},
	}
	for _, tc := range cases {
		game := catalog.Game{Title: tc.title, CanonicalTitle: tc.canonical}
		if got := seededActRaiserGame(game); got != tc.want {
			t.Fatalf("seededActRaiserGame(title=%q canonical=%q) = %v, want %v", tc.title, tc.canonical, got, tc.want)
		}
	}
}

func TestServiceSeededActRaiserAliasRejectsSequelTitle(t *testing.T) {
	game := serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})
	game.Title = "ActRaiser 2"
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.launch = exactLaunchResponse(t, game, contentIdentity(*game.Content))
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath},
		},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.launchCalls != 1 || client.nativeLaunchCalls != 0 {
		t.Fatalf("content launchCalls=%d native=%d", client.launchCalls, client.nativeLaunchCalls)
	}
}

func TestServiceUnmappedFPGALaunchKeepsContentPath(t *testing.T) {
	game := serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.launch = exactLaunchResponse(t, game, contentIdentity(*game.Content))
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath},
		},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.launchCalls != 1 || client.nativeLaunchCalls != 0 {
		t.Fatalf("content launchCalls=%d native=%d", client.launchCalls, client.nativeLaunchCalls)
	}
}

func TestServiceFPGANativeOnKitLaunchRejectsMismatchedStatus(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.Title = "ActRaiser"
	client := &fakeServiceClient{
		nativeLaunch: func(context.Context, protocol.LaunchRequest) (protocol.Status, error) {
			return protocol.Status{State: protocol.StateIdle}, nil
		},
	}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath},
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceFPGANativeClientUsesAgentLaunchAndEmptyStop(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.Title = "ActRaiser"
	var launchBody []byte
	var stopBody []byte
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/health":
			_, _ = io.WriteString(w, `{"api_version":"v1","agent_version":"0.1.0","ready":true,"mister_process":true,"command_pipe":true}`)
		case "/v1/status":
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null}`)
		case "/v1/launch":
			launchBody = append([]byte(nil), body...)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-synthetic","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null}`)
		case "/v1/stop":
			stopBody = append([]byte(nil), body...)
			_, _ = io.WriteString(w, `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := host.NewClient(baseURL, "test-token", server.Client())
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
			FPGAROMPaths:   map[string]string{DefaultFPGAROMGameID: DefaultActRaiserROMPath},
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	if _, err := service.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, err := service.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := strings.Join(paths, ","); got != "GET /v1/health,GET /v1/status,GET /v1/kit/lease,POST /v1/launch,GET /v1/status,POST /v1/stop" {
		t.Fatalf("paths = %q", got)
	}
	wantLaunch := `{"game_id":"snes-synthetic","system":"snes","rom_path":"/media/fat/games/SNES/ActRaiser.smc"}`
	if string(launchBody) != wantLaunch {
		t.Fatalf("launch body = %s", launchBody)
	}
	if len(stopBody) != 0 {
		t.Fatalf("stop body = %q", stopBody)
	}
}

func TestServiceCatalogAndV1ControlDelegation(t *testing.T) {
	game := serviceGame(catalog.Content{})
	report := catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "snes-main", Added: 1}}}
	store := &fakeServiceCatalog{games: []catalog.Game{game}, searchGames: []catalog.Game{game}}
	scanner := &fakeServiceScanner{report: report}
	health := protocol.Health{APIVersion: "v1", Ready: true}
	status := protocol.Status{State: protocol.StateActive}
	stopped := protocol.Status{State: protocol.StateIdle}
	client := &fakeServiceClient{healthResult: health, statusResult: status, stopResult: stopped}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{}, store, scanner, &fakeServicePreparer{}, client,
	)
	ctx := context.Background()

	if got, err := service.Scan(ctx); err != nil || !reflect.DeepEqual(got, report) {
		t.Fatalf("Scan = %+v, %v", got, err)
	}
	if got, err := service.Games(ctx); err != nil || !reflect.DeepEqual(got, []catalog.Game{game}) {
		t.Fatalf("Games = %+v, %v", got, err)
	}
	if got, err := service.Game(ctx, game.ID); err != nil || !reflect.DeepEqual(got, game) {
		t.Fatalf("Game = %+v, %v", got, err)
	}
	if got, err := service.Search(ctx, "synthetic"); err != nil || !reflect.DeepEqual(got, []catalog.Game{game}) {
		t.Fatalf("Search = %+v, %v", got, err)
	}
	if store.searchQuery != "synthetic" {
		t.Fatalf("search query = %q", store.searchQuery)
	}
	if got, err := service.QueryGames(ctx, catalog.Query{}); err != nil || !reflect.DeepEqual(got.Games, []catalog.Game{game}) {
		t.Fatalf("QueryGames = %+v, %v", got, err)
	}
	if got, err := service.Platforms(ctx); err != nil || len(got) != 0 {
		t.Fatalf("Platforms = %+v, %v", got, err)
	}
	if client.healthCalls != 0 {
		t.Fatalf("catalog reads contacted target health %d times", client.healthCalls)
	}
	if got, err := service.Health(ctx); err != nil || got != health {
		t.Fatalf("Health = %+v, %v", got, err)
	}
	if got, err := service.Status(ctx); err != nil || !reflect.DeepEqual(got, status) {
		t.Fatalf("Status = %+v, %v", got, err)
	}
	if got, err := service.Stop(ctx); err != nil || !reflect.DeepEqual(got, stopped) {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
	if scanner.calls != 1 || client.healthCalls != 1 || client.statusCalls != 1 || client.stopCalls != 1 {
		t.Fatalf("delegation calls = scan:%d health:%d status:%d stop:%d", scanner.calls, client.healthCalls, client.statusCalls, client.stopCalls)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close(first): %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close(second): %v", err)
	}
	if store.closeCount() != 1 {
		t.Fatalf("catalog close calls = %d, want 1", store.closeCount())
	}
}

func TestServiceOpenComposesCatalogScannerAndAuthenticatedClient(t *testing.T) {
	const token = "synthetic-private-token"
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	library := filepath.Join(dir, "library")
	if err := os.Mkdir(library, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	writeServiceConfig(t, configPath, server.URL, token, library)
	indexParent := filepath.Join(dir, "state")
	staging := filepath.Join(dir, "cache", "staging")
	if err := os.Mkdir(indexParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := Paths{Config: configPath, Index: filepath.Join(indexParent, "library.sqlite3"), Staging: staging}

	service, err := Open(context.Background(), paths, server.Client())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()
	if status, err := service.Status(context.Background()); err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Status = %+v, %v", status, err)
	}
	if requestCount != 1 {
		t.Fatalf("status request count = %d, want 1", requestCount)
	}
	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].RootID != "snes-main" || report.Roots[0].Offline {
		t.Fatalf("scan report = %+v", report)
	}
	if games, err := service.Games(context.Background()); err != nil || len(games) != 1 || !catalog.IsBuiltinPong(games[0]) {
		t.Fatalf("Games = %+v, %v", games, err)
	}
	for _, path := range []string{indexParent, staging} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %q mode = %s, want private 0700", filepath.Base(path), info.Mode())
		}
	}
	indexInfo, err := os.Lstat(paths.Index)
	if err != nil {
		t.Fatal(err)
	}
	if !indexInfo.Mode().IsRegular() || indexInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("index mode = %s, want private regular file", indexInfo.Mode())
	}
}

func TestServiceOpenUsesOperationContextsInsteadOfCallerHTTPClientTimeout(t *testing.T) {
	body := []byte("synthetic-timeout-rom")
	dir := t.TempDir()
	library := filepath.Join(dir, "library")
	if err := os.Mkdir(library, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "game.sfc"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	writeServiceConfig(t, configPath, "http://fogcast.invalid", "synthetic-token", library)
	paths := Paths{
		Config:  configPath,
		Index:   filepath.Join(dir, "state", "library.sqlite3"),
		Staging: filepath.Join(dir, "staging"),
	}
	transport := serviceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/v1/kit/claim":
			return serviceJSONResponse(request, map[string]any{"status": map[string]any{"state": "held", "generation": "test", "expires_at": time.Now().Add(time.Minute), "expires_in_ms": 60000}, "token": "test-lease"})
		case request.URL.Path == "/v1/kit/release":
			return serviceJSONResponse(request, map[string]string{"state": "free"})
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v2/cache/"):
			return serviceJSONResponse(request, protocol.CacheProbeResponse{Present: false})
		case request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/v2/cache/"):
			timer := time.NewTimer(50 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			uploaded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(uploaded, body) {
				t.Errorf("uploaded = %q, want %q", uploaded, body)
			}
			gamesDigest := strings.TrimPrefix(request.URL.Path, "/v2/cache/snes/")
			identity := protocol.ContentIdentity{SHA256: gamesDigest, Size: int64(len(body)), Extension: request.URL.Query().Get("extension")}
			return serviceJSONResponse(request, protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: protocol.SystemSNES, Content: identity})
		case request.Method == http.MethodPost && request.URL.Path == "/v2/launch":
			var launch protocol.CachedLaunchRequest
			if err := json.NewDecoder(request.Body).Decode(&launch); err != nil {
				return nil, err
			}
			gameID, system, coreName := launch.GameID, launch.System, "SNES"
			return serviceJSONResponse(request, protocol.CachedLaunchResponse{
				Status: protocol.Status{
					State: protocol.StateActive, GameID: &gameID, System: &system,
					ExpectedCore: &coreName, ObservedCore: &coreName,
				},
				Content: launch.Content,
			})
		default:
			return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	})
	callerClient := &http.Client{Transport: transport, Timeout: 10 * time.Millisecond}
	service, err := Open(context.Background(), paths, callerClient)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()
	if _, err := service.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	games, err := service.Games(context.Background())
	if err != nil || len(games) != 2 || !catalog.IsBuiltinPong(games[1]) {
		t.Fatalf("Games = %+v, %v", games, err)
	}

	response, err := service.Launch(context.Background(), games[0].ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if response.Status.State != protocol.StateActive || response.Status.GameID == nil || *response.Status.GameID != games[0].ID {
		t.Fatalf("response = %+v", response)
	}
	if callerClient.Timeout != 10*time.Millisecond {
		t.Fatalf("caller HTTP timeout mutated to %s", callerClient.Timeout)
	}
}

func TestServiceLaunchRejectsReconfiguredRootBeforeReadingOrTargetMutation(t *testing.T) {
	bodyA := []byte("root-a-private")
	bodyB := []byte("root-b-private")
	dir := t.TempDir()
	rootA := filepath.Join(dir, "library-a")
	rootB := filepath.Join(dir, "library-b")
	for _, root := range []string{rootA, rootB} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	pathA := filepath.Join(rootA, "game.sfc")
	pathB := filepath.Join(rootB, "game.sfc")
	if err := os.WriteFile(pathA, bodyA, 0o600); err != nil {
		t.Fatal(err)
	}
	infoA, err := os.Stat(pathA)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, bodyB, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pathB, infoA.ModTime(), infoA.ModTime()); err != nil {
		t.Fatal(err)
	}
	infoB, err := os.Stat(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if infoB.Size() != infoA.Size() || infoB.ModTime().UnixNano() != infoA.ModTime().UnixNano() {
		t.Fatalf("root fixtures differ: A=%d/%d B=%d/%d", infoA.Size(), infoA.ModTime().UnixNano(), infoB.Size(), infoB.ModTime().UnixNano())
	}

	configPath := filepath.Join(dir, "config.toml")
	paths := Paths{Config: configPath, Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}
	writeServiceConfig(t, configPath, "http://fogcast.invalid", "synthetic-token", rootA)
	seed, err := Open(context.Background(), paths, nil)
	if err != nil {
		t.Fatalf("Open(root A): %v", err)
	}
	if _, err := seed.Scan(context.Background()); err != nil {
		t.Fatalf("Scan(root A): %v", err)
	}
	games, err := seed.Games(context.Background())
	if err != nil || len(games) != 2 || !catalog.IsBuiltinPong(games[1]) {
		t.Fatalf("Games(root A) = %+v, %v", games, err)
	}
	gameID := games[0].ID
	if err := seed.Close(); err != nil {
		t.Fatalf("Close(root A): %v", err)
	}

	var requestCount int
	var uploaded []byte
	transport := serviceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		switch request.Method {
		case http.MethodGet:
			return serviceJSONResponse(request, protocol.CacheProbeResponse{Present: false})
		case http.MethodPut:
			uploaded, err = io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			digest := strings.TrimPrefix(request.URL.Path, "/v2/cache/snes/")
			identity := protocol.ContentIdentity{SHA256: digest, Size: int64(len(uploaded)), Extension: request.URL.Query().Get("extension")}
			return serviceJSONResponse(request, protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: protocol.SystemSNES, Content: identity})
		case http.MethodPost:
			var launch protocol.CachedLaunchRequest
			if err := json.NewDecoder(request.Body).Decode(&launch); err != nil {
				return nil, err
			}
			gameID, system, expectedCore := launch.GameID, launch.System, "SNES"
			return serviceJSONResponse(request, protocol.CachedLaunchResponse{
				Status: protocol.Status{
					State: protocol.StateActive, GameID: &gameID, System: &system,
					ExpectedCore: &expectedCore, ObservedCore: &expectedCore,
				},
				Content: launch.Content,
			})
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.Method)
		}
	})
	writeServiceConfig(t, configPath, "http://fogcast.invalid", "synthetic-token", rootB)
	service, err := Open(context.Background(), paths, &http.Client{Transport: transport})
	if err != nil {
		t.Fatalf("Open(root B): %v", err)
	}
	defer service.Close()

	_, err = service.Launch(context.Background(), gameID, nil)
	assertServiceErrorCode(t, err, protocol.CodeROMNotFound)
	if requestCount != 0 || len(uploaded) != 0 {
		t.Fatalf("reconfigured root reached target: requests=%d uploaded=%q", requestCount, uploaded)
	}
}

func TestServiceOpenFailureDoesNotExposeConfiguredRootOrToken(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "private-library")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	const token = "private-token-value"
	content := fmt.Sprintf(`base_url = "http://127.0.0.1:8182"
token = %q
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q

[[libraries]]
id = "snes-other"
system = "snes"
root = %q
`, token, root, root)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), Paths{Config: configPath, Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}, nil)
	if err == nil {
		t.Fatal("Open succeeded with duplicate roots")
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), token) {
		t.Fatalf("Open error exposed private config: %v", err)
	}
}

func TestServiceOpenClosesUserLibraryWhenMediaIndexPathIsInvalid(t *testing.T) {
	dir := t.TempDir()
	library := filepath.Join(dir, "library")
	mediaRoot := filepath.Join(dir, "covers")
	if err := os.Mkdir(library, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(mediaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`base_url = "http://127.0.0.1:9"
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q

[[library_media]]
id = "covers-main"
root = %q
`, library, mediaRoot)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	indexParent := filepath.Join(dir, "state")
	if err := os.Mkdir(indexParent, 0o700); err != nil {
		t.Fatal(err)
	}
	userLibrary := filepath.Join(indexParent, "library-user.sqlite3")
	mediaIndex := filepath.Join(indexParent, "media-index")
	if err := os.Mkdir(mediaIndex, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), Paths{
		Config:      configPath,
		Index:       filepath.Join(indexParent, "library.sqlite3"),
		Staging:     filepath.Join(dir, "staging"),
		UserLibrary: userLibrary,
		MediaIndex:  mediaIndex,
	}, nil)
	if err == nil {
		t.Fatal("Open succeeded with a directory media index")
	}

	users, err := libraryuser.OpenContext(context.Background(), userLibrary)
	if err != nil {
		t.Fatalf("reopen user library after failed Open: %v", err)
	}
	defer users.Close()
	if err := users.SetFavorite(context.Background(), "megadrive-sonic-test", true); err != nil {
		t.Fatalf("user library write after failed Open: %v", err)
	}
}

func TestServiceOpenHonorsPreCanceledContextWithoutCreatingState(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{Config: filepath.Join(dir, "missing.toml"), Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Open(ctx, paths, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want context cancellation", err)
	}
	if _, err := os.Lstat(filepath.Dir(paths.Index)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled Open created state: %v", err)
	}
}

func TestValidatePrivateDirectoryPathRejectsFilesystemRoot(t *testing.T) {
	if err := validatePrivateDirectoryPath(string(os.PathSeparator)); err == nil {
		t.Fatal("filesystem root accepted as private state directory")
	}
	if err := validatePrivateDirectoryPath(filepath.Join(t.TempDir(), "state")); err != nil {
		t.Fatalf("temporary state directory rejected: %v", err)
	}
}

type fakeServiceCatalog struct {
	games       []catalog.Game
	searchGames []catalog.Game
	gameErr     error
	gameCalls   int
	matchCalls  int
	updateCalls int
	closeMu     sync.Mutex
	closeCalls  int
	queryErr    error
	searchQuery string
	rootMatch   func(context.Context, catalog.Game, catalog.Root) (bool, error)
	match       func(context.Context, catalog.Game, catalog.Root, catalog.Content) (bool, error)
	update      func(context.Context, catalog.Game, catalog.Root, catalog.Content) (bool, error)
}

func (f *fakeServiceCatalog) Game(context.Context, string) (catalog.Game, error) {
	f.gameCalls++
	if f.gameErr != nil {
		return catalog.Game{}, f.gameErr
	}
	if len(f.games) == 0 {
		return catalog.Game{}, errors.New("missing fake game")
	}
	index := f.gameCalls - 1
	if index >= len(f.games) {
		index = len(f.games) - 1
	}
	return f.games[index], nil
}

func (f *fakeServiceCatalog) Games(context.Context) ([]catalog.Game, error) {
	return append([]catalog.Game(nil), f.games...), nil
}

func (f *fakeServiceCatalog) Search(_ context.Context, query string) ([]catalog.Game, error) {
	f.searchQuery = query
	return append([]catalog.Game(nil), f.searchGames...), nil
}

func (f *fakeServiceCatalog) QueryGames(_ context.Context, query catalog.Query) (catalog.Page, error) {
	if f.queryErr != nil {
		return catalog.Page{}, f.queryErr
	}
	games := f.games
	if query.Text != "" {
		games = f.searchGames
	}
	if query.Restrict {
		allowed := make(map[string]struct{}, len(query.RestrictIDs))
		for _, id := range query.RestrictIDs {
			allowed[id] = struct{}{}
		}
		filtered := make([]catalog.Game, 0, len(games))
		for _, game := range games {
			if _, ok := allowed[game.ID]; ok {
				filtered = append(filtered, game)
			}
		}
		games = filtered
	}
	limit := query.Limit
	if limit <= 0 || limit > len(games) {
		limit = len(games)
	}
	return catalog.Page{Games: append([]catalog.Game(nil), games[:limit]...)}, nil
}

func (f *fakeServiceCatalog) Platforms(context.Context) ([]catalog.PlatformInfo, error) {
	return nil, nil
}

func (f *fakeServiceCatalog) GamesByIDs(_ context.Context, ids []string) ([]catalog.Game, error) {
	byID := make(map[string]catalog.Game, len(f.games))
	for _, game := range f.games {
		byID[game.ID] = game
	}
	games := make([]catalog.Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := byID[id]; ok {
			games = append(games, game)
		}
	}
	return games, nil
}

func (f *fakeServiceCatalog) GameMatchesRoot(ctx context.Context, game catalog.Game, root catalog.Root) (bool, error) {
	if f.rootMatch != nil {
		return f.rootMatch(ctx, game, root)
	}
	return game.LibraryID == root.ID && game.System == root.System, nil
}

func (f *fakeServiceCatalog) ContentMatches(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (bool, error) {
	f.matchCalls++
	if f.match != nil {
		return f.match(ctx, game, root, content)
	}
	return true, nil
}

type fakeContentLaunchAdmission struct {
	matches bool
}

func (a *fakeContentLaunchAdmission) ContentMatches() bool { return a.matches }
func (a *fakeContentLaunchAdmission) Close() error         { return nil }

func (f *fakeServiceCatalog) BeginContentLaunchAdmission(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (catalog.ContentLaunchAdmission, error) {
	matches, err := f.ContentMatches(ctx, game, root, content)
	if err != nil {
		return nil, err
	}
	return &fakeContentLaunchAdmission{matches: matches}, nil
}

func (f *fakeServiceCatalog) CompareAndSetContent(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (bool, error) {
	f.updateCalls++
	if f.update != nil {
		return f.update(ctx, game, root, content)
	}
	return true, nil
}

func (f *fakeServiceCatalog) Close() error {
	f.closeMu.Lock()
	defer f.closeMu.Unlock()
	f.closeCalls++
	return nil
}

func (f *fakeServiceCatalog) closeCount() int {
	f.closeMu.Lock()
	defer f.closeMu.Unlock()
	return f.closeCalls
}

type fakeServiceScanner struct {
	report     catalog.ScanReport
	err        error
	calls      int
	roots      []catalog.Root
	started    chan struct{}
	block      <-chan struct{}
	attempted  chan struct{}
	admit      func(context.Context) (func(), error)
	onAdmitted func()
	once       sync.Once
}

type serviceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f serviceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type delayedUploadResult struct {
	data     []byte
	readErr  error
	closeErr error
}

type delayedFailingUploadTransport struct {
	failure error
	started chan struct{}
	result  chan delayedUploadResult
}

func (t *delayedFailingUploadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.Method {
	case http.MethodGet:
		return serviceJSONResponse(request, protocol.CacheProbeResponse{Present: false})
	case http.MethodPut:
		go func() {
			data := make([]byte, 0, request.ContentLength)
			buffer := make([]byte, 1)
			started := false
			for {
				n, readErr := request.Body.Read(buffer)
				if n > 0 {
					data = append(data, buffer[:n]...)
				}
				if !started {
					close(t.started)
					started = true
				}
				if readErr != nil {
					t.result <- delayedUploadResult{data: data, readErr: readErr, closeErr: request.Body.Close()}
					return
				}
				runtime.Gosched()
			}
		}()
		<-t.started
		return nil, t.failure
	default:
		return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
	}
}

func serviceJSONResponse(request *http.Request, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}, nil
}

func (f *fakeServiceScanner) SetAdmissionGate(admit func(context.Context) (func(), error)) {
	f.admit = admit
}

func (f *fakeServiceScanner) Scan(ctx context.Context, roots []catalog.Root) (catalog.ScanReport, error) {
	f.calls++
	f.roots = append([]catalog.Root(nil), roots...)
	if f.attempted != nil {
		close(f.attempted)
	}
	release := func() {}
	if f.admit != nil {
		var err error
		release, err = f.admit(ctx)
		if err != nil {
			return catalog.ScanReport{}, err
		}
	}
	defer release()
	if f.onAdmitted != nil {
		f.onAdmitted()
	}
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.block != nil {
		<-f.block
	}
	return f.report, f.err
}

type fakeServicePreparer struct {
	prepared *romsource.Prepared
	err      error
	calls    int
	prepare  func(context.Context, catalog.Root, catalog.Game) (*romsource.Prepared, error)
}

func (f *fakeServicePreparer) Prepare(ctx context.Context, root catalog.Root, game catalog.Game) (*romsource.Prepared, error) {
	f.calls++
	if f.prepare != nil {
		return f.prepare(ctx, root, game)
	}
	return f.prepared, f.err
}

type fakeServiceClient struct {
	probe              func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error)
	upload             func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error)
	launch             func(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error)
	nativeLaunch       func(context.Context, protocol.LaunchRequest) (protocol.Status, error)
	developmentLoad    func(context.Context, int64, io.Reader) (protocol.Status, error)
	coreLoad           func(context.Context, int64, io.Reader) (protocol.Status, error)
	developmentReboot  func(context.Context) (protocol.Status, error)
	probeCalls         int
	uploadCalls        int
	launchCalls        int
	nativeLaunchCalls  int
	developmentCalls   int
	coreCalls          int
	developmentReboots int
	developmentSize    int64
	developmentBody    []byte
	nativeLaunchReq    protocol.LaunchRequest
	healthCalls        int
	statusCalls        int
	stopCalls          int
	activeGame         string
	healthResult       protocol.Health
	healthFn           func(context.Context) (protocol.Health, error)
	statusResult       protocol.Status
	statusFn           func(context.Context) (protocol.Status, error)
	stopResult         protocol.Status
	stopFn             func(context.Context) (protocol.Status, error)
	healthErr          error
	statusErr          error
	stopErr            error
}

func TestServiceCorePackageMutatesOnlyAfterTargetAdmission(t *testing.T) {
	payload := []byte("fcore")
	packageStatus := protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 3,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32),
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad", Major: 1}}, Gamepad: true}}
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive},
		coreLoad: func(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
			if size != int64(len(payload)) {
				t.Fatalf("size=%d", size)
			}
			got, _ := io.ReadAll(body)
			if !bytes.Equal(got, payload) {
				t.Fatalf("body=%q", got)
			}
			return packageStatus, nil
		}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution = ExecutionFPGANative
	service.activeGameID = "prior"

	status, err := service.LoadCore(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil || status.CorePackage == nil || status.CorePackage.Generation != 3 || client.coreCalls != 1 {
		t.Fatalf("status=%#v calls=%d error=%v", status, client.coreCalls, err)
	}
	if service.activeExecution != ExecutionFPGADevelopment || service.activeGameID != "" {
		t.Fatalf("execution=%q game=%q", service.activeExecution, service.activeGameID)
	}

	service.activeExecution, service.activeGameID = ExecutionFPGANative, "prior"
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "invalid", Phase: "admission"}
	}
	_, err = service.LoadCore(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err == nil || service.activeExecution != ExecutionFPGANative || service.activeGameID != "prior" || client.stopCalls != 0 {
		t.Fatalf("execution=%q game=%q stops=%d error=%v", service.activeExecution, service.activeGameID, client.stopCalls, err)
	}
}

func TestServiceCorePackageMarksPriorStatusFailureAsPredispatch(t *testing.T) {
	client := &fakeServiceClient{statusErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable,
		Message: "status unavailable", Phase: "recovery"}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution, service.activeGameID = ExecutionHostOnly, "prior-host-game"
	_, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Phase != "request" || client.coreCalls != 0 ||
		service.activeExecution != ExecutionHostOnly || service.activeGameID != "prior-host-game" {
		t.Fatalf("error=%v core calls=%d execution=%q game=%q", err, client.coreCalls, service.activeExecution, service.activeGameID)
	}
}

func TestServiceCorePackageReconcilesLostReplyBeforePublishingExecution(t *testing.T) {
	prior := protocol.Status{State: protocol.StateIdle}
	active := protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 8,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32)}}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, errors.New("lost target HTTP reply")
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return prior, nil
		}
		return active, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	if err != nil || status.CorePackage == nil || status.CorePackage.Generation != 8 ||
		service.activeExecution != ExecutionFPGADevelopment || client.coreCalls != 1 || client.statusCalls < 2 {
		t.Fatalf("status=%+v error=%v execution=%q core=%d status=%d", status, err, service.activeExecution, client.coreCalls, client.statusCalls)
	}
}

func TestServiceCorePackageLostReplyReconciliationHasOverallDeadline(t *testing.T) {
	prior := protocol.Status{State: protocol.StateIdle}
	launching := protocol.Status{State: protocol.StateLaunching, Development: true}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, errors.New("lost target HTTP reply")
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return prior, nil
		}
		return launching, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.coreLoadReconcileTimeout = 40 * time.Millisecond
	started := time.Now()
	_, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	elapsed := time.Since(started)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" ||
		elapsed > 500*time.Millisecond || client.statusCalls < 2 {
		t.Fatalf("error=%v elapsed=%s status calls=%d", err, elapsed, client.statusCalls)
	}
}

func TestServiceCorePackagePreservesConfirmedIdleFailureEvidence(t *testing.T) {
	loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567"}
	prior := protocol.Status{State: protocol.StateIdle}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, loadErr
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return prior, nil
		}
		return protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{
			Code: loadErr.Code, Message: "public identity mismatch", Phase: loadErr.Phase,
			Expected: loadErr.Expected, Observed: loadErr.Observed,
		}}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution, service.activeTarget = ExecutionFPGANative, "dev"
	service.activeGameID, service.activeSystem = "prior-game", protocol.SystemSNES
	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != loadErr.Code || apiErr.Phase != loadErr.Phase ||
		apiErr.Expected != loadErr.Expected || apiErr.Observed != loadErr.Observed ||
		status.State != protocol.StateIdle || status.LastError == nil || client.coreCalls != 1 || client.statusCalls != 2 ||
		service.activeExecution != "" || service.activeTarget != "" || service.activeGameID != "" || service.activeSystem != "" ||
		!service.selectedTargetReconciled || service.selectedTargetRepairAllowed {
		t.Fatalf("status=%+v error=%v core=%d status_calls=%d execution=%q target=%q game=%q system=%q reconciled=%t repair=%t",
			status, err, client.coreCalls, client.statusCalls, service.activeExecution, service.activeTarget,
			service.activeGameID, service.activeSystem, service.selectedTargetReconciled, service.selectedTargetRepairAllowed)
	}
}

func TestServiceCorePackageConfirmedIdleStopsPriorHostOnlyExecution(t *testing.T) {
	for _, test := range []struct {
		name       string
		stopErr    error
		wantCode   protocol.ErrorCode
		wantPhase  string
		wantActive string
	}{
		{name: "stopped", wantCode: protocol.CodeUnrecognizedCore, wantPhase: "identity"},
		{name: "cleanup failure", stopErr: errors.New("host stop failed"), wantCode: protocol.CodeInternal,
			wantPhase: "recovery", wantActive: ExecutionHostOnly},
	} {
		t.Run(test.name, func(t *testing.T) {
			loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
				Expected: "0123", Observed: "4567"}
			idleFailure := protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{
				Code: loadErr.Code, Message: "public identity mismatch", Phase: loadErr.Phase,
				Expected: loadErr.Expected, Observed: loadErr.Observed,
			}}
			client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
				return protocol.Status{}, loadErr
			}}
			client.statusFn = func(context.Context) (protocol.Status, error) {
				if client.statusCalls == 1 {
					return protocol.Status{State: protocol.StateIdle}, nil
				}
				return idleFailure, nil
			}
			hostExecutor := &fakeHostExecutor{stopErr: test.stopErr}
			service := newTestServiceWithExecution(&fakeServiceCatalog{}, &fakeServicePreparer{}, client,
				ExecutionPolicy{Host: hostExecutor})
			service.activeExecution, service.activeTarget = ExecutionHostOnly, "host"
			service.activeGameID, service.activeSystem = "prior-host-game", protocol.SystemSNES

			status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != test.wantCode || apiErr.Phase != test.wantPhase ||
				status.State != protocol.StateIdle || hostExecutor.stopCalls != 1 || service.activeExecution != test.wantActive ||
				!service.selectedTargetReconciled || service.selectedTargetRepairAllowed {
				t.Fatalf("status=%+v error=%v stops=%d execution=%q target=%q game=%q system=%q reconciled=%t repair=%t",
					status, err, hostExecutor.stopCalls, service.activeExecution, service.activeTarget,
					service.activeGameID, service.activeSystem, service.selectedTargetReconciled, service.selectedTargetRepairAllowed)
			}
			if test.wantActive == "" && (service.activeTarget != "" || service.activeGameID != "" || service.activeSystem != "") {
				t.Fatalf("retired host ownership target=%q game=%q system=%q", service.activeTarget, service.activeGameID, service.activeSystem)
			}
			if test.wantActive != "" && (service.activeTarget != "host" || service.activeGameID != "prior-host-game" || service.activeSystem != protocol.SystemSNES) {
				t.Fatalf("lost host ownership target=%q game=%q system=%q", service.activeTarget, service.activeGameID, service.activeSystem)
			}
		})
	}
}

func TestServiceCorePackagePreservesRepeatedConfirmedIdleFailureEvidence(t *testing.T) {
	loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567"}
	retained := &protocol.APIError{Code: loadErr.Code, Message: "public identity mismatch", Phase: loadErr.Phase,
		Expected: loadErr.Expected, Observed: loadErr.Observed}
	prior := protocol.Status{State: protocol.StateIdle, LastError: retained}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, loadErr
	}, statusResult: prior}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != loadErr.Code || apiErr.Phase != loadErr.Phase ||
		apiErr.Expected != loadErr.Expected || apiErr.Observed != loadErr.Observed || status.State != protocol.StateIdle ||
		status.LastError == nil || client.coreCalls != 1 || client.statusCalls != 2 {
		t.Fatalf("status=%+v error=%v core=%d status_calls=%d", status, err, client.coreCalls, client.statusCalls)
	}
}

func TestServiceCorePackageRejectsMismatchedIdleFailureEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*protocol.APIError)
	}{
		{name: "code", mutate: func(err *protocol.APIError) { err.Code = protocol.CodeMiSTerUnavailable }},
		{name: "phase", mutate: func(err *protocol.APIError) { err.Phase = "video" }},
		{name: "expected", mutate: func(err *protocol.APIError) { err.Expected = "different" }},
		{name: "observed", mutate: func(err *protocol.APIError) { err.Observed = "different" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
				Expected: "0123", Observed: "4567"}
			retained := *loadErr
			test.mutate(&retained)
			client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
				return protocol.Status{}, loadErr
			}}
			client.statusFn = func(context.Context) (protocol.Status, error) {
				if client.statusCalls == 1 {
					return protocol.Status{State: protocol.StateIdle}, nil
				}
				return protocol.Status{State: protocol.StateIdle, LastError: &retained}, nil
			}
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
			status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" ||
				status != (protocol.Status{}) || client.coreCalls != 1 || client.statusCalls != 2 {
				t.Fatalf("status=%+v error=%v core=%d status_calls=%d", status, err, client.coreCalls, client.statusCalls)
			}
		})
	}
}

func TestServiceCorePackageDoesNotTreatAmbiguousTransferAsConfirmedIdleFailure(t *testing.T) {
	loadAPIError := &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "target reply lost", Phase: "transfer"}
	loadErr := &ambiguousPackageLoadError{cause: loadAPIError}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, loadErr
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{
			Code: loadAPIError.Code, Message: "retained transfer failure", Phase: loadAPIError.Phase,
		}}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" ||
		status != (protocol.Status{}) || client.coreCalls != 1 || client.statusCalls != 2 {
		t.Fatalf("status=%+v error=%v core=%d status_calls=%d", status, err, client.coreCalls, client.statusCalls)
	}
}

func TestServiceCorePackagePublishesTargetBeforeHostCleanupFailure(t *testing.T) {
	active := protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 8,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32)}}
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle},
		coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) { return active, nil }}
	hostExecutor := &fakeHostExecutor{stopErr: errors.New("host stop failed")}
	service := newTestServiceWithExecution(&fakeServiceCatalog{}, &fakeServicePreparer{}, client, ExecutionPolicy{Host: hostExecutor})
	service.activeExecution, service.activeGameID = ExecutionHostOnly, "prior-host-game"
	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	if err == nil || status.CorePackage == nil || service.activeExecution != ExecutionFPGADevelopment || service.activeGameID != "" {
		t.Fatalf("status=%+v error=%v execution=%q game=%q", status, err, service.activeExecution, service.activeGameID)
	}
}

func TestServiceDevelopmentRBFUsesSelectedTargetAndStops(t *testing.T) {
	payload := []byte("development-rbf")
	observed := "DEVCORE"
	client := &fakeServiceClient{
		developmentLoad: func(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
			if size != int64(len(payload)) {
				t.Fatalf("development RBF size = %d", size)
			}
			got, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("development RBF body = %q", got)
			}
			return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}, nil
		},
		statusResult: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
	}
	client.developmentReboot = func(context.Context) (protocol.Status, error) {
		client.statusResult = protocol.Status{State: protocol.StateIdle}
		return protocol.Status{}, io.EOF
	}
	healthChecks := 0
	client.healthFn = func(context.Context) (protocol.Health, error) {
		healthChecks++
		if healthChecks < 3 {
			return protocol.Health{Ready: true, BootID: "boot-before"}, nil
		}
		return protocol.Health{Ready: true, BootID: "boot-after"}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.LoadDevelopmentRBF(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateActive || !status.Development {
		t.Fatalf("development load status = %+v", status)
	}
	if service.activeExecution != ExecutionFPGADevelopment || service.activeGameID != "" || service.activeSystem != "" {
		t.Fatalf("active development execution = %q game = %q system = %q", service.activeExecution, service.activeGameID, service.activeSystem)
	}

	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateActive || !status.Development {
		t.Fatalf("development status = %+v", status)
	}
	status, err = service.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateIdle || client.stopCalls != 1 || client.developmentReboots != 1 || healthChecks < 3 || service.activeExecution != "" {
		t.Fatalf("stop status = %+v calls = %d reboots = %d health checks = %d execution = %q", status, client.stopCalls, client.developmentReboots, healthChecks, service.activeExecution)
	}
}

func TestServiceDevelopmentStopUsesNativeRecoveryStatusOverHTTP(t *testing.T) {
	var stopCalls, rebootCalls, healthCalls, statusCalls int
	rebooted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" && r.Header.Get("Authorization") != "Bearer target-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/stop":
			stopCalls++
			_ = json.NewEncoder(w).Encode(protocol.Status{
				State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/development/reboot":
			rebootCalls++
			rebooted = true
			_ = json.NewEncoder(w).Encode(protocol.Status{
				State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/health":
			healthCalls++
			bootID := "boot-before"
			if rebooted {
				bootID = "boot-after"
			}
			_ = json.NewEncoder(w).Encode(protocol.Health{Ready: true, BootID: bootID})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
			statusCalls++
			if !rebooted {
				_ = json.NewEncoder(w).Encode(protocol.Status{
					State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := host.NewClient(baseURL, "target-token", server.Client())
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution = ExecutionFPGADevelopment

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("HTTP native recovery stop = %#v, %v", status, err)
	}
	if stopCalls != 1 || rebootCalls != 1 || healthCalls < 2 || statusCalls != 1 || service.activeExecution != "" {
		t.Fatalf("HTTP native recovery calls = stop:%d reboot:%d health:%d status:%d execution:%q", stopCalls, rebootCalls, healthCalls, statusCalls, service.activeExecution)
	}
}

type noDevelopmentRecoveryClient struct {
	serviceClient
}

func TestServiceDevelopmentStopRejectsTargetWithoutRecoveryCapability(t *testing.T) {
	target := &fakeServiceClient{
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &noDevelopmentRecoveryClient{serviceClient: target})
	service.activeExecution = ExecutionFPGADevelopment

	_, err := service.Stop(context.Background())
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("missing recovery capability error = %v", err)
	}
	if target.stopCalls != 1 || target.developmentReboots != 0 || service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("missing recovery calls = stop:%d reboot:%d execution:%q", target.stopCalls, target.developmentReboots, service.activeExecution)
	}
}

func TestServiceDevelopmentStopBoundsFailedRecoveryHandshake(t *testing.T) {
	target := &fakeServiceClient{
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
		developmentReboot: func(context.Context) (protocol.Status, error) {
			return protocol.Status{}, errors.New("reboot command failed")
		},
		healthFn: func(context.Context) (protocol.Health, error) {
			return protocol.Health{Ready: true, BootID: "boot-before"}, nil
		},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, target)
	service.activeExecution = ExecutionFPGADevelopment
	service.uploadTimeout = 50 * time.Millisecond

	_, err := service.Stop(context.Background())
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("failed recovery error = %v", err)
	}
	if target.stopCalls != 1 || target.developmentReboots != 1 || service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("failed recovery calls = stop:%d reboot:%d execution:%q", target.stopCalls, target.developmentReboots, service.activeExecution)
	}
}

func TestServiceDevelopmentRBFLostResponseUsesStatusOnlyOnce(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     protocol.Status
		cancelLoop bool
		wantCode   protocol.ErrorCode
	}{
		{name: "active development", status: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: serviceStringPtr("DEVCORE")}},
		{name: "retained operation error", status: protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "runtime operation failed"}}, wantCode: protocol.CodeCoreTimeout},
		{name: "wrong terminal state", status: protocol.Status{State: protocol.StateActive, ObservedCore: serviceStringPtr("MegaDrive")}, wantCode: protocol.CodeMiSTerUnavailable},
		{name: "caller cancellation", status: protocol.Status{State: protocol.StateIdle}, cancelLoop: true, wantCode: protocol.CodeMiSTerUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			uploads, statuses := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/development/rbf":
					_, _ = io.ReadAll(r.Body)
					mu.Lock()
					uploads++
					mu.Unlock()
					<-r.Context().Done()
				case "/v1/status":
					mu.Lock()
					statuses++
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(test.status)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			baseURL, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := host.NewClient(baseURL, "test-token", server.Client())
			service := newService(
				Config{RequestTimeout: 20 * time.Millisecond, UploadTimeout: 30 * time.Millisecond},
				Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
			)
			parent, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			if test.cancelLoop {
				cancel()
				parent, cancel = context.WithTimeout(context.Background(), 90*time.Millisecond)
			}
			defer cancel()
			status, loadErr := service.LoadDevelopmentRBF(parent, 3, strings.NewReader("rbf"))
			mu.Lock()
			gotUploads, gotStatuses := uploads, statuses
			mu.Unlock()
			if gotUploads != 1 {
				t.Fatalf("development uploads = %d, want exactly one", gotUploads)
			}
			if test.wantCode == "" {
				if loadErr != nil || status.State != protocol.StateActive || !status.Development || gotStatuses == 0 {
					t.Fatalf("status=%+v err=%v status calls=%d", status, loadErr, gotStatuses)
				}
				return
			}
			var apiErr *protocol.APIError
			if !errors.As(loadErr, &apiErr) || apiErr.Code != test.wantCode || gotStatuses == 0 {
				t.Fatalf("error=%v status calls=%d, want %s after observation", loadErr, gotStatuses, test.wantCode)
			}
		})
	}
}

func serviceStringPtr(value string) *string { return &value }

func TestServiceDevelopmentRBFLostResponseBoundsEachStatusCall(t *testing.T) {
	var mu sync.Mutex
	uploads, statuses := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/development/rbf":
			_, _ = io.ReadAll(r.Body)
			mu.Lock()
			uploads++
			mu.Unlock()
			<-r.Context().Done()
		case "/v1/status":
			mu.Lock()
			statuses++
			mu.Unlock()
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	service := newService(
		Config{RequestTimeout: 25 * time.Millisecond, UploadTimeout: 30 * time.Millisecond},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, host.NewClient(baseURL, "test-token", server.Client()),
	)
	parent, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := service.LoadDevelopmentRBF(parent, 3, strings.NewReader("rbf"))
	elapsed := time.Since(started)
	mu.Lock()
	gotUploads, gotStatuses := uploads, statuses
	mu.Unlock()
	if err == nil || gotUploads != 1 || gotStatuses != 1 || elapsed < 45*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("err=%v uploads=%d statuses=%d elapsed=%s", err, gotUploads, gotStatuses, elapsed)
	}
}

func TestServiceRejectsCatalogLaunchWhileDevelopmentRBFIsActive(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.activeExecution = ExecutionFPGADevelopment

	_, err := service.Launch(context.Background(), "snes-replacement", nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBusy {
		t.Fatalf("launch error = %v", err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("active execution = %q", service.activeExecution)
	}
}

func TestServiceDevelopmentActiveReconstructsAfterHostRestart(t *testing.T) {
	observed := "DEVCORE"
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	active, err := service.DevelopmentActive(context.Background())
	if err != nil || !active {
		t.Fatalf("development active = %t, %v", active, err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("reconstructed execution = %q", service.activeExecution)
	}
}

func TestServiceDevelopmentSessionStateReconstructsNativeGameAfterHostRestart(t *testing.T) {
	gameID, system, core := "megadrive-reconstructed", protocol.SystemMegaDrive, "MegaDrive"
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core,
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	development, execution, err := service.DevelopmentSessionState(context.Background())
	if err != nil || development || execution != ExecutionFPGANative {
		t.Fatalf("development=%t execution=%q err=%v", development, execution, err)
	}
	if client.statusCalls != 1 || service.activeExecution != ExecutionFPGANative {
		t.Fatalf("status calls=%d active execution=%q", client.statusCalls, service.activeExecution)
	}
}

func TestServiceDevelopmentActiveReconstructsStoppingRecoveryAfterHostRestart(t *testing.T) {
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	active, err := service.DevelopmentActive(context.Background())
	if err != nil || !active {
		t.Fatalf("development active = %t, %v", active, err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("reconstructed execution = %q", service.activeExecution)
	}
}

func TestNamedTargetSelectionRoutesPlayStatusAndStop(t *testing.T) {
	ctx := context.Background()
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}
	game := catalog.Game{
		ID: "snes-target-route", Title: "Target Route", System: protocol.SystemSNES, LibraryID: root.ID,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	dev := &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateIdle},
		nativeLaunch: func(context.Context, protocol.LaunchRequest) (protocol.Status, error) {
			return protocol.Status{}, errors.New("dev should not launch")
		},
	}
	spare := &fakeServiceClient{
		nativeLaunch: func(_ context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
			expected := "SNES"
			return protocol.Status{State: protocol.StateActive, GameID: &request.GameID, System: &request.System, ExpectedCore: &expected, ObservedCore: &expected}, nil
		},
		statusResult: protocol.Status{State: protocol.StateActive, GameID: &game.ID, System: &game.System},
		stopResult:   protocol.Status{State: protocol.StateIdle},
	}
	service := newService(
		Config{
			Libraries: []catalog.Root{root},
			Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
			FPGAROMPaths: map[string]string{game.ID: "/media/fat/games/SNES/TargetRoute.smc"},
		},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile selected target: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}, Libraries: []catalog.Root{root},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token", AgentSet: true},
		},
		SelectedTarget: "spare",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Launch(ctx, game.ID, nil); err != nil {
		t.Fatal(err)
	}
	if spare.nativeLaunchCalls != 1 || dev.nativeLaunchCalls != 0 {
		t.Fatalf("launch calls dev=%d spare=%d", dev.nativeLaunchCalls, spare.nativeLaunchCalls)
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if spare.statusCalls != 1 || dev.statusCalls != 1 {
		t.Fatalf("status calls dev=%d spare=%d", dev.statusCalls, spare.statusCalls)
	}
	changedActiveConnection := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}, Libraries: []catalog.Root{root},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.99:8182", Agent: "replacement-test-token", AgentSet: true},
		},
		SelectedTarget: "spare",
	})
	var apiErr *protocol.APIError
	if !errors.As(changedActiveConnection, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("active connection edit error = %v", changedActiveConnection)
	}
	blocked := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}, Libraries: []catalog.Root{root},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "dev",
	})
	apiErr = nil
	if !errors.As(blocked, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("active target switch error = %v", blocked)
	}
	if _, err := service.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if spare.stopCalls != 1 || dev.stopCalls != 0 {
		t.Fatalf("stop calls dev=%d spare=%d", dev.stopCalls, spare.stopCalls)
	}
}

func TestStartupReconciliationBlocksNamedTargetSwitchBeforeStatus(t *testing.T) {
	ctx := context.Background()
	gameID := "snes-startup-active"
	system := protocol.SystemSNES
	dev := &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system},
		stopResult:   protocol.Status{State: protocol.StateIdle},
	}
	spare := &fakeServiceClient{}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "spare",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("unreconciled target switch error = %v", err)
	}
	if dev.statusCalls != 0 || service.LibrarySettings().SelectedTarget != "dev" {
		t.Fatalf("rejected switch queried or changed target: status=%d selected=%q", dev.statusCalls, service.LibrarySettings().SelectedTarget)
	}
	status, err := service.Status(ctx)
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("startup status = %+v, %v", status, err)
	}
	if _, err := service.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if dev.stopCalls != 1 || spare.stopCalls != 0 {
		t.Fatalf("stop calls dev=%d spare=%d", dev.stopCalls, spare.stopCalls)
	}
}

func TestIdleStartupReconciliationAllowsOneNamedTargetSwitch(t *testing.T) {
	ctx := context.Background()
	dev := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	spare := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	settingsFor := func(selected string) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: selected,
		}
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("spare")); err == nil {
		t.Fatal("unreconciled startup switch succeeded")
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("spare")); err != nil {
		t.Fatalf("idle-reconciled switch: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("dev")); err == nil {
		t.Fatal("new selected target switched again without reconciliation")
	}
	if dev.statusCalls != 1 || spare.statusCalls != 0 {
		t.Fatalf("status calls dev=%d spare=%d", dev.statusCalls, spare.statusCalls)
	}
}

func TestFailedStatusDoesNotAllowNamedTargetRecoverySwitch(t *testing.T) {
	ctx := context.Background()
	statusCalls := 0
	dev := &fakeServiceClient{statusFn: func(context.Context) (protocol.Status, error) {
		statusCalls++
		if statusCalls == 1 {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{}, errors.New("fixture target unavailable")
	}}
	spare := &fakeServiceClient{}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	settingsFor := func(selected string) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: selected,
		}
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile dev: %v", err)
	}
	if _, err := service.Status(ctx); err == nil {
		t.Fatal("later unreachable dev status succeeded")
	} else {
		var apiErr *protocol.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("unreachable dev status error = %v", err)
		}
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("spare")); err == nil {
		t.Fatal("unreachable target switched without successful reconciliation")
	}
	if dev.statusCalls != 2 || spare.statusCalls != 0 || service.LibrarySettings().SelectedTarget != "dev" {
		t.Fatalf("status calls dev=%d spare=%d selected=%q", dev.statusCalls, spare.statusCalls, service.LibrarySettings().SelectedTarget)
	}
}

func TestFailedStartupStatusAllowsOneSelectedTargetConnectionRepair(t *testing.T) {
	ctx := context.Background()
	unreachable := &fakeServiceClient{statusErr: errors.New("fixture target unavailable")}
	repaired := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, unreachable,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Address == "http://192.0.2.20:8182" {
				return repaired, nil
			}
			return unreachable, nil
		}),
	)
	settingsFor := func(address string) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets:            []TargetConfig{{Name: "dev", Enabled: true, Address: address}},
			SelectedTarget:     "dev",
		}
	}
	if _, err := service.Status(ctx); err == nil {
		t.Fatal("unreachable startup status succeeded")
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("http://192.0.2.20:8182")); err != nil {
		t.Fatalf("repair selected target connection: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("http://192.0.2.21:8182")); err == nil {
		t.Fatal("second connection repair succeeded without reconciling repaired target")
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("status through repaired connection: %v", err)
	}
	if unreachable.statusCalls != 1 || repaired.statusCalls != 1 {
		t.Fatalf("status calls unreachable=%d repaired=%d", unreachable.statusCalls, repaired.statusCalls)
	}
}

func TestFailedStatusDoesNotAllowRecoveryFromKnownActiveExecution(t *testing.T) {
	ctx := context.Background()
	dev := &fakeServiceClient{statusErr: errors.New("fixture target unavailable")}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(TargetConfig) (serviceClient, error) { return &fakeServiceClient{}, nil }),
	)
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "dev"
	service.executionMu.Unlock()
	if _, err := service.Status(ctx); err == nil {
		t.Fatal("unreachable active target status succeeded")
	}
	service.executionMu.Lock()
	repairAllowed := service.selectedTargetRepairAllowed
	service.executionMu.Unlock()
	if repairAllowed {
		t.Fatal("failed status granted repair while an execution was active")
	}
	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "spare",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("active target recovery switch error = %v", err)
	}
	if service.LibrarySettings().SelectedTarget != "dev" {
		t.Fatalf("active target changed to %q", service.LibrarySettings().SelectedTarget)
	}
}

func TestStatusReconciliationPinsActiveNamedTargetBeforeSettingsSwitch(t *testing.T) {
	ctx := context.Background()
	gameID := "snes-reconciled"
	system := protocol.SystemSNES
	statusEntered := make(chan struct{})
	statusRelease := make(chan struct{})
	dev := &fakeServiceClient{
		statusFn: func(context.Context) (protocol.Status, error) {
			close(statusEntered)
			<-statusRelease
			return protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}, nil
		},
		stopResult: protocol.Status{State: protocol.StateIdle},
	}
	spare := &fakeServiceClient{}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	statusDone := make(chan error, 1)
	go func() {
		status, err := service.Status(ctx)
		if err == nil && status.State != protocol.StateActive {
			err = errors.New("reconciled status was not active")
		}
		statusDone <- err
	}()
	<-statusEntered
	settingsDone := make(chan error, 1)
	go func() {
		settingsDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: "spare",
		})
	}()
	select {
	case err := <-settingsDone:
		t.Fatalf("settings switch completed before status reconciliation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(statusRelease)
	if err := <-statusDone; err != nil {
		t.Fatal(err)
	}
	err := <-settingsDone
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("active reconciled target switch error = %v", err)
	}
	if _, err := service.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if dev.stopCalls != 1 || spare.stopCalls != 0 {
		t.Fatalf("stop calls dev=%d spare=%d", dev.stopCalls, spare.stopCalls)
	}
}

func TestStopPinsReconciledNamedTargetThroughSettingsSwitch(t *testing.T) {
	ctx := context.Background()
	stopEntered := make(chan struct{})
	stopRelease := make(chan struct{})
	dev := &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateIdle},
		stopFn: func(context.Context) (protocol.Status, error) {
			close(stopEntered)
			<-stopRelease
			return protocol.Status{State: protocol.StateIdle}, nil
		},
	}
	spare := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile dev idle: %v", err)
	}

	stopDone := make(chan error, 1)
	go func() {
		_, err := service.Stop(ctx)
		stopDone <- err
	}()
	<-stopEntered
	settingsDone := make(chan error, 1)
	go func() {
		settingsDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: "spare",
		})
	}()
	select {
	case err := <-settingsDone:
		t.Fatalf("settings switch completed before Stop reconciliation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(stopRelease)
	if err := <-stopDone; err != nil {
		t.Fatalf("stop dev: %v", err)
	}
	if err := <-settingsDone; err != nil {
		t.Fatalf("switch to spare: %v", err)
	}

	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "dev",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("unreconciled spare switch error = %v", err)
	}
	if spare.statusCalls != 0 {
		t.Fatalf("spare status calls = %d, want 0", spare.statusCalls)
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile spare idle: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "dev",
	}); err != nil {
		t.Fatalf("switch after spare reconciliation: %v", err)
	}
}

func TestIdleStatusCannotClearNewerNamedTargetLaunch(t *testing.T) {
	ctx := context.Background()
	root := catalog.Root{ID: "snes-status-race", System: protocol.SystemSNES, Path: t.TempDir()}
	game := catalog.Game{
		ID: "snes-status-race", Title: "Status Race", System: protocol.SystemSNES, LibraryID: root.ID,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	statusEntered := make(chan struct{})
	statusRelease := make(chan struct{})
	launchEntered := make(chan struct{})
	client := &fakeServiceClient{
		statusFn: func(context.Context) (protocol.Status, error) {
			close(statusEntered)
			<-statusRelease
			return protocol.Status{State: protocol.StateIdle}, nil
		},
		nativeLaunch: func(_ context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
			close(launchEntered)
			expected := "SNES"
			return protocol.Status{State: protocol.StateActive, GameID: &request.GameID, System: &request.System, ExpectedCore: &expected, ObservedCore: &expected}, nil
		},
		stopResult: protocol.Status{State: protocol.StateIdle},
	}
	service := newService(
		Config{
			Libraries: []catalog.Root{root},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
			FPGAROMPaths:   map[string]string{game.ID: "/media/fat/games/SNES/StatusRace.smc"},
		},
		Paths{}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
		withTargetClientFactory(func(TargetConfig) (serviceClient, error) { return client, nil }),
	)
	statusDone := make(chan error, 1)
	go func() {
		_, err := service.Status(ctx)
		statusDone <- err
	}()
	<-statusEntered
	launchDone := make(chan error, 1)
	go func() {
		_, err := service.Launch(ctx, game.ID, nil)
		launchDone <- err
	}()
	select {
	case <-launchEntered:
		t.Fatal("launch reached the target before idle status reconciliation completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(statusRelease)
	if err := <-statusDone; err != nil {
		t.Fatal(err)
	}
	if err := <-launchDone; err != nil {
		t.Fatal(err)
	}
	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Libraries:          []catalog.Root{root},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "spare",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("post-launch target switch error = %v", err)
	}
}

func TestStopDeadlineIsBoundedWhileNamedTargetLaunchIsBlocked(t *testing.T) {
	root := catalog.Root{ID: "snes-stop-deadline", System: protocol.SystemSNES, Path: t.TempDir()}
	game := catalog.Game{
		ID: "snes-stop-deadline", Title: "Stop Deadline", System: protocol.SystemSNES, LibraryID: root.ID,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	launchEntered := make(chan struct{})
	launchRelease := make(chan struct{})
	client := &fakeServiceClient{
		nativeLaunch: func(_ context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
			close(launchEntered)
			<-launchRelease
			expected := "SNES"
			return protocol.Status{State: protocol.StateActive, GameID: &request.GameID, System: &request.System, ExpectedCore: &expected, ObservedCore: &expected}, nil
		},
	}
	service := newService(
		Config{
			Libraries:      []catalog.Root{root},
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
			FPGAROMPaths:   map[string]string{game.ID: "/media/fat/games/SNES/StopDeadline.smc"},
		},
		Paths{}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	launchDone := make(chan error, 1)
	go func() {
		_, err := service.Launch(context.Background(), game.ID, nil)
		launchDone <- err
	}()
	<-launchEntered
	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := service.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked stop error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("blocked stop exceeded bounded deadline: %s", elapsed)
	}
	if client.stopCalls != 0 {
		t.Fatalf("target stop calls = %d, want 0 before launch transition completes", client.stopCalls)
	}
	close(launchRelease)
	if err := <-launchDone; err != nil {
		t.Fatal(err)
	}
}

func TestNamedTargetSwitchRejectsStartupBoundComposedDependencies(t *testing.T) {
	for _, test := range []struct {
		name   string
		config func(*Config)
	}{
		{name: "remote input", config: func(config *Config) { config.RemoteInput.Enabled = true }},
		{name: "media", config: func(config *Config) { config.Media.Enabled = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Config{
				Targets: []TargetConfig{
					{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
				},
				SelectedTarget: "dev",
				Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			}
			test.config(&config)
			service := newService(config, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
			if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
				AttractIdleSeconds: 60,
				PreferredRegions:   []string{"usa"},
				Targets: []TargetConfig{
					{Name: "den", PreviousName: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
				},
				SelectedTarget: "den",
			}); err != nil {
				t.Fatalf("selected target rename: %v", err)
			}
			if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
				AttractIdleSeconds: 60,
				PreferredRegions:   []string{"usa"},
				Targets: []TargetConfig{
					{Name: "den", Enabled: true, Address: "http://192.0.2.10:8182"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.12:8182"},
				},
				SelectedTarget: "den",
			}); err != nil {
				t.Fatalf("unselected target edit: %v", err)
			}
			err := service.SetLibrarySettings(context.Background(), LibraryConfig{
				AttractIdleSeconds: 60,
				PreferredRegions:   []string{"usa"},
				Targets: []TargetConfig{
					{Name: "den", Enabled: true, Address: "http://192.0.2.10:8182"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.12:8182"},
				},
				SelectedTarget: "spare",
			})
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
				t.Fatalf("target switch error = %v", err)
			}
		})
	}
}

func TestNamedTargetRenameRetainsStoredAgentWithoutEcho(t *testing.T) {
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "stored-test-token"}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		withTargetClientFactory(func(TargetConfig) (serviceClient, error) { return &fakeServiceClient{}, nil }),
	)
	if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{{
			Name: "den", PreviousName: "dev", Enabled: true, Address: "http://192.0.2.10:8182",
		}},
		SelectedTarget: "den",
	}); err != nil {
		t.Fatal(err)
	}
	settings := service.LibrarySettings()
	if len(settings.Targets) != 1 || settings.Targets[0].Name != "den" || settings.Targets[0].Agent != "stored-test-token" {
		t.Fatalf("renamed target = %#v", settings.Targets)
	}
}

func TestMergeTargetAgentsUsesExplicitRenameIdentityBeforeDestinationName(t *testing.T) {
	service := &Service{targets: []TargetConfig{
		{Name: "dev", Agent: "dev-test-token"},
		{Name: "spare", Agent: "spare-test-token"},
	}}
	for _, test := range []struct {
		name string
		next []TargetConfig
		want map[string]string
	}{
		{
			name: "swap",
			next: []TargetConfig{
				{Name: "spare", PreviousName: "dev"},
				{Name: "dev", PreviousName: "spare"},
			},
			want: map[string]string{"spare": "dev-test-token", "dev": "spare-test-token"},
		},
		{
			name: "rename into removed existing name",
			next: []TargetConfig{{Name: "spare", PreviousName: "dev"}},
			want: map[string]string{"spare": "dev-test-token"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged, err := service.mergeTargetAgentsLocked(test.next)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range merged {
				if target.Agent != test.want[target.Name] {
					t.Fatalf("target %q agent = %q", target.Name, target.Agent)
				}
			}
		})
	}
	if _, err := service.mergeTargetAgentsLocked([]TargetConfig{
		{Name: "dev"},
		{Name: "den", PreviousName: "dev"},
	}); err == nil {
		t.Fatal("duplicate current target identity was accepted")
	}
}

func (f *fakeServiceClient) ProbeContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	f.probeCalls++
	if f.probe == nil {
		return protocol.CacheProbeResponse{}, errors.New("unexpected probe")
	}
	return f.probe(ctx, system, content)
}

func (f *fakeServiceClient) UploadContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, error) {
	f.uploadCalls++
	if f.upload == nil {
		return protocol.CacheUploadResponse{}, errors.New("unexpected upload")
	}
	return f.upload(ctx, system, content, body)
}

func (f *fakeServiceClient) LaunchContent(ctx context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
	f.launchCalls++
	if f.launch == nil {
		return protocol.CachedLaunchResponse{}, errors.New("unexpected launch")
	}
	response, err := f.launch(ctx, request)
	if err == nil {
		f.activeGame = request.GameID
	}
	return response, err
}

func (f *fakeServiceClient) Launch(ctx context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
	f.nativeLaunchCalls++
	f.nativeLaunchReq = request
	if f.nativeLaunch == nil {
		return protocol.Status{}, errors.New("unexpected native launch")
	}
	status, err := f.nativeLaunch(ctx, request)
	if err == nil {
		f.activeGame = request.GameID
	}
	return status, err
}

func (f *fakeServiceClient) LoadDevelopmentRBF(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
	f.developmentCalls++
	f.developmentSize = size
	if f.developmentLoad == nil {
		return protocol.Status{}, errors.New("unexpected development RBF load")
	}
	return f.developmentLoad(ctx, size, body)
}

func (f *fakeServiceClient) LoadCore(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
	f.coreCalls++
	if f.coreLoad == nil {
		return protocol.Status{}, errors.New("unexpected core package load")
	}
	return f.coreLoad(ctx, size, body)
}

func (f *fakeServiceClient) RebootDevelopment(ctx context.Context) (protocol.Status, error) {
	f.developmentReboots++
	if f.developmentReboot == nil {
		return protocol.Status{}, errors.New("unexpected development reboot")
	}
	return f.developmentReboot(ctx)
}

func (f *fakeServiceClient) Health(ctx context.Context) (protocol.Health, error) {
	f.healthCalls++
	if f.healthFn != nil {
		return f.healthFn(ctx)
	}
	return f.healthResult, f.healthErr
}

func (f *fakeServiceClient) Status(ctx context.Context) (protocol.Status, error) {
	f.statusCalls++
	if f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return f.statusResult, f.statusErr
}

func (f *fakeServiceClient) Stop(ctx context.Context) (protocol.Status, error) {
	f.stopCalls++
	if f.stopFn != nil {
		return f.stopFn(ctx)
	}
	return f.stopResult, f.stopErr
}

type fakeHostExecutor struct {
	launchCalls int
	stopCalls   int
	statusCalls int
	contentPath string
	stopErr     error
}

type ambiguousPackageLoadError struct {
	cause *protocol.APIError
}

func (e *ambiguousPackageLoadError) Error() string           { return e.cause.Error() }
func (e *ambiguousPackageLoadError) Unwrap() error           { return e.cause }
func (e *ambiguousPackageLoadError) AmbiguousMutation() bool { return true }

func (f *fakeHostExecutor) ID() string { return "fake" }
func (f *fakeHostExecutor) Capabilities() []hostexec.Capability {
	return []hostexec.Capability{hostexec.HostOnly}
}
func (f *fakeHostExecutor) Launch(_ context.Context, content io.Reader, identity protocol.ContentIdentity) (hostexec.Status, error) {
	f.launchCalls++
	if content == nil || identity.Size == 0 {
		return hostexec.Status{}, errors.New("missing host content")
	}
	body, err := io.ReadAll(content)
	if err != nil {
		return hostexec.Status{}, err
	}
	f.contentPath = string(body)
	return hostexec.Status{State: hostexec.Active}, nil
}
func (f *fakeHostExecutor) Stop(context.Context) error { f.stopCalls++; return f.stopErr }
func (f *fakeHostExecutor) Status(context.Context) (hostexec.Status, error) {
	f.statusCalls++
	return hostexec.Status{State: hostexec.Active}, nil
}

func newTestService(store *fakeServiceCatalog, preparer *fakeServicePreparer, client serviceClient) *Service {
	return newTestServiceWithExecution(store, preparer, client, ExecutionPolicy{})
}

func newTestServiceWithExecution(store *fakeServiceCatalog, preparer *fakeServicePreparer, client serviceClient, policy ExecutionPolicy) *Service {
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}
	return newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, preparer, client,
		WithExecutionPolicy(policy),
	)
}

func serviceGame(content catalog.Content) catalog.Game {
	return catalog.Game{
		ID: "snes-synthetic", Title: "Synthetic", LibraryID: "snes-main", RelativePath: "game.sfc",
		System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		RootOnline: true, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 123}, Content: &content,
	}
}

func contentIdentity(content catalog.Content) protocol.ContentIdentity {
	return protocol.ContentIdentity{SHA256: content.SHA256, Size: content.Size, Extension: content.Extension}
}

func absentProbe(_ context.Context, _ protocol.System, _ protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	return protocol.CacheProbeResponse{Present: false}, nil
}

func exactNativeLaunchResponse(t *testing.T, game catalog.Game, romPath string) func(context.Context, protocol.LaunchRequest) (protocol.Status, error) {
	t.Helper()
	return func(_ context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
		if request != (protocol.LaunchRequest{GameID: game.ID, System: game.System, ROMPath: romPath}) {
			t.Fatalf("native launch request = %+v", request)
		}
		gameID, system, coreName := game.ID, game.System, "SNES"
		if game.System == protocol.SystemMegaDrive {
			coreName = "MegaDrive"
		}
		return protocol.Status{
			State: protocol.StateActive, GameID: &gameID, System: &system,
			ExpectedCore: &coreName, ObservedCore: &coreName,
		}, nil
	}
}

func exactLaunchResponse(t *testing.T, game catalog.Game, content protocol.ContentIdentity) func(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
	t.Helper()
	return func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		if request != (protocol.CachedLaunchRequest{GameID: game.ID, System: game.System, Content: content}) {
			t.Fatalf("launch request = %+v", request)
		}
		gameID, system, coreName := game.ID, game.System, "SNES"
		if game.System == protocol.SystemMegaDrive {
			coreName = "MegaDrive"
		}
		return protocol.CachedLaunchResponse{
			Status: protocol.Status{
				State: protocol.StateActive, GameID: &gameID, System: &system,
				ExpectedCore: &coreName, ObservedCore: &coreName,
			},
			Content: content,
		}, nil
	}
}

func TestServiceLaunchRejectsUnmappedPlatformWithoutProbe(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "bin"}
	game := catalog.Game{
		ID: "unknown-mario-test", Title: "Mario", LibraryID: "unknown-main", RelativePath: "game.bin",
		System: "mystery", Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		RootOnline: true, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 123}, Content: &content,
	}
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		t.Fatal("unmapped platform probed the target")
		return protocol.CacheProbeResponse{}, nil
	}
	root := catalog.Root{ID: "unknown-main", System: "mystery", Path: "/private/library"}
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeUnsupportedSystem)
	if client.probeCalls != 0 || client.launchCalls != 0 || client.uploadCalls != 0 {
		t.Fatalf("target calls = probe:%d upload:%d launch:%d", client.probeCalls, client.uploadCalls, client.launchCalls)
	}
}

func TestCanonicalRemoteErrorPreservesUnsupportedOperation(t *testing.T) {
	t.Parallel()
	private := errors.New("private target detail")
	err := canonicalRemoteError(errors.Join(&protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "target wording"}, private), protocol.CodeTransferFailed)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation || apiErr.Message != "requested operation is unsupported" {
		t.Fatalf("canonical error = %#v", err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "target wording") {
		t.Fatalf("remote detail leaked: %v", err)
	}
}

type fakePathHostExecutor struct {
	fakeHostExecutor
	pathCalls  int
	lastPath   string
	lastSystem protocol.System
	cleanup    func()
}

func (f *fakePathHostExecutor) LaunchPath(_ context.Context, system protocol.System, sourcePath string) (hostexec.Status, error) {
	return f.LaunchOwnedPath(context.Background(), system, sourcePath, nil)
}

func (f *fakePathHostExecutor) LaunchOwnedPath(_ context.Context, system protocol.System, sourcePath string, cleanup func()) (hostexec.Status, error) {
	f.pathCalls++
	f.lastSystem = system
	f.lastPath = sourcePath
	f.cleanup = cleanup
	return hostexec.Status{State: hostexec.Active}, nil
}

func TestServiceFPGALaunchRejectsCueWithoutTargetUpload(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		t.Fatal("cue set probed the target")
		return protocol.CacheProbeResponse{}, nil
	}
	service := newTestService(store, &fakeServicePreparer{}, client)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 {
		t.Fatalf("target calls = probe:%d upload:%d launch:%d", client.probeCalls, client.uploadCalls, client.launchCalls)
	}
}

func TestServiceHostOnlyCueLaunchUsesConfinedPath(t *testing.T) {
	dir := t.TempDir()
	cuePath := filepath.Join(dir, "game.cue")
	binPath := filepath.Join(dir, "game.bin")
	if err := os.WriteFile(cuePath, []byte("FILE \"game.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("track-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, cuePath)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.upload = func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error) {
		t.Fatal("host cue launch uploaded to the target")
		return protocol.CacheUploadResponse{}, nil
	}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	response, err := service.Launch(context.Background(), game.ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() {
		if adapter.cleanup != nil {
			adapter.cleanup()
		}
	})
	if response.Status.State != protocol.StateActive || adapter.pathCalls != 1 || adapter.lastSystem != protocol.SystemSNES {
		t.Fatalf("path launch response=%+v adapter=%#v", response, adapter)
	}
	if adapter.lastPath == "" || adapter.lastPath == cuePath || filepath.Base(adapter.lastPath) != "game.cue" || !strings.Contains(adapter.lastPath, "fogcast-host-launch-") {
		t.Fatalf("launched path = %q, want private copy of game.cue", adapter.lastPath)
	}
	copiedBin, err := os.ReadFile(filepath.Join(filepath.Dir(adapter.lastPath), "game.bin"))
	if err != nil || string(copiedBin) != "track-bytes" {
		t.Fatalf("copied companion = %q, %v", copiedBin, err)
	}
	if _, err := os.Stat(cuePath); err != nil {
		t.Fatalf("library cue removed: %v", err)
	}
	if client.uploadCalls != 0 || adapter.launchCalls != 0 {
		t.Fatalf("unexpected snapshot/upload launch=%d upload=%d", adapter.launchCalls, client.uploadCalls)
	}
}

func TestServiceHostOnlyCueLaunchRejectsEscapingReference(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "library")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "secret.bin")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.cue"), []byte("FILE \"../secret.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(dir, "game.cue"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		t.Fatal("escaping cue probed the target")
		return protocol.CacheProbeResponse{}, nil
	}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if adapter.pathCalls != 0 || client.probeCalls != 0 || client.uploadCalls != 0 {
		t.Fatalf("escaping cue caused I/O adapter=%d probe=%d upload=%d", adapter.pathCalls, client.probeCalls, client.uploadCalls)
	}
}

func TestServiceHostOnlyCueLaunchRejectsOversizedSheet(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("A", 1<<20+32) + "\nFILE \"../outside.bin\" BINARY\n"
	if err := os.WriteFile(filepath.Join(dir, "game.cue"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(dir, "game.cue"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if adapter.pathCalls != 0 {
		t.Fatalf("oversized cue launched %q", adapter.lastPath)
	}
}

func TestServiceHostOnlyCueLaunchRejectsMissingCompanion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.cue"), []byte("FILE \"game.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(dir, "game.cue"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeSourceUnavailable)
	if adapter.pathCalls != 0 {
		t.Fatalf("missing companion launched %q", adapter.lastPath)
	}
}

func TestServiceHostOnlyLaunchRejectsReplacedSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "library")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(realRoot, "game.chd")
	if err := os.WriteFile(rom, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "game.chd"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(realRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, realRoot); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.chd"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(outside, "game.chd"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: realRoot}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeSourceUnavailable)
	if adapter.pathCalls != 0 {
		t.Fatalf("symlink root launched %q", adapter.lastPath)
	}
}
func fileServiceFingerprint(t *testing.T, path string) catalog.Fingerprint {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return catalog.Fingerprint{SourceSize: info.Size(), ModifiedNS: info.ModTime().UnixNano()}
}
func assertServiceErrorCode(t *testing.T, err error, want protocol.ErrorCode) {
	t.Helper()
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != want {
		t.Fatalf("error = %T %v, want %s", err, err, want)
	}
	if strings.Contains(err.Error(), "/private/") {
		t.Fatalf("error exposes private path: %v", err)
	}
}

func preparedServiceFixture(t *testing.T, body []byte, identity protocol.ContentIdentity) *romsource.Prepared {
	t.Helper()
	prepared, err := romsource.NewPreparedSnapshot(body, identity.Extension)
	if err != nil {
		t.Fatalf("NewPreparedSnapshot: %v", err)
	}
	prepared.Content = identity
	return prepared
}

func assertPreparedRemoved(t *testing.T, prepared *romsource.Prepared) {
	t.Helper()
	if file, err := prepared.Open(); err == nil {
		_ = file.Close()
		t.Fatal("prepared content remained openable after service cleanup")
	}
	if prepared.Path != "" {
		if _, err := os.Lstat(prepared.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("staging path still exists or cannot be inspected: %v", err)
		}
	}
	if err := prepared.Remove(); err != nil {
		t.Fatalf("second Remove = %v, want idempotent success", err)
	}
}

func assertProgressStages(t *testing.T, progress []Progress, want []string) {
	t.Helper()
	stages := make([]string, len(progress))
	for index, event := range progress {
		stages[index] = event.Stage
		if event.Message == "" {
			t.Fatalf("progress %d has empty message", index)
		}
	}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("progress stages = %v, want %v", stages, want)
	}
}

func writeServiceConfig(t *testing.T, path, baseURL, token, root string) {
	t.Helper()
	content := fmt.Sprintf(`base_url = %q
token = %q
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q
`, baseURL, token, root)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
