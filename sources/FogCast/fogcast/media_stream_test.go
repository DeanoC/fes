package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLibraryMediaDeliveryDeadline(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy configured timeout", true: "stream 150 second budget"}[stream], func(t *testing.T) {
			extra := ""
			if stream {
				extra = "\n[[interfaces]]\nid = \"fes.media.blob-stream\"\nmajor = 1\nminor = 0\nrequired = true\n"
			}
			s, client, entry, inspection := newCoreEntryLaunchFixture(t, serviceMediaPackageFixture(t, "0.1.0", true, extra), "delivery deadline")
			s.uploadTimeout = time.Second
			active := coreEntryActiveStatus(inspection, 9, true)
			if stream {
				active.CorePackage.ActiveInterfaces = append(active.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: protocol.MediaStreamInterface().ID, Major: 1})
				active.CorePackage.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
			}
			client.mediaStatus = active
			client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
				client.statusResult = active
				return active, nil
			}
			started := time.Now()
			_, err := s.Launch(context.Background(), entry.GameID, nil)
			finished := time.Now()
			if err != nil || client.mediaCalls != 1 || client.mediaBinding.Stream != stream {
				t.Fatalf("delivery calls=%d binding=%+v err=%v", client.mediaCalls, client.mediaBinding, err)
			}
			want := s.uploadTimeout
			if stream {
				want = 150 * time.Second
			}
			// Bound the absolute deadline by the call interval; no sleeps or
			// narrow scheduler-sensitive remaining-time tolerance is needed.
			if client.mediaDeadline.Before(started.Add(want)) || client.mediaDeadline.After(finished.Add(want)) {
				t.Fatalf("delivery deadline=%v, want between %v and %v", client.mediaDeadline, started.Add(want), finished.Add(want))
			}
		})
	}
}

func TestLibraryMediaStreamUsesDeclaredContractAndObservedEndpoint(t *testing.T) {
	for _, observed := range []bool{true, false} {
		t.Run(map[bool]string{true: "supported", false: "missing observation"}[observed], func(t *testing.T) {
			ctx := context.Background()
			extra := "\n[[interfaces]]\nid = \"fes.media.blob-stream\"\nmajor = 1\nminor = 0\nrequired = true\n"
			s, client, entry, inspection := newCoreEntryLaunchFixture(t, serviceMediaPackageFixture(t, "0.1.0", true, extra), "stream title")
			payload := bytes.Repeat([]byte{0, 255}, 16384)
			media := importServiceCoreMedia(t, s, payload)
			entry, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, "blob", media.MediaID)
			if err != nil {
				t.Fatal(err)
			}
			large := importServiceCoreMedia(t, s, append(bytes.Clone(payload), 1))
			if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, "blob", large.MediaID); err == nil {
				t.Fatal("oversize selection accepted")
			}
			active := coreEntryActiveStatus(inspection, 9, true)
			active.CorePackage.ActiveInterfaces = append(active.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: protocol.MediaStreamInterface().ID, Major: 1})
			if observed {
				active.CorePackage.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
			}
			client.mediaStatus = active
			client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
				client.statusResult = active
				return active, nil
			}
			_, err = s.Launch(ctx, entry.GameID, nil)
			if observed {
				if err != nil || client.mediaCalls != 1 || !client.mediaBinding.Stream || !bytes.Equal(client.mediaBody, payload) {
					t.Fatalf("stream calls=%d binding=%+v err=%v", client.mediaCalls, client.mediaBinding, err)
				}
			} else if err == nil || client.mediaCalls != 0 || client.stopCalls != 1 {
				t.Fatalf("unobserved dispatch: media=%d stop=%d err=%v", client.mediaCalls, client.stopCalls, err)
			}
		})
	}
}

type closeFailMediaStore struct{ *catalog.Store }
type closeFailMediaReader struct{ io.ReadCloser }

func (r closeFailMediaReader) Close() error {
	return errors.Join(r.ReadCloser.Close(), errors.New("close /private/temp/media.bin failed"))
}
func (s closeFailMediaStore) OpenCoreMedia(ctx context.Context, id string) (catalog.CoreMedia, io.ReadCloser, error) {
	media, reader, err := s.Store.OpenCoreMedia(ctx, id)
	if err != nil {
		return media, reader, err
	}
	return media, closeFailMediaReader{reader}, nil
}

func TestLibraryMediaStreamCloseFailureStopsActivatedTarget(t *testing.T) {
	extra := "\n[[interfaces]]\nid = \"fes.media.blob-stream\"\nmajor = 1\nminor = 0\nrequired = true\n"
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, serviceMediaPackageFixture(t, "0.1.0", true, extra), "close failure")
	active := coreEntryActiveStatus(inspection, 9, true)
	active.CorePackage.ActiveInterfaces = append(active.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: protocol.MediaStreamInterface().ID, Major: 1})
	active.CorePackage.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
	client.mediaStatus = active
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	s.catalog = closeFailMediaStore{s.catalog.(*catalog.Store)}
	_, err := s.Launch(context.Background(), entry.GameID, nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal || apiErr.Phase != "recovery" || strings.Contains(err.Error(), "/private/temp") || client.mediaCalls != 1 || client.stopCalls != 1 {
		t.Fatalf("media=%d stop=%d err=%v", client.mediaCalls, client.stopCalls, err)
	}
}
