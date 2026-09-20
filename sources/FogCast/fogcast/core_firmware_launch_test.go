package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func firmwareColecoPackageFixture(t *testing.T) []byte {
	t.Helper()
	return colecoLibraryPackageContractsFixture(t, "fes.application", `
[[interfaces]]
id = "fes.firmware.blob"
major = 1
minor = 0
required = false
`)
}

func attachLibraryMedia(t *testing.T, s *Service, entryGameID, packageID, expectedMedia string, body []byte) {
	t.Helper()
	ctx := context.Background()
	asset, _, err := s.ImportCoreMedia(ctx, int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entryGameID, packageID, expectedMedia, "blob", asset.MediaID); err != nil {
		t.Fatal(err)
	}
}

func importHouseholdFirmware(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	data := bytes.Repeat([]byte{0x55, 0xaa}, int(protocol.FirmwareBytes/2))
	asset, _, err := s.ImportCoreMedia(ctx, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, asset.MediaID); err != nil {
		t.Fatal(err)
	}
}

func coreEntryFirmwareActiveStatus(inspection corepackage.Inspection, generation uint64) protocol.Status {
	status := coreEntryActiveStatus(inspection, generation, true)
	status.CorePackage.ActiveInterfaces = append(status.CorePackage.ActiveInterfaces,
		protocol.RuntimeInterface{ID: protocol.FirmwareInterfaceID, Major: 1})
	return status
}

type firmwareLaunchClient struct {
	*defaultMediaPackageClient
	active   protocol.Status
	roles    []string
	bodies   [][]byte
	bindings []protocol.DevelopmentMediaBinding
}

func (c *firmwareLaunchClient) LoadDevelopmentMedia(_ context.Context, _ int64, body io.Reader, binding protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	data, _ := io.ReadAll(body)
	c.roles = append(c.roles, binding.Role)
	c.bodies = append(c.bodies, data)
	c.bindings = append(c.bindings, binding)
	c.mediaCalls++
	c.mediaBody = data
	c.mediaBinding = binding
	return c.active, nil
}

func TestGraphicsIStaysReadyAndLaunchesWithoutFirmware(t *testing.T) {
	ctx := context.Background()
	raw := firmwareColecoPackageFixture(t)
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Graphics I")
	attachLibraryMedia(t, s, entry.GameID, entry.PackageID, "", []byte("graphics-i cart"))
	active := coreEntryFirmwareActiveStatus(inspection, 3)
	client.mediaStatus = active
	client.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}

	comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !comps[entry.GameID].FirmwareReady || comps[entry.GameID].FirmwareRequired {
		t.Fatalf("graphics-i composition = %+v err=%v", comps, err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if client.coreCalls != 1 || client.mediaCalls != 1 || client.mediaBinding.Role != "" {
		t.Fatalf("core=%d media=%d role=%q, want package then cart only", client.coreCalls, client.mediaCalls, client.mediaBinding.Role)
	}
}

func TestFroggerFailsClosedBeforeFPGAWithoutHouseholdFirmware(t *testing.T) {
	ctx := context.Background()
	raw := firmwareColecoPackageFixture(t)
	s, client, _, inspection := newCoreEntryLaunchFixture(t, raw, "placeholder")
	cartBody := []byte("frogger cart")
	cart, _, err := s.ImportCoreMedia(ctx, int64(len(cartBody)), bytes.NewReader(cartBody))
	if err != nil {
		t.Fatal(err)
	}
	frogger, err := s.CreateCoreEntryWithFirmware(ctx, "Frogger", inspection.PackageID, "blob", cart.MediaID, true)
	if err != nil || !frogger.FirmwareRequired {
		t.Fatalf("frogger entry: %+v %v", frogger, err)
	}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		t.Fatal("missing firmware programmed the FPGA")
		return protocol.Status{}, nil
	}

	comps, err := s.CoreCompositions(ctx, []string{frogger.GameID})
	if err != nil || !comps[frogger.GameID].FirmwareRequired || comps[frogger.GameID].FirmwareReady {
		t.Fatalf("frogger composition = %+v err=%v", comps, err)
	}
	_, launchErr := s.Launch(ctx, frogger.GameID, nil)
	var apiErr *protocol.APIError
	if !errors.As(launchErr, &apiErr) || apiErr.Code != protocol.CodeBadRequest ||
		!strings.Contains(apiErr.Message, "firmware") || client.coreCalls != 0 || client.mediaCalls != 0 {
		t.Fatalf("launch error = %v core=%d media=%d", launchErr, client.coreCalls, client.mediaCalls)
	}
	if apiErr.Phase != "request" && apiErr.Phase != "admission" {
		t.Fatalf("phase = %q, want pre-dispatch", apiErr.Phase)
	}
}

