package policyobserve

import (
	"bufio"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

// ObserveCompileLink parses the command portion of a V=1 build log.  Shell
// diagnostics, scheduler order, and the sed diagnostic pipeline are removed;
// the resulting argv and ordered inputs remain candidate observations.
func ObserveCompileLink(log []byte, authority policy.Authority) (policy.Document, error) {
	records, err := parseCompileCommands(string(log))
	if err != nil {
		return policy.Document{}, err
	}
	if len(records) == 0 {
		return policy.Document{}, gitInvalid("build log contains no supported compile/link commands")
	}
	sort.Slice(records, func(i, j int) bool {
		left := records[i].Output + "\x00" + records[i].Source + "\x00" + records[i].ToolRole
		right := records[j].Output + "\x00" + records[j].Source + "\x00" + records[j].ToolRole
		return left < right
	})
	document := policy.Document{
		Format:               policy.FormatV1,
		Schema:               policy.SchemaFor(policy.KindCompileLink),
		Kind:                 policy.KindCompileLink,
		Authority:            authority,
		NormalizationVersion: policy.NormalizationV1,
		CompileLink:          &policy.CompileLinkPolicy{Completeness: policy.CompletenessObserved, Records: records},
	}
	if err := policy.Validate(document); err != nil {
		return policy.Document{}, gitInvalid("generated compile-link candidate is invalid: " + err.Error())
	}
	return document, nil
}

// ObserveGeneratedInput derives the candidate dependency-file set from the
// dependency-generation commands in a V=1 log.  It intentionally records the
// source command relationship, not an unobserved dependency-file payload.
func ObserveGeneratedInput(log []byte, inventory []firstbuild.InventoryEntry, authority policy.Authority) (policy.Document, error) {
	records, err := parseCompileCommands(string(log))
	if err != nil {
		return policy.Document{}, err
	}
	modes := inventoryModes(inventory)
	generated := make([]policy.GeneratedRecord, 0)
	for _, command := range records {
		if command.Phase != "dependency" || !strings.HasSuffix(command.Output, ".d") {
			continue
		}
		mode := modes[command.Output]
		if mode == "" {
			mode = "0644"
		}
		key := producerKey(command.Source)
		generated = append(generated, policy.GeneratedRecord{
			Path:          command.Output,
			Kind:          "compiler-dependency",
			ProducerKey:   key,
			ConsumerKeys:  []string{"compile-" + key},
			SourcePaths:   []string{command.Source},
			Normalization: "compiler-dependency-v1",
			ExpectedMode:  mode,
		})
	}
	sort.Slice(generated, func(i, j int) bool { return generated[i].Path < generated[j].Path })
	if len(generated) == 0 {
		return policy.Document{}, gitInvalid("build log contains no dependency-file commands")
	}
	document := policy.Document{
		Format:               policy.FormatV1,
		Schema:               policy.SchemaFor(policy.KindGeneratedInput),
		Kind:                 policy.KindGeneratedInput,
		Authority:            authority,
		NormalizationVersion: policy.NormalizationV1,
		GeneratedInput:       &policy.GeneratedInputPolicy{Completeness: policy.CompletenessObserved, Records: generated},
	}
	if err := policy.Validate(document); err != nil {
		return policy.Document{}, gitInvalid("generated-input candidate is invalid: " + err.Error())
	}
	return document, nil
}

// ObserveIntermediate records the complete captured bin inventory as a
// candidate path/class/mode manifest.  It does not assert that an inventory is
// the final approved set; promotion performs the exact final-artifact check.
func ObserveIntermediate(evidence firstbuild.Evidence, authority policy.Authority) (policy.Document, error) {
	records := make([]policy.IntermediateRecord, 0, len(evidence.Output.Inventory))
	for _, entry := range evidence.Output.Inventory {
		class, ok := intermediateClass(entry.Path)
		if !ok {
			return policy.Document{}, gitInvalid("captured inventory contains an unsupported intermediate path: " + entry.Path)
		}
		records = append(records, policy.IntermediateRecord{
			Path:         entry.Path,
			Class:        class,
			ProducerKey:  producerKey(entry.Path),
			ExpectedMode: entry.Mode,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	if len(records) == 0 {
		return policy.Document{}, gitInvalid("captured inventory is empty")
	}
	document := policy.Document{
		Format:               policy.FormatV1,
		Schema:               policy.SchemaFor(policy.KindIntermediatePath),
		Kind:                 policy.KindIntermediatePath,
		Authority:            authority,
		NormalizationVersion: policy.NormalizationV1,
		IntermediatePath:     &policy.IntermediatePathPolicy{Completeness: policy.CompletenessObserved, Records: records},
	}
	if err := policy.Validate(document); err != nil {
		return policy.Document{}, gitInvalid("intermediate-path candidate is invalid: " + err.Error())
	}
	return document, nil
}

func parseCompileCommands(log string) ([]policy.CompileRecord, error) {
	var records []policy.CompileRecord
	scanner := bufio.NewScanner(strings.NewReader(log))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "+ ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "+ "))
		}
		if line == "" || (!strings.HasPrefix(line, "arm-none-linux-gnueabihf-") && !strings.HasPrefix(line, "cp ")) {
			continue
		}
		if strings.HasPrefix(line, "arm-none-linux-gnueabihf-gcc (GNU Toolchain") {
			continue
		}
		if pipe := strings.Index(line, " 2>&1 |"); pipe >= 0 {
			line = line[:pipe]
		}
		argv, err := shellWords(line)
		if err != nil {
			return nil, gitInvalid("build log command quoting is invalid")
		}
		if len(argv) == 0 || argv[0] == "arm-none-linux-gnueabihf-gcc" && len(argv) == 2 && argv[1] == "--version" {
			continue
		}
		record, ok, err := compileRecord(argv)
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, &Failure{Code: CodeCommandFailed, Detail: "build log cannot be read"}
	}
	return records, nil
}

func compileRecord(argv []string) (policy.CompileRecord, bool, error) {
	if len(argv) == 0 {
		return policy.CompileRecord{}, false, nil
	}
	for i := range argv {
		if unsafeArgumentPath(argv[i]) {
			return policy.CompileRecord{}, false, gitInvalid("build command contains an unsafe path: " + argv[i])
		}
		argv[i] = normalizeArgument(argv[i])
	}
	tool, role, ok := commandTool(argv[0])
	if !ok {
		return policy.CompileRecord{}, false, nil
	}
	output := argumentAfter(argv, "-o")
	if output == "" && containsArgument(argv, "-MF") {
		output = argumentAfter(argv, "-MF")
	}
	if role == "copy" {
		if len(argv) != 3 {
			return policy.CompileRecord{}, false, gitInvalid("copy command has an unexpected shape")
		}
		output = normalizePath(argv[2])
	}
	if role == "strip" {
		if len(argv) != 2 {
			return policy.CompileRecord{}, false, gitInvalid("strip command has an unexpected shape")
		}
		output = normalizePath(argv[1])
	}
	if output == "" {
		return policy.CompileRecord{}, false, gitInvalid("build command has no output: " + strings.Join(argv, " "))
	}
	output = normalizePath(output)
	phase := "link"
	if containsArgument(argv, "-MM") {
		phase = "dependency"
	} else if containsArgument(argv, "-c") {
		phase = "compile"
	}
	source := "link"
	if phase != "link" {
		source = sourceArgument(argv)
		if source == "" {
			return policy.CompileRecord{}, false, gitInvalid("compile command has no source")
		}
		source = normalizePath(source)
	}
	if role == "copy" {
		source = normalizePath(argv[1])
		phase = "link"
	}
	if role == "strip" {
		source = normalizePath(argv[1])
		phase = "link"
	}
	inputs := orderedInputs(argv, phase, source)
	if len(inputs) == 0 {
		inputs = []string{source}
	}
	return policy.CompileRecord{
		Output:          output,
		Source:          source,
		Phase:           phase,
		ToolRole:        role,
		ToolLogicalPath: tool,
		CWD:             "/stage-a0/src",
		Argv:            argv,
		OrderedInputs:   inputs,
		Outputs:         []string{output},
	}, true, nil
}

func commandTool(command string) (string, string, bool) {
	switch command {
	case "arm-none-linux-gnueabihf-gcc":
		return "/stage-a0/toolchain/bin/arm-none-linux-gnueabihf-gcc", commandRole(command), true
	case "arm-none-linux-gnueabihf-ld":
		return "/stage-a0/toolchain/bin/arm-none-linux-gnueabihf-ld", "linker", true
	case "arm-none-linux-gnueabihf-strip":
		return "/stage-a0/toolchain/bin/arm-none-linux-gnueabihf-strip", "strip", true
	case "cp":
		return "/stage-a0/build-utils/bin/cp", "copy", true
	default:
		return "", "", false
	}
}

func commandRole(command string) string {
	if command == "arm-none-linux-gnueabihf-gcc" {
		return "compiler"
	}
	return "tool"
}

func sourceArgument(argv []string) string {
	for i := len(argv) - 1; i >= 0; i-- {
		value := argv[i]
		lower := strings.ToLower(value)
		if strings.HasPrefix(value, "-") || value == "" || value == "binary" {
			continue
		}
		for _, suffix := range []string{".c", ".cc", ".cpp", ".cxx"} {
			if strings.HasSuffix(lower, suffix) {
				return value
			}
		}
	}
	return ""
}

func orderedInputs(argv []string, phase, source string) []string {
	if phase != "link" {
		return []string{normalizePath(source)}
	}
	seen := map[string]struct{}{}
	var result []string
	skipNext := false
	for _, value := range argv {
		if skipNext {
			skipNext = false
			continue
		}
		if value == "-o" || value == "-MF" || value == "-MT" || value == "-MQ" {
			skipNext = true
			continue
		}
		value = normalizePath(value)
		if isLinkInput(value) {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	if len(result) == 0 && source != "" {
		result = []string{normalizePath(source)}
	}
	return result
}

func isLinkInput(value string) bool {
	if strings.HasPrefix(value, "-") || value == "" {
		return false
	}
	if strings.HasSuffix(value, ".o") || strings.HasSuffix(value, ".a") {
		return true
	}
	for _, suffix := range []string{".c", ".cc", ".cpp", ".cxx", ".png", ".bin", ".dat", ".img", ".rom"} {
		if strings.HasSuffix(strings.ToLower(value), suffix) {
			return true
		}
	}
	return strings.Contains(value, "/")
}

func argumentAfter(argv []string, flag string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	return ""
}

func containsArgument(argv []string, flag string) bool {
	for _, value := range argv {
		if value == flag {
			return true
		}
	}
	return false
}

func normalizeArgument(value string) string {
	value = strings.ReplaceAll(value, "\"", "")
	for _, prefix := range []string{"-I", "-L", "-include", "-imacros", "-isystem", "-o", "-MF", "-MT", "-MQ"} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
			suffix := value[len(prefix):]
			return prefix + normalizePathArgument(suffix)
		}
	}
	if value == "." || value == "./" {
		return "/stage-a0/src"
	}
	if strings.HasPrefix(value, "./") {
		return strings.TrimPrefix(path.Clean(value), "./")
	}
	if strings.Contains(value, "/./") {
		return path.Clean(value)
	}
	return value
}

func unsafeArgumentPath(value string) bool {
	if value == "" {
		return false
	}
	for _, prefix := range []string{"-I", "-L", "-include", "-imacros", "-isystem", "-o", "-MF", "-MT", "-MQ"} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
			value = value[len(prefix):]
			break
		}
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	if strings.HasPrefix(value, "/") {
		return value != "/stage-a0" && !strings.HasPrefix(value, "/stage-a0/")
	}
	return false
}

func normalizePathArgument(value string) string {
	if value == "." || value == "./" {
		return "/stage-a0/src"
	}
	if strings.HasPrefix(value, "./") {
		value = strings.TrimPrefix(value, "./")
	}
	return path.Clean(value)
}

func normalizePath(value string) string {
	value = normalizeArgument(value)
	if value == "." || value == "./" {
		return "/stage-a0/src"
	}
	return path.Clean(value)
}

func shellWords(line string) ([]string, error) {
	var words []string
	var current strings.Builder
	inSingle, inDouble, escaped, have := false, false, false, false
	flush := func() {
		if have {
			words = append(words, current.String())
			current.Reset()
			have = false
		}
	}
	for _, r := range line {
		if escaped {
			current.WriteRune(r)
			escaped = false
			have = true
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			have = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			have = true
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			have = true
			continue
		}
		if unicode.IsSpace(r) && !inSingle && !inDouble {
			flush()
			continue
		}
		current.WriteRune(r)
		have = true
	}
	if escaped || inSingle || inDouble {
		return nil, errors.New("unterminated shell word")
	}
	flush()
	return words, nil
}

func inventoryModes(entries []firstbuild.InventoryEntry) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry.Mode
	}
	return result
}

func intermediateClass(value string) (string, bool) {
	switch {
	case value == "bin/MiSTer":
		return "final-stripped", true
	case value == "bin/MiSTer.elf":
		return "final-unstripped", true
	case strings.HasSuffix(value, ".o"):
		return "object", true
	case strings.HasSuffix(value, ".d"):
		return "dependency", true
	default:
		return "", false
	}
}

func producerKey(value string) string {
	value = strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(value)
	value = strings.ToLower(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "producer"
	}
	return "producer-" + value
}
