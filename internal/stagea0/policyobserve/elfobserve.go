package policyobserve

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

const toolchainMaterialID = "toolchain"

// ObserveELFDependency parses the two captured Main ELF artifacts and walks
// their DT_NEEDED closure against the fork's bundled libraries and the pinned
// Arm sysroot.  It is a candidate observer: material IDs are source hints,
// not a substitute for the final immutable material/license catalog.
func ObserveELFDependency(evidence firstbuild.Evidence, artifactDir, repository, toolchainRoot string, authority policy.Authority) (policy.Document, error) {
	if artifactDir == "" || repository == "" || toolchainRoot == "" {
		return policy.Document{}, invalidInput("ELF observation roots are required")
	}
	artifactDir, err := absoluteDirectory(artifactDir)
	if err != nil {
		return policy.Document{}, err
	}
	repository, err = absoluteDirectory(repository)
	if err != nil {
		return policy.Document{}, err
	}
	toolchainRoot, err = absoluteDirectory(toolchainRoot)
	if err != nil {
		return policy.Document{}, err
	}
	roots := []dependencyRoot{
		{physical: repository, materialID: "main-fork", sourcePackage: "main-fork"},
		// The archive is the single locked toolchain material.  Keep the
		// package label descriptive, but bind every sysroot dependency to the
		// lock's canonical material ID so promotion can close the ELF graph.
		{physical: toolchainRoot, materialID: toolchainMaterialID, sourcePackage: "arm-toolchain"},
	}
	index, err := indexELFDependencies(roots)
	if err != nil {
		return policy.Document{}, err
	}
	artifacts := []struct {
		logical string
		role    string
		name    string
	}{
		{logical: "bin/MiSTer", role: "final-stripped", name: "MiSTer"},
		{logical: "bin/MiSTer.elf", role: "final-unstripped", name: "MiSTer.elf"},
	}
	elfs := make([]policy.ELFRecord, 0, len(artifacts))
	queue := make([]string, 0)
	for _, artifact := range artifacts {
		filename, err := findCapturedArtifact(artifactDir, artifact.name)
		if err != nil {
			return policy.Document{}, err
		}
		if err := matchCapturedArtifact(evidence, artifact.logical, filename); err != nil {
			return policy.Document{}, err
		}
		record, needed, err := parseELFRecord(filename, artifact.logical, artifact.role)
		if err != nil {
			return policy.Document{}, err
		}
		elfs = append(elfs, record)
		queue = append(queue, needed...)
	}

	dependencies, err := resolveDependencyClosure(queue, index)
	if err != nil {
		return policy.Document{}, err
	}
	sort.Slice(elfs, func(i, j int) bool { return elfs[i].Path < elfs[j].Path })
	document := policy.Document{
		Format:               policy.FormatV1,
		Schema:               policy.SchemaFor(policy.KindELFDependency),
		Kind:                 policy.KindELFDependency,
		Authority:            authority,
		NormalizationVersion: policy.NormalizationV1,
		ELFDependency: &policy.ELFDependencyPolicy{
			Completeness: policy.CompletenessObserved,
			ELFs:         elfs,
			Dependencies: dependencies,
		},
	}
	if err := policy.Validate(document); err != nil {
		return policy.Document{}, gitInvalid("generated ELF/dependency candidate is invalid: " + err.Error())
	}
	return document, nil
}

type dependencyRoot struct {
	physical      string
	materialID    string
	sourcePackage string
}

type dependencyCandidate struct {
	physical      string
	root          dependencyRoot
	soname        string
	alias         bool
	logical       string
	realLogical   string
	symlinkChain  []string
	absTargetPath string
	contentSHA256 string
}

func absoluteDirectory(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", invalidInput("ELF observation root is invalid")
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", invalidInput("ELF observation root must be a non-symlink directory")
	}
	return absolute, nil
}

