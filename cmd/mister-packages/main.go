package main

import (
	"fmt"
	"os"

	"github.com/DeanoC/mister-packages/internal/emitcpp"
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
		platform, err := platformPath(args)
		if err != nil {
			return err
		}
		resolved, err := pack.LoadPlatform(platform)
		if err != nil {
			return err
		}
		if _, err := resolved.Symbols(); err != nil {
			return err
		}
		fmt.Printf("ok %s\n", resolved.Platform.ID)
		return nil
	case "report":
		platform, err := platformPath(args)
		if err != nil {
			return err
		}
		resolved, err := pack.LoadPlatform(platform)
		if err != nil {
			return err
		}
		return resolved.Report(os.Stdout)
	case "emit-cpp":
		platform, err := platformPath(args)
		if err != nil {
			return err
		}
		resolved, err := pack.LoadPlatform(platform)
		if err != nil {
			return err
		}
		text, err := emitcpp.Generate(resolved)
		if err != nil {
			return err
		}
		fmt.Print(text)
		return nil
	case "diff-oracle":
		if len(args) < 2 {
			return fmt.Errorf("usage: mister-packages diff-oracle <platform.yaml> <oracle.yaml>")
		}
		resolved, err := pack.LoadPlatform(args[0])
		if err != nil {
			return err
		}
		got, err := resolved.SymbolMap()
		if err != nil {
			return err
		}
		oracle, err := pack.LoadOracle(args[1])
		if err != nil {
			return err
		}
		problems := pack.DiffOracle(got, oracle)
		if len(problems) > 0 {
			for _, problem := range problems {
				fmt.Fprintf(os.Stderr, "%s\n", problem)
			}
			return fmt.Errorf("%d oracle mismatch(es)", len(problems))
		}
		fmt.Printf("ok %d oracle constants (%s)\n", len(oracle.Constants), oracle.Source.Commit)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func platformPath(args []string) (string, error) {
	if len(args) == 0 {
		return "packages/platform/de10_nano.yaml", nil
	}
	return args[0], nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `mister-packages — validate and emit hardware packages

Commands:
  validate [platform.yaml]
  report [platform.yaml]
  emit-cpp [platform.yaml]
  diff-oracle <platform.yaml> <oracle.yaml>

Default platform is packages/platform/de10_nano.yaml.
`)
}
