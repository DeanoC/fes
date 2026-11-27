package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"unicode/utf8"
)

// Nano format 3 retains one firmware image that is delivered beside the
// unchanged payload: to the CPU-fed held-ROM endpoint, or at launch over the
// fes.computer mailbox. This map names no physical address, bridge grant or
// bitstream destinations.
const maxNanoROMMapBytes = 1024

func nanoROM(d Descriptor) bool {
	return d.Format == 3 && d.Target.Platform == "de25_nano" && d.Target.Device == "A5EB013BB23BE4SCS"
}

// nanoROMEncoding is the map encoding fixed by the package's one required ROM
// transport interface. Declaring both, neither, or one that is optional or at
// another version has no encoding.
func nanoROMEncoding(d Descriptor) (string, bool) {
	encoding, transports := "", 0
	for _, i := range d.Interfaces {
		selected := ""
		switch i.ID {
		case "fes.embedded.rom":
			selected = "cpu-csr-v1"
		case "fes.fabric.firmware-upload":
			selected = "gp-firmware-upload-v1"
		default:
			continue
		}
		transports++
		if i.Major == 1 && i.Minor == 0 && i.Required {
			encoding = selected
		}
	}
	return encoding, transports == 1 && encoding != ""
}

// FirmwareUpload reports a Nano package whose one firmware image the runtime
// uploads over the fes.computer mailbox at launch. Its payload is programmed
// unchanged, so no linker runs and the ROM source itself goes to the kit.
func FirmwareUpload(d Descriptor) bool {
	encoding, ok := nanoROMEncoding(d)
	return nanoROM(d) && d.ROM != nil && ok && encoding == "gp-firmware-upload-v1"
}

// DE25ComputerABI is the exact fes.computer contract a DE25 machine runs. The
// PC/XT's floppy and the pc386's ATA disk, machine words and wall clock are
// fes.computer 1.1 interfaces; every other machine is 1.0.
func DE25ComputerABI(coreID string) Contract {
	return Contract{ID: "fes.computer", Major: 1, Minor: map[string]int64{"fes.pcxt-dos": 1, "fes.pc386": 1}[coreID]}
}

// firmwareUploadMachine admits the machines the DE25 runtime launches this
// way, each with its endpoint's exact image sizes. The package format allows
// any 1 KiB multiple; a launch does not.
func firmwareUploadMachine(d Descriptor) error {
	sizes := map[string][]int64{
		"fes.spectrum":       {16384, 32768},
		"fes.spectrum128":    {32768},
		"fes.spectrum-plus3": {65536},
		"fes.pentagon":       {49152},
		"fes.atari-st":       {196608},
		"fes.c64":            {36864},
		// The 64 KiB system ROM window, then the 4 KiB CGA font.
		"fes.pcxt-dos": {73728},
	}[d.Core.ID]
	if !FirmwareUpload(d) || d.Target.ProgrammingProfile != "de25-linux-computer-v1" ||
		d.ABI != DE25ComputerABI(d.Core.ID) || !slices.Contains(sizes, d.ROM.SourceSize) {
		return errors.New("firmware upload requires a DE25 Spectrum-family, Atari ST, C64 or PC/XT computer package and its exact image size")
	}
	return nil
}

func validateNanoROMMap(ctx context.Context, r io.Reader, size int64, d Descriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !nanoROM(d) || d.ROM == nil || size < 1 || size > maxNanoROMMapBytes {
		return errors.New("invalid held-ROM map scope or bound")
	}
	raw, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) != size || !utf8.Valid(raw) {
		return errors.New("invalid held-ROM map length or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("held-ROM map requires an object")
	}
	seen := make(map[string]bool)
	var format, sourceSize int64
	var device, encoding, base string
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return errors.New("invalid or duplicate held-ROM map key")
		}
		seen[key] = true
		switch key {
		case "format":
			err = decoder.Decode(&format)
		case "device":
			err = decoder.Decode(&device)
		case "encoding":
			err = decoder.Decode(&encoding)
		case "base_sha256":
			err = decoder.Decode(&base)
		case "source_size":
			err = decoder.Decode(&sourceSize)
		default:
			return errors.New("unknown held-ROM map key")
		}
		if err != nil {
			return err
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return errors.New("incomplete held-ROM map")
	}
	var extra any
	expected, ok := nanoROMEncoding(d)
	if decoder.Decode(&extra) != io.EOF || len(seen) != 5 || format != 1 || !ok ||
		device != d.Target.Device || encoding != expected ||
		base != d.Payload.SHA256 || sourceSize != d.ROM.SourceSize {
		return errors.New("held-ROM map differs from sealed package")
	}
	return ctx.Err()
}
