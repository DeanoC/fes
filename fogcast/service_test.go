package fogcast

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/DeanoC/FogCast-POC/romsource"
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
			if store.gameCalls != 1 || store.updateCalls != 0 {
				t.Fatalf("catalog calls = game:%d update:%d", store.gameCalls, store.updateCalls)
			}
			if preparer.calls != 0 || client.uploadCalls != 0 || client.launchCalls != 1 {
				t.Fatalf("source/target calls = prepare:%d upload:%d launch:%d", preparer.calls, client.uploadCalls, client.launchCalls)
			}
		})
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
	assertProgressStages(t, progress, []string{"prepare", "cache", "cache", "upload", "launch"})
	assertPreparedRemoved(t, prepared)
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
	if got, err := service.Search(ctx, "synthetic"); err != nil || !reflect.DeepEqual(got, []catalog.Game{game}) {
		t.Fatalf("Search = %+v, %v", got, err)
	}
	if store.searchQuery != "synthetic" {
		t.Fatalf("search query = %q", store.searchQuery)
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
	if store.closeCalls != 1 {
		t.Fatalf("catalog close calls = %d, want 1", store.closeCalls)
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
	if games, err := service.Games(context.Background()); err != nil || len(games) != 0 {
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
	if err != nil || len(games) != 1 {
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
	if err != nil || len(games) != 1 {
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
	assertServiceErrorCode(t, err, protocol.CodeSourceUnavailable)
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
	updateCalls int
	closeCalls  int
	searchQuery string
	rootMatch   func(context.Context, catalog.Game, catalog.Root) (bool, error)
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

func (f *fakeServiceCatalog) GameMatchesRoot(ctx context.Context, game catalog.Game, root catalog.Root) (bool, error) {
	if f.rootMatch != nil {
		return f.rootMatch(ctx, game, root)
	}
	return game.LibraryID == root.ID && game.System == root.System, nil
}

func (f *fakeServiceCatalog) CompareAndSetContent(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (bool, error) {
	f.updateCalls++
	if f.update != nil {
		return f.update(ctx, game, root, content)
	}
	return true, nil
}

func (f *fakeServiceCatalog) Close() error {
	f.closeCalls++
	return nil
}

type fakeServiceScanner struct {
	report catalog.ScanReport
	err    error
	calls  int
	roots  []catalog.Root
}

type serviceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f serviceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
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

func (f *fakeServiceScanner) Scan(_ context.Context, roots []catalog.Root) (catalog.ScanReport, error) {
	f.calls++
	f.roots = append([]catalog.Root(nil), roots...)
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
	probe        func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error)
	upload       func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error)
	launch       func(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error)
	probeCalls   int
	uploadCalls  int
	launchCalls  int
	healthCalls  int
	statusCalls  int
	stopCalls    int
	activeGame   string
	healthResult protocol.Health
	statusResult protocol.Status
	stopResult   protocol.Status
	healthErr    error
	statusErr    error
	stopErr      error
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

func (f *fakeServiceClient) Health(context.Context) (protocol.Health, error) {
	f.healthCalls++
	return f.healthResult, f.healthErr
}

func (f *fakeServiceClient) Status(context.Context) (protocol.Status, error) {
	f.statusCalls++
	return f.statusResult, f.statusErr
}

func (f *fakeServiceClient) Stop(context.Context) (protocol.Status, error) {
	f.stopCalls++
	return f.stopResult, f.stopErr
}

func newTestService(store *fakeServiceCatalog, preparer *fakeServicePreparer, client *fakeServiceClient) *Service {
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}
	return newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, preparer, client,
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
