package fogcast

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestMediaUnchangedPreservesJoinedCause(t *testing.T) {
	err := mediaUnchanged(errors.Join(
		&protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "insert status unavailable", Phase: "admission"},
		context.Canceled,
	))
	var api *protocol.APIError
	if !errors.As(err, &api) || !api.NoChange || !errors.Is(err, context.Canceled) {
		t.Fatalf("mediaUnchanged lost the no-change flag or cause: %v", err)
	}
}

func TestInsertLibraryDiskLifecycleCancellationLeavesMediaUnchanged(t *testing.T) {
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	_, err := s.InsertLibraryDisk(context.Background(), protocol.LibraryMediaBinding{})
	if !protocol.LiveMediaNoChange(err) {
		t.Fatalf("invalid binding changed media: %v", err)
	}
	release, err := s.acquireLifecycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	b := protocol.LibraryMediaBinding{
		MediaUnitBinding: protocol.MediaUnitBinding{
			PackageID: strings.Repeat("ab", 32), Generation: 3, Target: "dev",
		},
		GameID: "c64-game", BaseMediaID: strings.Repeat("cd", 32),
	}
	_, err = s.InsertLibraryDisk(canceled, b)
	if !protocol.LiveMediaNoChange(err) || !errors.Is(err, context.Canceled) {
		t.Fatalf("lifecycle cancellation changed media or lost its cause: %v", err)
	}
}

func TestInsertLibraryDiskStaleGenerationIsNoChange(t *testing.T) {
	ctx := context.Background()
	s, client, entry, disk := diskLaunchFixture(t, protocol.AtariStFloppyInterface(), protocol.AtariStFloppyBytes)
	base, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, base.MediaID); err != nil {
		t.Fatal(err)
	}
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	dev := s.libraryDevelopmentMediaBinding(*response.Status.CorePackage)
	b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{
		PackageID: dev.PackageID, Generation: dev.Generation, Unit: protocol.AtariStFloppyUnit,
		Target: dev.Target, TargetID: dev.TargetID,
	}, GameID: entry.GameID, BaseMediaID: base.MediaID}
	stale := b
	stale.Generation++
	_, err = s.InsertLibraryDisk(ctx, stale)
	if !protocol.LiveMediaNoChange(err) || client.libraryCalls != 1 {
		t.Fatalf("stale generation err=%v library insert calls=%d", err, client.libraryCalls)
	}
}

func TestInsertLibraryDiskPreservesTargetNoChange(t *testing.T) {
	ctx := context.Background()
	s, client, entry, disk := diskLaunchFixture(t, protocol.AtariStFloppyInterface(), protocol.AtariStFloppyBytes)
	base, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, base.MediaID); err != nil {
		t.Fatal(err)
	}
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	dev := s.libraryDevelopmentMediaBinding(*response.Status.CorePackage)
	b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{
		PackageID: dev.PackageID, Generation: dev.Generation, Unit: protocol.AtariStFloppyUnit,
		Target: dev.Target, TargetID: dev.TargetID,
	}, GameID: entry.GameID, BaseMediaID: base.MediaID}
	client.libraryErr = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "insert rejected", Phase: "input", NoChange: true}
	_, err = s.InsertLibraryDisk(ctx, b)
	if !protocol.LiveMediaNoChange(err) || client.libraryCalls != 2 {
		t.Fatalf("target no-change err=%v library insert calls=%d", err, client.libraryCalls)
	}
}
