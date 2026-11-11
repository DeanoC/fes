// de25-pr-target-proof assembles ordinary vendor-produced files into an offline
// base-plus-persona deployment bundle. It has no device, controller or MMIO API.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type selectionFlags []string

func (s *selectionFlags) String() string { return strings.Join(*s, ",") }
func (s *selectionFlags) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type deploymentStep struct {
	Action     string `json:"action"`
	Artifact   string `json:"artifact"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	RegionID   string `json:"region_id,omitempty"`
	Persona    string `json:"persona,omitempty"`
	HardwareID uint32 `json:"hardware_id,omitempty"`
}

type deployment struct {
	Format             int              `json:"format"`
	Kind               string           `json:"kind"`
	DiagnosticOnly     bool             `json:"diagnostic_only"`
	PhysicalAcceptance string           `json:"physical_acceptance"`
	Controller         string           `json:"controller"`
	MonolithicRBF      bool             `json:"monolithic_rbf"`
	ShellID            string           `json:"shell_id"`
	ControlBuildID     string           `json:"control_build_id"`
	DescriptorSHA256   string           `json:"descriptor_sha256"`
	Device             string           `json:"device"`
	CompositionID      string           `json:"composition_id"`
	Steps              []deploymentStep `json:"steps"`
}

func makeDeployment(a artifacts, p persona, receipt []byte) (deployment, []byte, error) {
	result := deployment{Format: 2, Kind: "fes.de25-pr-proof-deployment", DiagnosticOnly: true,
		PhysicalAcceptance: "not-run", Controller: "runtime-protocol-only", MonolithicRBF: false,
		ShellID: a.Shell.ID, DescriptorSHA256: digest(receipt), Device: a.Device,
		ControlBuildID: a.Shell.ControlBuildID,
		Steps: []deploymentStep{
			{Action: "load-base", Artifact: "base.rbf", SHA256: a.Shell.Base.SHA256, Size: a.Shell.Base.Size},
			{Action: "load-persona", Artifact: "slot0/persona.rbf", SHA256: p.SHA256, Size: p.Size, RegionID: p.RegionID, Persona: p.ID, HardwareID: p.HardwareID},
		}}
	identity, err := canonical(map[string]any{"shell_id": result.ShellID, "control_build_id": result.ControlBuildID, "descriptor_sha256": result.DescriptorSHA256, "steps": result.Steps})
	if err != nil {
		return deployment{}, nil, err
	}
	result.CompositionID = digest(append([]byte("fes-de25-pr-deployment-v2\x00"), identity...))
	encoded, err := canonical(result)
	return result, append(encoded, '\n'), err
}

// openRegular permits only existing ordinary files underneath the receipt root.
// Symlink components are refused; os.Root also contains traversal during races.
func openRegular(root *os.Root, name string) (*os.File, error) {
	if !safePath(name) {
		return nil, errors.New("artifact path is not a bounded relative path")
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return nil, errors.New("artifact path contains a symlink or non-directory")
		}
	}
	// Nonblocking/no-follow avoids waiting on a substituted FIFO or following a
	// last-component symlink between the namespace check and the open.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("artifact is not a regular file")
	}
	return f, nil
}

func readDescriptor(root *os.Root, name string) ([]byte, error) {
	f, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil || len(data) > maxManifestBytes {
		return nil, errors.New("descriptor exceeds bounded read")
	}
	return data, nil
}

// copyBlob hashes the exact bytes it copies, and detects truncation, extension
// and a changed payload. The same operation is used for validation and bundling.
func copyBlob(root *os.Root, b blob, destination io.Writer) error {
	f, err := openRegular(root, b.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != b.Size {
		return fmt.Errorf("artifact size differs: %s", b.Path)
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(f, b.Size+1))
	if err != nil {
		return err
	}
	if n != b.Size || hex.EncodeToString(hash.Sum(nil)) != b.SHA256 {
		return fmt.Errorf("artifact bytes differ: %s", b.Path)
	}
	return nil
}

func tarBytes(writer *tar.Writer, name string, value []byte) error {
	if err := tarHeader(writer, name, int64(len(value))); err != nil {
		return err
	}
	_, err := writer.Write(value)
	return err
}

func tarHeader(writer *tar.Writer, name string, size int64) error {
	return writer.WriteHeader(&tar.Header{Name: name, Size: size, Mode: 0644, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR, Typeflag: tar.TypeReg})
}

func writeBundle(root *os.Root, a artifacts, p persona, receipt, plan []byte, output string) error {
	if output == "" || output == "-" {
		return errors.New("a new ordinary output path is required")
	}
	parent, err := os.OpenRoot(filepath.Dir(output))
	if err != nil {
		return err
	}
	defer parent.Close()
	parentInfo, err := parent.Stat(".")
	if err != nil {
		return err
	}
	owner, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || parentInfo.Mode().Perm() != 0700 {
		return errors.New("output requires a caller-owned private directory with mode 0700")
	}
	name := filepath.Base(output)
	if !safePath(name) || strings.Contains(name, "/") {
		return errors.New("invalid output filename")
	}
	if _, err := parent.Lstat(name); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("output already exists or cannot be inspected")
	}
	// The caller exclusively owns this private directory for the operation.
	// Keep its directory handle open; parent pathname replacement cannot redirect
	// publication. This is not isolation from a hostile process with the same UID.
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".de25-pr-proof-" + hex.EncodeToString(nonce[:])
	f, err := parent.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { f.Close(); parent.Remove(temporary) }()
	w := tar.NewWriter(f)
	for _, member := range []struct {
		name string
		data []byte
	}{{"deployment.json", plan}, {"artifacts.json", receipt}} {
		if err := tarBytes(w, member.name, member.data); err != nil {
			return err
		}
	}
	for _, member := range []struct {
		name string
		blob blob
	}{{"base.rbf", a.Shell.Base}, {"slot0/persona.rbf", blob{Path: p.Path, Size: p.Size, SHA256: p.SHA256}}} {
		if err := tarHeader(w, member.name, member.blob.Size); err != nil {
			return err
		}
		if err := copyBlob(root, member.blob, w); err != nil {
			return err
		}
	}
	if err = w.Close(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return parent.Link(temporary, name)
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "compose" {
		return errors.New("usage: compose --artifacts PATH --expected-descriptor-sha256 SHA256 --expected-shell-id SHA256 --expected-base-sha256 SHA256 --persona ID --output NEW.tar")
	}
	flags := flag.NewFlagSet("compose", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifest := flags.String("artifacts", "", "compiler-produced canonical descriptor")
	expectedDescriptor := flags.String("expected-descriptor-sha256", "", "external SHA256 of the exact trusted producer descriptor")
	expectedShell := flags.String("expected-shell-id", "", "externally selected trusted shell ID")
	expectedBase := flags.String("expected-base-sha256", "", "externally selected base RBF SHA256")
	output := flags.String("output", "", "new offline deployment bundle path")
	var selections selectionFlags
	flags.Var(&selections, "persona", "one selected persona ID")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *manifest == "" {
		return errors.New("descriptor is required and positional arguments are unsupported")
	}
	absolute, err := filepath.Abs(*manifest)
	if err != nil {
		return err
	}
	for _, forbidden := range []string{"/dev", "/proc", "/sys"} {
		if absolute == forbidden || strings.HasPrefix(absolute, forbidden+"/") {
			return errors.New("device and kernel namespaces are unsupported")
		}
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer root.Close()
	receipt, err := readDescriptor(root, filepath.Base(absolute))
	if err != nil {
		return err
	}
	if !hash64.MatchString(*expectedDescriptor) || digest(receipt) != *expectedDescriptor {
		return errors.New("descriptor differs from external trusted producer selection")
	}
	a, err := parseArtifacts(receipt)
	if err != nil {
		return err
	}
	p, err := validate(a, *expectedShell, *expectedBase, selections)
	if err != nil {
		return err
	}
	// Verify the entire producer inventory, including the unselected alternative.
	if err = copyBlob(root, a.Shell.Base, io.Discard); err != nil {
		return err
	}
	for _, part := range a.Personas {
		if err = copyBlob(root, blob{part.Path, part.Size, part.SHA256}, io.Discard); err != nil {
			return err
		}
	}
	_, encoded, err := makeDeployment(a, p, receipt)
	if err != nil {
		return err
	}
	if err = writeBundle(root, a, p, receipt, encoded, *output); err != nil {
		return err
	}
	// Success is reported only after the bundle has been completely assembled.
	_, err = io.Copy(stdout, bytes.NewReader(encoded))
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "de25-pr-target-proof:", err)
		os.Exit(1)
	}
}
