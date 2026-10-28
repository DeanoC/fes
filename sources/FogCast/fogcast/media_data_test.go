package fogcast

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestInsertLibraryDiskPreDispatchFailureLeavesMediaUnchanged(t *testing.T) {
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	_, err := s.InsertLibraryDisk(context.Background(), protocol.LibraryMediaBinding{})
	if !protocol.LiveMediaNoChange(err) {
		t.Fatalf("invalid binding changed media: %v", err)
	}
	b := protocol.LibraryMediaBinding{
		MediaUnitBinding: protocol.MediaUnitBinding{
			PackageID: strings.Repeat("ab", 32), Generation: 3, Target: "dev",
		},
		GameID: "c64-game", BaseMediaID: strings.Repeat("cd", 32),
	}
	_, err = s.InsertLibraryDisk(context.Background(), b)
	if !protocol.LiveMediaNoChange(err) {
		t.Fatalf("admission failure before dispatch changed media: %v", err)
	}
}
