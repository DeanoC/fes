package fogcast

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

// Synthetic package bytes exercise the real package reader and catalog. They
// are not a deployable FPGA artifact.
func applicationLibraryFixture(t *testing.T, media bool) []byte {
	t.Helper()
	raw := libraryPackageFixture(t, "0.1.0")
	r := tar.NewReader(bytes.NewReader(raw))
	var out bytes.Buffer
	offset := 0
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		header := bytes.Clone(raw[offset : offset+512])
		offset += 512 + (len(body)+511)/512*512
		if h.Name == "manifest.toml" {
			body = bytes.Replace(body, []byte("fes.simple-game"), []byte("fes.application"), 1)
			body = bytes.Replace(body, []byte("fes.pong"), []byte("example.palette"), 1)
			if media {
				body = append(body, []byte("\n[[interfaces]]\nid = \"fes.media.blob\"\nmajor = 1\nminor = 0\nrequired = true\n")...)
			} else {
				body = bytes.Replace(body, []byte("[[interfaces]]\nid = \"fes.gamepad\"\nmajor = 1\nminor = 0\nrequired = true\n"), nil, 1)
			}
		}
		copy(header[124:136], fmt.Sprintf("%011o\x00", len(body)))
		copy(header[148:156], "        ")
		sum := 0
		for _, b := range header {
			sum += int(b)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(header)
		out.Write(body)
		out.Write(make([]byte, (512-len(body)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func TestApplicationLibraryLaunchRequiresMediaBeforeMutation(t *testing.T) {
	ctx := context.Background()
	s, client, entry, _ := newCoreEntryLaunchFixture(t, applicationLibraryFixture(t, true), "Palette application")
	rejection := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Phase: "identity"}
	s.packageRejection = rejection
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		t.Fatal("missing media changed the active FPGA")
		return protocol.Status{}, nil
	}
	_, err := s.Launch(ctx, entry.GameID, nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest || apiErr.Phase != "request" {
		t.Fatalf("missing-media launch: %v", err)
	}
	if client.stopCalls != 0 || client.mediaCalls != 0 {
		t.Fatal("missing-media launch mutated target")
	}
	if s.packageRejection != rejection {
		t.Fatal("invalid next launch cleared pending recovery")
	}
}

func TestApplicationLibraryLaunchComposesGamepadAndSelectedMedia(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, applicationLibraryFixture(t, true), "Palette application")
	payload := []byte{255, 32, 96}
	asset, _, err := s.ImportCoreMedia(ctx, int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	entry, err = s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "blob", asset.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	active := coreEntryActiveStatus(inspection, 12, true)
	active.CorePackage.Gamepad = true
	active.CorePackage.ActiveInterfaces = append(active.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: "fes.gamepad", Major: 1})
	client.mediaStatus = active
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	result, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.mediaCalls != 1 || !bytes.Equal(client.mediaBody, payload) || client.mediaBinding.Generation != 12 ||
		client.mediaBinding.PackageID != entry.PackageID || !result.Status.CorePackage.Gamepad {
		t.Fatalf("application launch: %+v media=%x binding=%+v", result, client.mediaBody, client.mediaBinding)
	}
}

func TestApplicationVideoOnlyLibraryLaunch(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, applicationLibraryFixture(t, false), "Autonomous demo")
	for _, contract := range inspection.Descriptor.Interfaces {
		if contract.ID != "fes.video.fixed-720p60" {
			t.Fatalf("unexpected demo interface: %+v", contract)
		}
	}
	active := coreEntryActiveStatus(inspection, 4, false)
	active.CorePackage.Gamepad = false
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	result, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || result.Status.CorePackage.Gamepad || client.mediaCalls != 0 {
		t.Fatalf("video demo launch=%+v err=%v", result, err)
	}
}