func findCapturedArtifact(root, name string) (string, error) {
	for _, candidate := range []string{filepath.Join(root, name), filepath.Join(root, "bin", name)} {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", invalidInput("captured ELF artifact is missing: " + name)
}

func matchCapturedArtifact(evidence firstbuild.Evidence, logical, filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return invalidInput("captured ELF artifact cannot be inspected: " + logical)
	}
	hash, err := fileSHA256(filename)
	if err != nil {
		return &Failure{Code: CodeCommandFailed, Detail: "captured ELF artifact cannot be hashed: " + logical}
	}
	for _, artifact := range evidence.Output.Artifacts {
		if artifact.Path == logical {
			if artifact.Size != info.Size() || artifact.SHA256 != hash {
				return gitInvalid("captured ELF artifact differs from receipt: " + logical)
			}
			return nil
		}
	}
	return gitInvalid("receipt does not contain captured ELF artifact: " + logical)
}

func indexELFDependencies(roots []dependencyRoot) (map[string][]dependencyCandidate, error) {
	index := make(map[string][]dependencyCandidate)
	for _, root := range roots {
		err := filepath.WalkDir(root.physical, func(filename string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(filename)
			}
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			candidate, ok, err := inspectDependencyCandidate(filename, root)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			keys := []string{filepath.Base(filename)}
			if candidate.soname != "" {
				keys = append(keys, candidate.soname)
			}
			for _, key := range keys {
				index[key] = append(index[key], candidate)
			}
			return nil
		})
		if err != nil {
			return nil, &Failure{Code: CodeCommandFailed, Detail: "dependency root cannot be inspected: " + err.Error()}
		}
	}
	for key := range index {
		unique := make([]dependencyCandidate, 0, len(index[key]))
		seen := make(map[string]struct{}, len(index[key]))
		for _, candidate := range index[key] {
			identity := candidate.root.materialID + "\x00" + candidate.physical
			if _, ok := seen[identity]; ok {
				continue
			}
			seen[identity] = struct{}{}
			unique = append(unique, candidate)
		}
		index[key] = unique
		sort.Slice(index[key], func(i, j int) bool {
			if index[key][i].alias != index[key][j].alias {
				return index[key][i].alias
			}
			left := index[key][i].root.materialID + "\x00" + index[key][i].logical + "\x00" + index[key][i].physical
			right := index[key][j].root.materialID + "\x00" + index[key][j].logical + "\x00" + index[key][j].physical
			return left < right
		})
	}
	return index, nil
}

func inspectDependencyCandidate(filename string, root dependencyRoot) (dependencyCandidate, bool, error) {
	observedRoot := root
	observedRoot.materialID = materialIDFor(root, filename)
	if observedRoot.materialID != root.materialID {
		observedRoot.sourcePackage = observedRoot.materialID
	}
	file, err := elf.Open(filename)
	if err != nil {
		return dependencyCandidate{}, false, nil
	}
	defer file.Close()
	soname := ""
	if values, dynErr := file.DynString(elf.DT_SONAME); dynErr == nil && len(values) > 0 {
		soname = values[0]
	}
	if soname == "" {
		return dependencyCandidate{}, false, nil
	}
	logical, err := logicalDependencyPath(observedRoot, filename, soname)
	if err != nil {
		return dependencyCandidate{}, false, err
	}
	chain, realLogical, target, err := dependencySymlinkChain(observedRoot, filename, logical)
	if err != nil {
		return dependencyCandidate{}, false, err
	}
	if err := validateARM32(file.FileHeader); err != nil {
		return dependencyCandidate{}, false, nil
	}
	hash, err := fileSHA256(target)
	if err != nil {
		return dependencyCandidate{}, false, &Failure{Code: CodeCommandFailed, Detail: "dependency ELF cannot be hashed: " + logical}
	}
	return dependencyCandidate{physical: filename, root: observedRoot, soname: soname, alias: filepath.Base(filename) == soname, logical: logical, realLogical: realLogical, symlinkChain: chain, absTargetPath: target, contentSHA256: hash}, true, nil
}