func TestFroggerBindsFirmwareBeforeMediaOnCapablePackage(t *testing.T) {
	ctx := context.Background()
	raw := firmwareColecoPackageFixture(t)
	s, client, _, inspection := newCoreEntryLaunchFixture(t, raw, "placeholder")
	cartBody := []byte("frogger cart")
	cart, _, err := s.ImportCoreMedia(ctx, int64(len(cartBody)), bytes.NewReader(cartBody))
	if err != nil {
		t.Fatal(err)
	}
	frogger, err := s.CreateCoreEntryWithFirmware(ctx, "Frogger", inspection.PackageID, "blob", cart.MediaID, true)
	if err != nil {
		t.Fatal(err)
	}
	importHouseholdFirmware(t, s)
	active := coreEntryFirmwareActiveStatus(inspection, 5)
	recorder := &firmwareLaunchClient{defaultMediaPackageClient: client, active: active}
	recorder.coreLoad = func(_ context.Context, _ int64, _ io.Reader) (protocol.Status, error) {
		recorder.statusResult = active
		return active, nil
	}
	s.targetClients[s.selectedTarget] = recorder

	comps, err := s.CoreCompositions(ctx, []string{frogger.GameID})
	if err != nil || !comps[frogger.GameID].FirmwareReady {
		t.Fatalf("frogger ready composition = %+v err=%v", comps, err)
	}
	if _, err := s.Launch(ctx, frogger.GameID, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if recorder.coreCalls != 1 || len(recorder.roles) != 2 || recorder.roles[0] != protocol.FirmwareRole || recorder.roles[1] != "" {
		t.Fatalf("bind order core=%d roles=%v", recorder.coreCalls, recorder.roles)
	}
	if len(recorder.bodies[0]) != int(protocol.FirmwareBytes) || !bytes.Equal(recorder.bodies[1], []byte("frogger cart")) {
		t.Fatalf("firmware=%d cart=%q", len(recorder.bodies[0]), recorder.bodies[1])
	}
	if recorder.bindings[0].Role != protocol.FirmwareRole || recorder.bindings[0].Generation != 5 {
		t.Fatalf("firmware binding=%+v", recorder.bindings[0])
	}
}

func TestFroggerImportedBIOSCannotBindOnBlobOnlyPackage(t *testing.T) {
	ctx := context.Background()
	s, client, _, inspection := newCoreEntryLaunchFixture(t, colecoLibraryPackageFixture(t), "placeholder")
	cartBody := []byte("frogger cart")
	cart, _, err := s.ImportCoreMedia(ctx, int64(len(cartBody)), bytes.NewReader(cartBody))
	if err != nil {
		t.Fatal(err)
	}
	frogger, err := s.CreateCoreEntryWithFirmware(ctx, "Frogger", inspection.PackageID, "blob", cart.MediaID, true)
	if err != nil {
		t.Fatal(err)
	}
	importHouseholdFirmware(t, s)
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		t.Fatal("blob-only package programmed the FPGA for Frogger")
		return protocol.Status{}, nil
	}

	comps, err := s.CoreCompositions(ctx, []string{frogger.GameID})
	if err != nil || comps[frogger.GameID].FirmwareReady {
		t.Fatalf("blob-only frogger composition = %+v err=%v", comps, err)
	}
	_, launchErr := s.Launch(ctx, frogger.GameID, nil)
	var apiErr *protocol.APIError
	if !errors.As(launchErr, &apiErr) || apiErr.Code != protocol.CodeBadRequest ||
		!strings.Contains(apiErr.Message, "fes.firmware.blob") || client.coreCalls != 0 {
		t.Fatalf("launch error = %v core=%d", launchErr, client.coreCalls)
	}
}
