package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/internal/targetimage"
)

const containerImage = "docker.io/library/debian:12.11-slim"

type commandRunner func(name string, args ...string) ([]byte, error)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, execute))
}

func execute(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func run(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "target-image-lock: command is required: resolve or verify-inputs")
		return 2
	}
	switch args[0] {
	case "resolve":
		return runResolve(args[1:], stdout, stderr, runner)
	case "verify-inputs":
		return runVerifyInputs(args[1:], stdout, stderr)
	case "select-core", "verify-core":
		return runCoreSelection(args[0], args[1:], stdout, stderr)
	case "select-package", "verify-package":
		return runPackageSelection(args[0], args[1:], stdout, stderr)
	case "select-megadrive":
		return runSelectMegaDrive(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "target-image-lock: unknown command %q\n", args[0])
		return 2
	}
}

func runPackageSelection(command string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	coreID := flags.String("core-id", "fes.pong", "expected package core ID")
	packageDirectory := flags.String("package", "", "sealed format-2 or format-3 package directory")
	selection := flags.String("selection", "", "closed package selection record")
	cache := flags.String("cache", "", "native package cache")
	output := flags.String("output", "", "published package selection record")
	printInputs := flags.Bool("print-inputs", false, "print canonical native image build inputs")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *packageDirectory == "" || *selection == "" {
		return 2
	}
	var err error
	if command == "select-package" {
		if *cache == "" || *output == "" || *printInputs {
			return 2
		}
		_, err = targetimage.PrepareCorePackageSelectionForCore(
			*packageDirectory, *selection, *cache, *output, *coreID)
	} else {
		if *cache != "" || *output != "" {
			return 2
		}
		var inspected targetimage.CorePackageSelection
		var selectionSHA256 string
		inspected, selectionSHA256, err = targetimage.InspectCorePackageSelectionForCore(
			*packageDirectory, *selection, *coreID)
		if err == nil && *printInputs {
			prefix := strings.ReplaceAll(*coreID, ".", "_")
			fmt.Fprintf(stdout, "%s_package_selection_sha256=%s\n", prefix, selectionSHA256)
			fmt.Fprintf(stdout, "%s_package_id=%s\n", prefix, inspected.PackageID)
			fmt.Fprintf(stdout, "%s_payload_sha256=%s\n", prefix, inspected.PayloadSHA256)
			fmt.Fprintf(stdout, "%s_misteross_revision=%s\n", prefix, inspected.MisterossRevision)
			fmt.Fprintf(stdout, "%s_mister_packages_revision=%s\n", prefix, inspected.MisterPackagesRevision)
			fmt.Fprintf(stdout, "%s_install_path=%s\n", prefix, inspected.InstallPath)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !*printInputs {
		fmt.Fprintln(stdout, command+" "+*coreID+" verified")
	}
	return 0
}

func runSelectMegaDrive(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("select-megadrive", flag.ContinueOnError)
	flags.SetOutput(stderr)
	source := flags.String("source", "", "source-built or upstream")
	bundle := flags.String("bundle", "", "sealed source-built bundle directory")
	artifact := flags.String("artifact", "", "downloaded upstream artifact")
	upstreamLock := flags.String("upstream-lock", "", "locked upstream runtime inputs")
	cache := flags.String("cache", "", "native artifact cache directory")
	output := flags.String("output", "", "normalized selection record")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *source == "" || *cache == "" || *output == "" {
		fmt.Fprintln(stderr, "target-image-lock select-megadrive: --source, --cache, and --output are required")
		return 2
	}
	if *source != "source-built" && *source != "upstream" {
		fmt.Fprintln(stderr, "target-image-lock select-megadrive: --source must be source-built or upstream")
		return 2
	}
	if (*source == "source-built" && (*bundle == "" || *artifact != "")) ||
		(*source == "upstream" && (*bundle != "" || *artifact == "" || *upstreamLock == "")) {
		fmt.Fprintln(stderr, "target-image-lock select-megadrive: source-specific flags do not match --source")
		return 2
	}
	selection, err := targetimage.PrepareMegaDriveSelection(targetimage.MegaDriveSelectionRequest{
		Source:       *source,
		Bundle:       *bundle,
		Artifact:     *artifact,
		UpstreamLock: *upstreamLock,
		Cache:        *cache,
		Output:       *output,
	})
	if err != nil {
		fmt.Fprintf(stderr, "target-image-lock select-megadrive: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "selected %s Mega Drive RBF %s\n", selection.Origin, selection.SHA256)
	return 0
}

func runResolve(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	flags := flag.NewFlagSet("resolve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	runtime := flags.String("container-runtime", "", "Docker-compatible container runtime")
	output := flags.String("output", "", "output source lock")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *runtime == "" || *output == "" {
		fmt.Fprintln(stderr, "target-image-lock resolve: --container-runtime and --output are required")
		return 2
	}
	if runner == nil {
		fmt.Fprintln(stderr, "target-image-lock resolve: container command runner is unavailable")
		return 1
	}
	result, err := runner(*runtime, "image", "inspect", "--format", "{{index .RepoDigests 0}}", containerImage)
	if err != nil {
		fmt.Fprintf(stderr, "target-image-lock resolve: inspect container: %v: %s\n", err, strings.TrimSpace(string(result)))
		return 1
	}
	digest, err := digestFromRepoDigest(string(result))
	if err != nil {
		fmt.Fprintf(stderr, "target-image-lock resolve: %v\n", err)
		return 1
	}
	lock := defaultSources(digest)
	if err := targetimage.WriteSourceLock(*output, lock); err != nil {
		fmt.Fprintf(stderr, "target-image-lock resolve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", *output)
	return 0
}

func runVerifyInputs(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-inputs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	lockPath := flags.String("lock", "", "target image source lock")
	cache := flags.String("cache", "", "target image source cache")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *lockPath == "" || *cache == "" {
		fmt.Fprintln(stderr, "target-image-lock verify-inputs: --lock and --cache are required")
		return 2
	}
	lock, err := targetimage.LoadSourceLock(*lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "target-image-lock verify-inputs: %v\n", err)
		return 1
	}
	creator := filepath.Join(*cache, "image-creator")
	files := []struct {
		name   string
		digest string
	}{
		{name: "rootfs.tar.bz2", digest: lock.ImageCreator.RootFSSHA256},
		{name: "modules.tar.gz", digest: lock.ImageCreator.ModulesSHA256},
		{name: "zImage_dtb", digest: lock.ImageCreator.KernelSHA256},
	}
	for _, file := range files {
		path := filepath.Join(creator, file.name)
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(stderr, "target-image-lock verify-inputs: %s: %v\n", file.name, err)
			return 1
		}
		if err := targetimage.VerifyFile(path, file.digest, info.Size()); err != nil {
			fmt.Fprintf(stderr, "target-image-lock verify-inputs: %s: %v\n", file.name, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "target image inputs verified")
	return 0
}

func defaultSources(containerDigest string) targetimage.Sources {
	return targetimage.Sources{
		Format: 1,
		Container: targetimage.Container{
			Image:    containerImage,
			Platform: "linux/amd64",
			Digest:   containerDigest,
		},
		Buildroot: targetimage.Buildroot{
			Version: "2021.02.4",
			Commit:  "004a792dcf10e6c474070c9571f7504411e786cc",
		},
		ImageCreator: targetimage.ImageCreator{
			Commit:        "8aba321b2162e54b56522aa30758b22d97eec8da",
			RootFSSHA256:  "65d968c566e5971debd6f822ebf13cc4555ad1bd69fa4a81fa30367f2323c344",
			ModulesSHA256: "62086a04e09b98162cc5db6d4ac607a2fdb41bbec02035f3318074a85ab9f2fe",
			KernelSHA256:  "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae",
		},
		Kernel: targetimage.Kernel{
			Commit:    "d7adb20b4ca595838289406c083fff78f004a8c3",
			Defconfig: "MiSTer_defconfig",
			DTB:       "socfpga_cyclone5_de10_nano.dtb",
			Release:   "5.15.1-MiSTer",
		},
	}
}

func digestFromRepoDigest(value string) (string, error) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndex(value, "@")
	if separator <= 0 || separator == len(value)-1 {
		return "", fmt.Errorf("container inspection did not return an immutable repository digest")
	}
	digest := value[separator+1:]
	encoded := strings.TrimPrefix(digest, "sha256:")
	decoded, err := hex.DecodeString(encoded)
	if !strings.HasPrefix(digest, "sha256:") || len(decoded) != sha256.Size || err != nil || encoded != strings.ToLower(encoded) {
		return "", fmt.Errorf("container inspection returned an invalid repository digest")
	}
	return digest, nil
}

func runCoreSelection(command string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	system := flags.String("system", "", "pong or snes")
	source := flags.String("source", "source-built", "source-built only")
	bundle := flags.String("bundle", "", "sealed bundle")
	cache := flags.String("cache", "", "artifact cache")
	output := flags.String("output", "", "selection record")
	artifact := flags.String("artifact", "", "artifact to verify")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *source != "source-built" || *system == "" || *output == "" {
		return 2
	}
	var err error
	if command == "select-core" {
		if *bundle == "" || *cache == "" || *artifact != "" {
			return 2
		}
		_, err = targetimage.PrepareCoreSelection(*system, *bundle, *cache, *output)
	} else {
		if *artifact == "" || *bundle != "" || *cache != "" {
			return 2
		}
		err = targetimage.VerifyCoreSelection(*system, *artifact, *output)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, command+" "+*system+" verified")
	return 0
}