// materialIDFor keeps bundled binary inputs separate from the Main_MiSTer
// source material. They are copied into the fork tree, but their licensing and
// corresponding-source obligations are independent of Main's own code.
func materialIDFor(root dependencyRoot, filename string) string {
	if root.materialID != "main-fork" {
		return root.materialID
	}
	relative, err := filepath.Rel(root.physical, filename)
	if err != nil {
		return root.materialID
	}
	switch filepath.ToSlash(relative) {
	case "lib/imlib2/libImlib2.so":
		return "main-fork-libimlib2"
	case "lib/imlib2/libbz2.so":
		return "main-fork-libbz2"
	case "lib/imlib2/libfreetype.so":
		return "main-fork-libfreetype"
	case "lib/imlib2/libpng16.so":
		return "main-fork-libpng16"
	case "lib/imlib2/libz.so":
		return "main-fork-libz"
	case "lib/bluetooth/libbluetooth.so":
		return "main-fork-libbluetooth"
	default:
		return root.materialID
	}
}

func logicalDependencyPath(root dependencyRoot, filename, soname string) (string, error) {
	if strings.HasPrefix(root.materialID, "main-fork") {
		return "/stage-a0/sysroot/usr/lib/" + logicalComponent(path.Base(soname)), nil
	}
	relative, err := filepath.Rel(root.physical, filename)
	if err != nil {
		return "", invalidInput("toolchain dependency path is invalid")
	}
	relative = filepath.ToSlash(relative)
	marker := "arm-none-linux-gnueabihf/libc/"
	if index := strings.Index(relative, marker); index >= 0 {
		relative = relative[index+len(marker):]
	}
	if strings.HasPrefix(relative, "lib/") || strings.HasPrefix(relative, "usr/") {
		parts := strings.Split(relative, "/")
		for i := range parts {
			parts[i] = logicalComponent(parts[i])
		}
		return "/stage-a0/sysroot/" + strings.Join(parts, "/"), nil
	}
	return "/stage-a0/sysroot/usr/lib/" + logicalComponent(path.Base(soname)), nil
}

func logicalComponent(value string) string {
	// Logical paths use the policy's portable relative-path alphabet. Keep
	// punctuation reversible instead of collapsing libstdc++ into a name that
	// does not identify the source file.
	return strings.NewReplacer("+", "_plus", "@", "_at", ":", "_colon").Replace(value)
}

func dependencySymlinkChain(root dependencyRoot, filename, logical string) ([]string, string, string, error) {
	current := filename
	chain := make([]string, 0, 2)
	var realLogical string
	for steps := 0; steps < 16; steps++ {
		if !pathWithin(root.physical, current) {
			return nil, "", "", gitInvalid("dependency symlink escapes observed root")
		}
		currentLogical, err := logicalDependencyPath(root, current, path.Base(logical))
		if err != nil {
			return nil, "", "", err
		}
		if len(chain) == 0 || chain[len(chain)-1] != currentLogical {
			chain = append(chain, currentLogical)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return nil, "", "", &Failure{Code: CodeCommandFailed, Detail: "dependency symlink cannot be inspected"}
		}
		if info.Mode()&os.ModeSymlink == 0 {
			realLogical = currentLogical
			return chain, realLogical, current, nil
		}
		target, err := os.Readlink(current)
		if err != nil {
			return nil, "", "", &Failure{Code: CodeCommandFailed, Detail: "dependency symlink cannot be read"}
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		current = filepath.Clean(target)
	}
	return nil, "", "", &Failure{Code: CodeCommandFailed, Detail: "dependency symlink chain is too deep"}
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func resolveDependencyClosure(queue []string, index map[string][]dependencyCandidate) ([]policy.DependencyRecord, error) {
	seen := make(map[string]struct{})
	records := make([]policy.DependencyRecord, 0)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		candidates := index[name]
		if len(candidates) == 0 {
			return nil, gitInvalid("dynamic dependency is not present in observed roots: " + name)
		}
		if len(candidates) > 1 {
			first := candidates[0]
			for _, candidate := range candidates[1:] {
				if candidate.root.materialID != first.root.materialID || candidate.contentSHA256 != first.contentSHA256 {
					return nil, gitInvalid(fmt.Sprintf("dynamic dependency has ambiguous observed providers: %s (%s/%s/%s/%s vs %s/%s/%s/%s)", name, first.root.materialID, first.logical, first.realLogical, first.physical, candidate.root.materialID, candidate.logical, candidate.realLogical, candidate.physical))
				}
			}
		}
		candidate := candidates[0]
		record, needed, err := dependencyRecord(candidate)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		queue = append(queue, needed...)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].LogicalPath < records[j].LogicalPath })
	return records, nil
}

