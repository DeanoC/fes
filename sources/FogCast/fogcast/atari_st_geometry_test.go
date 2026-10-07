package fogcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLiveAtariStGeometryValidatesImmutableBPBAndActiveExtension(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := strings.Repeat("a", 64)
	status := func(state string) protocol.Status {
		s := computerSessionStatus(id, 4, state)
		s.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyGeometryInterface().ID, Major: 1}}
		s.CorePackage.MediaUnits = []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: uint32(protocol.AtariStFloppyGeometryMinBytes), MaxBytes: uint32(protocol.AtariStFloppyGeometryMaxBytes), ChunkBytes: 512, State: state}}
		return s
	}
	client := &mediaUnitServiceClient{fakeServiceClient: fakeServiceClient{statusResult: status("empty")}, after: status}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.activeExecution = ExecutionFPGANative
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 4, Target: "dev"}
	for _, size := range []int{409600, 839680} {
		data := make([]byte, size)
		binary.LittleEndian.PutUint16(data[11:13], 512)
		binary.LittleEndian.PutUint16(data[19:21], uint16(size/512))
		binary.LittleEndian.PutUint16(data[24:26], 10)
		heads := uint16(1)
		if size == 839680 {
			heads = 2
		}
		binary.LittleEndian.PutUint16(data[26:28], heads)
		good, _, err := store.ImportCoreMediaStream(ctx, int64(size), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ReplaceLiveMedia(ctx, good.MediaID, "demo.st", b)
		if err != nil || got.CorePackage == nil || !bytes.Equal(client.inserted, data) {
			t.Fatal("valid demo replacement failed", err)
		}
		calls := client.inserts
		bad := bytes.Clone(data)
		bad[24] ^= 1
		broken, _, err := store.ImportCoreMediaStream(ctx, int64(size), bytes.NewReader(bad))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReplaceLiveMedia(ctx, broken.MediaID, "demo.st", b); err == nil || client.inserts != calls {
			t.Fatal("bad BPB reached target")
		}
		client.statusResult = status("ready")
		client.statusResult.CorePackage.ActiveInterfaces = client.statusResult.CorePackage.ActiveInterfaces[:1]
		if _, err := s.ReplaceLiveMedia(ctx, good.MediaID, "demo.st", b); err == nil || client.inserts != calls {
			t.Fatal("unnegotiated geometry reached target")
		}
		client.statusResult = status("ready")
	}
}
