package fogcast

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/DeanoC/FogCast/corepackage"
)

// MachineROMLinker replaces the empty ZX81 machine ROM in an already placed RBF.
// The sealed package bytes stay unchanged.
type MachineROMLinker interface {
	Link(base []byte) (programmed []byte, imageSHA256 string, err error)
}

// PythonMachineROM runs link_static_rbf.py init --machine on the host.
type PythonMachineROM struct {
	Python    string
	Script    string
	Image     string
	MistralCV string
}

func (p PythonMachineROM) Link(base []byte) ([]byte, string, error) {
	python := p.Python
	if python == "" {
		python = "python3"
	}
	if p.Script == "" || p.Image == "" || p.MistralCV == "" || len(base) == 0 {
		return nil, "", errors.New("zx81 machine ROM linker is not configured")
	}
	directory, err := os.MkdirTemp("", "fes-zx81-rom-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(directory)
	basePath := directory + "/base.rbf"
	outputPath := directory + "/programmed.rbf"
	if err = os.WriteFile(basePath, base, 0o600); err != nil {
		return nil, "", err
	}
	cmd := exec.Command(python, p.Script, "init", "--machine", "--base", basePath, "--image", p.Image, "--output", outputPath)
	cmd.Env = append(os.Environ(), "MISTRAL_CV="+p.MistralCV)
	output, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("zx81 machine ROM link failed: %w", err)
	}
	programmed, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, "", err
	}
	var receipt struct {
		ImageSHA256 string `json:"image_sha256"`
	}
	if err = json.Unmarshal(output, &receipt); err != nil || len(receipt.ImageSHA256) != 64 {
		return nil, "", errors.New("zx81 machine ROM link receipt is invalid")
	}
	return programmed, receipt.ImageSHA256, nil
}

func (s *Service) SetMachineROMLinker(linker MachineROMLinker) {
	s.machineROM = linker
}

func (s *Service) applyZX81MachineROM(coreID string, archive []byte, composed *corepackage.CompositionBundle) ([]byte, string, error) {
	if coreID != "fes.zx81" || s.machineROM == nil {
		return nil, "", nil
	}
	base := archive
	var composition []byte
	if composed != nil {
		base = composed.Payload
		var encoded bytes.Buffer
		if err := composed.Write(&encoded); err != nil {
			return nil, "", err
		}
		composition = encoded.Bytes()
	} else {
		payload, err := corepackage.ArchivePayload(archive)
		if err != nil {
			return nil, "", err
		}
		base = payload
	}
	programmed, imageSHA, err := s.machineROM.Link(base)
	if err != nil {
		return nil, "", err
	}
	body, err := corepackage.WriteRomInit(corepackage.RomInit{
		Package: archive, Programmed: programmed, ImageSHA256: imageSHA, Composition: composition,
	})
	if err != nil {
		return nil, "", err
	}
	return body, imageSHA, nil
}
