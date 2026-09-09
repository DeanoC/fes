package main

import (
	"fmt"
	"os"

	"github.com/DeanoC/mister-packages/internal/emitcpp"
	"github.com/DeanoC/mister-packages/internal/emitgo"
	"github.com/DeanoC/mister-packages/internal/emitverilog"
	"github.com/DeanoC/mister-packages/internal/pack"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "mister-packages: %v\n", err)
		os.Exit(1)
	}
}

func run(command string, args []string) error {
	switch command {
	case "validate":
		path, err := inputPath(args)
		if err != nil {
			return err
		}
		id, err := validatePath(path)
		if err != nil {
			return err
		}
		fmt.Printf("ok %s\n", id)
		return nil
	case "report":
		path, err := inputPath(args)
		if err != nil {
			return err
		}
		return reportPath(path)
	case "emit-cpp":
		path, err := inputPath(args)
		if err != nil {
			return err
		}
		text, err := emitPath(path)
		if err != nil {
			return err
		}
		fmt.Print(text)
		return nil
	case "emit-go":
		path, err := inputPath(args)
		if err != nil {
			return err
		}
		text, err := emitGoPath(path)
		if err != nil {
			return err
		}
		fmt.Print(text)
		return nil
	case "emit-verilog":
		path, err := inputPath(args)
		if err != nil {
			return err
		}
		text, err := emitVerilogPath(path)
		if err != nil {
			return err
		}
		fmt.Print(text)
		return nil
	case "diff-oracle":
		if len(args) < 2 {
			return fmt.Errorf("usage: mister-packages diff-oracle <package.yaml> <oracle.yaml>")
		}
		return diffOracle(args[0], args[1])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func inputPath(args []string) (string, error) {
	if len(args) == 0 {
		return "packages/platform/de10_nano.yaml", nil
	}
	return args[0], nil
}

func validatePath(path string) (string, error) {
	kind, err := pack.PeekKind(path)
	if err != nil {
		return "", err
	}
	switch kind {
	case "platform":
		resolved, err := pack.LoadPlatform(path)
		if err != nil {
			return "", err
		}
		if _, err := resolved.Symbols(); err != nil {
			return "", err
		}
		return resolved.Platform.ID, nil
	case "system":
		sys, err := pack.LoadSystem(path)
		if err != nil {
			return "", err
		}
		return sys.ID, nil
	case "core_source":
		src, err := pack.LoadCoreSource(path)
		if err != nil {
			return "", err
		}
		return src.ID, nil
	case "abi":
		abi, err := pack.LoadABI(path)
		if err != nil {
			return "", err
		}
		return abi.ID, nil
	case "programming_profiles":
		profiles, err := pack.LoadProgrammingProfiles(path)
		if err != nil {
			return "", err
		}
		return profiles.ID, nil
	default:
		return "", fmt.Errorf("%s: kind %q is not platform, system, core_source, abi, or programming_profiles", path, kind)
	}
}

func reportPath(path string) error {
	kind, err := pack.PeekKind(path)
	if err != nil {
		return err
	}
	switch kind {
	case "platform":
		resolved, err := pack.LoadPlatform(path)
		if err != nil {
			return err
		}
		return resolved.Report(os.Stdout)
	case "system":
		sys, err := pack.LoadSystem(path)
		if err != nil {
			return err
		}
		return sys.Report(os.Stdout)
	case "core_source":
		src, err := pack.LoadCoreSource(path)
		if err != nil {
			return err
		}
		return src.Report(os.Stdout)
	default:
		return fmt.Errorf("%s: kind %q is not platform, system, or core_source", path, kind)
	}
}

func emitGoPath(path string) (string, error) {
	kind, err := pack.PeekKind(path)
	if err != nil {
		return "", err
	}
	switch kind {
	case "system":
		sys, err := pack.LoadSystem(path)
		if err != nil {
			return "", err
		}
		return emitgo.GenerateSystem(sys)
	case "abi":
		abi, err := pack.LoadABI(path)
		if err != nil {
			return "", err
		}
		return emitgo.GenerateABI(abi)
	case "programming_profiles":
		profiles, err := pack.LoadProgrammingProfiles(path)
		if err != nil {
			return "", err
		}
		return emitgo.GenerateProgrammingProfiles(profiles)
	default:
		return "", fmt.Errorf("%s: emit-go expects kind system, abi, or programming_profiles, got %q", path, kind)
	}
}

func emitPath(path string) (string, error) {
	kind, err := pack.PeekKind(path)
	if err != nil {
		return "", err
	}
	switch kind {
	case "platform":
		resolved, err := pack.LoadPlatform(path)
		if err != nil {
			return "", err
		}
		return emitcpp.Generate(resolved)
	case "system":
		sys, err := pack.LoadSystem(path)
		if err != nil {
			return "", err
		}
		return emitcpp.GenerateSystem(sys)
	case "abi":
		abi, err := pack.LoadABI(path)
		if err != nil {
			return "", err
		}
		return emitcpp.GenerateABI(abi)
	case "programming_profiles":
		profiles, err := pack.LoadProgrammingProfiles(path)
		if err != nil {
			return "", err
		}
		return emitcpp.GenerateProgrammingProfiles(profiles)
	default:
		return "", fmt.Errorf("%s: emit-cpp expects kind platform, system, abi, or programming_profiles, got %q", path, kind)
	}
}

func emitVerilogPath(path string) (string, error) {
	kind, err := pack.PeekKind(path)
	if err != nil {
		return "", err
	}
	if kind != "abi" {
		return "", fmt.Errorf("%s: emit-verilog expects kind abi, got %q", path, kind)
	}
	abi, err := pack.LoadABI(path)
	if err != nil {
		return "", err
	}
	return emitverilog.GenerateABI(abi)
}

func diffOracle(packagePath, oraclePath string) error {
	kind, err := pack.PeekKind(packagePath)
	if err != nil {
		return err
	}
	switch kind {
	case "platform":
		resolved, err := pack.LoadPlatform(packagePath)
		if err != nil {
			return err
		}
		got, err := resolved.SymbolMap()
		if err != nil {
			return err
		}
		oracle, err := pack.LoadOracle(oraclePath)
		if err != nil {
			return err
		}
		problems := pack.DiffOracle(got, oracle)
		if err := printProblems(problems); err != nil {
			return err
		}
		fmt.Printf("ok %d oracle constants (%s)\n", len(oracle.Constants), oracle.Source.Commit)
		return nil
	case "system":
		sys, err := pack.LoadSystem(packagePath)
		if err != nil {
			return err
		}
		oracle, err := pack.LoadSystemOracle(oraclePath)
		if err != nil {
			return err
		}
		problems := pack.DiffSystemOracle(sys, oracle)
		if err := printProblems(problems); err != nil {
			return err
		}
		fmt.Printf("ok %s profile (%s)\n", sys.ID, oracle.Source.Commit)
		return nil
	case "core_source":
		src, err := pack.LoadCoreSource(packagePath)
		if err != nil {
			return err
		}
		oracle, err := pack.LoadCoreSourceOracle(oraclePath)
		if err != nil {
			return err
		}
		problems := pack.DiffCoreSourceOracle(src, oracle)
		if err := printProblems(problems); err != nil {
			return err
		}
		fmt.Printf("ok %s pin (%s)\n", src.ID, oracle.Source.Commit)
		return nil
	default:
		return fmt.Errorf("%s: kind %q is not platform, system, or core_source", packagePath, kind)
	}
}

func printProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	for _, problem := range problems {
		fmt.Fprintf(os.Stderr, "%s\n", problem)
	}
	return fmt.Errorf("%d oracle mismatch(es)", len(problems))
}

func usage() {
	fmt.Fprintf(os.Stderr, `mister-packages — validate and emit hardware and system packages

Commands:
  validate [package.yaml]
  report [package.yaml]
  emit-cpp [package.yaml]
  emit-go [system.yaml|abi.yaml|programming.yaml]
  emit-verilog [abi.yaml]
  diff-oracle <package.yaml> <oracle.yaml>

Default package is packages/platform/de10_nano.yaml.
Package kind is platform, system, core_source, abi, or programming_profiles.
`)
}
