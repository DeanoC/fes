package pack

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadPlatform(path string) (*Resolved, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	packagesDir, err := packagesRoot(abs)
	if err != nil {
		return nil, err
	}
	var platform PlatformFile
	if err := readYAML(abs, &platform); err != nil {
		return nil, err
	}
	if err := checkHeader(platform.Schema, platform.Kind, "platform", abs); err != nil {
		return nil, err
	}
	boardPath := filepath.Join(packagesDir, platform.Board)
	var board BoardFile
	if err := readYAML(boardPath, &board); err != nil {
		return nil, err
	}
	if err := checkHeader(board.Schema, board.Kind, "board", boardPath); err != nil {
		return nil, err
	}
	socPath := filepath.Join(packagesDir, platform.SoC)
	var soc SoCFile
	if err := readYAML(socPath, &soc); err != nil {
		return nil, err
	}
	if err := checkHeader(soc.Schema, soc.Kind, "soc", socPath); err != nil {
		return nil, err
	}
	cpuPath := filepath.Join(packagesDir, platform.CPU)
	var cpu CPUFile
	if err := readYAML(cpuPath, &cpu); err != nil {
		return nil, err
	}
	if err := checkHeader(cpu.Schema, cpu.Kind, "cpu", cpuPath); err != nil {
		return nil, err
	}

	resolved := &Resolved{
		Root:     packagesDir,
		Platform: platform,
		Board:    board,
		SoC:      soc,
		CPU:      cpu,
	}
	seenBanks := map[string]bool{}
	for _, bank := range soc.Banks {
		if bank.Name == "" {
			return nil, fmt.Errorf("%s: bank missing name", socPath)
		}
		if seenBanks[bank.Name] {
			return nil, fmt.Errorf("%s: duplicate bank %s", socPath, bank.Name)
		}
		seenBanks[bank.Name] = true
		if bank.Registers == "" {
			return nil, fmt.Errorf("%s: bank %s missing registers", socPath, bank.Name)
		}
		regPath := filepath.Join(packagesDir, bank.Registers)
		var regs RegisterBankFile
		if err := readYAML(regPath, &regs); err != nil {
			return nil, err
		}
		if err := checkHeader(regs.Schema, regs.Kind, "register_bank", regPath); err != nil {
			return nil, err
		}
		resolved.Banks = append(resolved.Banks, ResolvedBank{Bank: bank, File: regs})
	}
	return resolved, nil
}

func LoadOracle(path string) (*OracleFile, error) {
	var oracle OracleFile
	if err := readYAML(path, &oracle); err != nil {
		return nil, err
	}
	if len(oracle.Constants) == 0 {
		return nil, fmt.Errorf("%s: no constants", path)
	}
	return &oracle, nil
}

func PeekKind(path string) (string, error) {
	var header struct {
		Schema string `yaml:"schema"`
		Kind   string `yaml:"kind"`
	}
	if err := readYAML(path, &header); err != nil {
		return "", err
	}
	if header.Schema != SchemaV1 {
		return "", fmt.Errorf("%s: schema %q want %q", path, header.Schema, SchemaV1)
	}
	if header.Kind == "" {
		return "", fmt.Errorf("%s: missing kind", path)
	}
	return header.Kind, nil
}

func checkHeader(schema, kind, wantKind, path string) error {
	if schema != SchemaV1 {
		return fmt.Errorf("%s: schema %q want %q", path, schema, SchemaV1)
	}
	if kind != wantKind {
		return fmt.Errorf("%s: kind %q want %q", path, kind, wantKind)
	}
	return nil
}

func readYAML(path string, dest interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func readStrictYAML(path string, dest interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(dest); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s: multiple YAML documents", path)
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func packagesRoot(platformPath string) (string, error) {
	dir := filepath.Dir(platformPath)
	for {
		if filepath.Base(dir) == "packages" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s: not under packages/", platformPath)
		}
		dir = parent
	}
}