func dependencyRecord(candidate dependencyCandidate) (policy.DependencyRecord, []string, error) {
	file, err := elf.Open(candidate.absTargetPath)
	if err != nil {
		return policy.DependencyRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "dependency ELF cannot be opened: " + candidate.logical}
	}
	defer file.Close()
	needed, err := file.ImportedLibraries()
	if err != nil {
		return policy.DependencyRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "dependency ELF dynamic section cannot be read: " + candidate.logical}
	}
	info, err := os.Stat(candidate.absTargetPath)
	if err != nil {
		return policy.DependencyRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "dependency ELF cannot be stat'ed: " + candidate.logical}
	}
	hash, err := fileSHA256(candidate.absTargetPath)
	if err != nil {
		return policy.DependencyRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "dependency ELF cannot be hashed: " + candidate.logical}
	}
	flags, err := elfFlags(candidate.absTargetPath, file.ByteOrder)
	if err != nil {
		return policy.DependencyRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "dependency ELF flags cannot be read: " + candidate.logical}
	}
	return policy.DependencyRecord{
		SONAME:          candidate.soname,
		LogicalPath:     candidate.logical,
		RealLogicalPath: candidate.realLogical,
		SymlinkChain:    candidate.symlinkChain,
		ABI:             elfABI(file.FileHeader, flags),
		Size:            info.Size(),
		SHA256:          hash,
		MaterialID:      candidate.root.materialID,
		SourcePackage:   candidate.root.sourcePackage,
	}, needed, nil
}

func parseELFRecord(filename, logical, role string) (policy.ELFRecord, []string, error) {
	file, err := elf.Open(filename)
	if err != nil {
		return policy.ELFRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "captured artifact is not a readable ELF: " + logical}
	}
	defer file.Close()
	if err := validateARM32(file.FileHeader); err != nil {
		return policy.ELFRecord{}, nil, gitInvalid("captured ELF header is unsupported: " + logical)
	}
	needed, err := file.ImportedLibraries()
	if err != nil {
		return policy.ELFRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "captured ELF dynamic section cannot be read: " + logical}
	}
	interpreter := ""
	if section := file.Section(".interp"); section != nil {
		data, dataErr := section.Data()
		if dataErr != nil {
			return policy.ELFRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "captured ELF interpreter cannot be read: " + logical}
		}
		interpreter = strings.TrimRight(string(data), "\x00")
	}
	programs := make([]policy.ProgramHeader, 0, len(file.Progs))
	for _, program := range file.Progs {
		programs = append(programs, policy.ProgramHeader{
			Type:            program.Type.String(),
			Offset:          hexValue(program.Off),
			VirtualAddress:  hexValue(program.Vaddr),
			PhysicalAddress: hexValue(program.Paddr),
			FileSize:        hexValue(program.Filesz),
			MemorySize:      hexValue(program.Memsz),
			Flags:           program.Flags.String(),
			Align:           hexValue(program.Align),
		})
	}
	sections := make([]policy.Section, 0, len(file.Sections))
	for _, section := range file.Sections {
		name := section.Name
		if name == "" {
			name = "<null>"
		}
		sections = append(sections, policy.Section{
			Name:      name,
			Type:      section.Type.String(),
			Address:   hexValue(section.Addr),
			Offset:    hexValue(section.Offset),
			Size:      hexValue(section.Size),
			EntrySize: hexValue(section.Entsize),
			Flags:     section.Flags.String(),
			Link:      fmt.Sprintf("%d", section.Link),
			Info:      fmt.Sprintf("%d", section.Info),
			Align:     hexValue(section.Addralign),
		})
	}
	if len(programs) == 0 || len(sections) == 0 || interpreter == "" {
		return policy.ELFRecord{}, nil, gitInvalid("captured ELF lacks required program, section, or interpreter data: " + logical)
	}
	rpath := dynamicString(file, elf.DT_RPATH)
	runpath := dynamicString(file, elf.DT_RUNPATH)
	flags, err := elfFlags(filename, file.ByteOrder)
	if err != nil {
		return policy.ELFRecord{}, nil, &Failure{Code: CodeCommandFailed, Detail: "captured ELF flags cannot be read: " + logical}
	}
	record := policy.ELFRecord{
		Path:                   logical,
		Role:                   role,
		Class:                  file.Class.String(),
		Data:                   file.Data.String(),
		Machine:                file.Machine.String(),
		OSABI:                  file.OSABI.String(),
		ABIFlags:               elfABI(file.FileHeader, flags),
		Entry:                  hexValue(file.Entry),
		Interpreter:            interpreter,
		InterpreterLogicalPath: logicalInterpreterPath(interpreter),
		ProgramHeaders:         programs,
		Sections:               sections,
		BuildID:                buildID(file),
		Needed:                 needed,
		RPath:                  rpath,
		RunPath:                runpath,
	}
	record.NormalizedReadelfSHA256 = canonicalELFHash("readelf-v1", record)
	record.NormalizedObjdumpSHA256 = canonicalELFHash("objdump-v1", record)
	queue := append([]string(nil), needed...)
	queue = append(queue, path.Base(interpreter))
	return record, queue, nil
}

func dynamicString(file *elf.File, tag elf.DynTag) string {
	values, err := file.DynString(tag)
	if err != nil || len(values) == 0 {
		return ""
	}
	return strings.Join(values, ":")
}

func logicalInterpreterPath(value string) string {
	if strings.HasPrefix(value, "/") {
		return "/stage-a0/sysroot" + value
	}
	return "/stage-a0/sysroot/" + value
}

func elfABI(header elf.FileHeader, flags uint32) string {
	if header.Machine == elf.EM_ARM {
		version := (flags >> 24) & 0xff
		if version == 5 && flags&0x400 != 0 {
			return "EABI5-hard-float"
		}
		if version == 5 {
			return "EABI5"
		}
		return fmt.Sprintf("ARM-flags-0x%x", flags)
	}
	return header.Machine.String()
}

func validateARM32(header elf.FileHeader) error {
	if header.Class != elf.ELFCLASS32 || header.Data != elf.ELFDATA2LSB || header.Machine != elf.EM_ARM {
		return fmt.Errorf("expected ARM ELF32 little-endian")
	}
	return nil
}

func elfFlags(filename string, order binary.ByteOrder) (uint32, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var raw [4]byte
	if _, err := file.ReadAt(raw[:], 36); err != nil {
		return 0, err
	}
	return order.Uint32(raw[:]), nil
}

func buildID(file *elf.File) string {
	section := file.Section(".note.gnu.build-id")
	if section == nil {
		return ""
	}
	data, err := section.Data()
	if err != nil || len(data) < 16 {
		return ""
	}
	order := file.ByteOrder
	nameSize := order.Uint32(data[0:4])
	descSize := order.Uint32(data[4:8])
	nameOffset := uint64(12)
	descOffset := nameOffset + uint64((nameSize+3)&^3)
	if descOffset+uint64(descSize) > uint64(len(data)) {
		return ""
	}
	if nameOffset+uint64(nameSize) > uint64(len(data)) || strings.TrimRight(string(data[nameOffset:nameOffset+uint64(nameSize)]), "\x00") != "GNU" {
		return ""
	}
	return hex.EncodeToString(data[descOffset : descOffset+uint64(descSize)])
}

func canonicalELFHash(prefix string, record policy.ELFRecord) string {
	record.NormalizedReadelfSHA256 = ""
	record.NormalizedObjdumpSHA256 = ""
	raw, _ := json.Marshal(record)
	digest := sha256.Sum256(append([]byte(prefix+"\n"), raw...))
	return hex.EncodeToString(digest[:])
}

func hexValue(value uint64) string { return fmt.Sprintf("0x%x", value) }

func fileSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
