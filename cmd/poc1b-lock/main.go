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

	"github.com/DeanoC/FogCast/internal/imagepoc"
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
		fmt.Fprintln(stderr, "poc1b-lock: command is required: resolve, verify-inputs, or record-outputs")
		return 2
	}
	switch args[0] {
	case "resolve":
		return runResolve(args[1:], stdout, stderr, runner)
	case "verify-inputs":
		return runVerifyInputs(args[1:], stdout, stderr)
	case "record-outputs":
		return runRecordOutputs(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "poc1b-lock: unknown command %q\n", args[0])
		return 2
	}
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
		fmt.Fprintln(stderr, "poc1b-lock resolve: --container-runtime and --output are required")
		return 2
	}
	if runner == nil {
		fmt.Fprintln(stderr, "poc1b-lock resolve: container command runner is unavailable")
		return 1
	}
	result, err := runner(*runtime, "image", "inspect", "--format", "{{index .RepoDigests 0}}", containerImage)
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock resolve: inspect container: %v: %s\n", err, strings.TrimSpace(string(result)))
		return 1
	}
	digest, err := digestFromRepoDigest(string(result))
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock resolve: %v\n", err)
		return 1
	}
	lock := defaultSources(digest)
	if err := imagepoc.WritePOC1B(*output, lock); err != nil {
		fmt.Fprintf(stderr, "poc1b-lock resolve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", *output)
	return 0
}

func runVerifyInputs(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-inputs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	lockPath := flags.String("lock", "", "POC 1B source lock")
	cache := flags.String("cache", "", "POC 1B source cache")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *lockPath == "" || *cache == "" {
		fmt.Fprintln(stderr, "poc1b-lock verify-inputs: --lock and --cache are required")
		return 2
	}
	lock, err := imagepoc.LoadPOC1B(*lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock verify-inputs: %v\n", err)
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
			fmt.Fprintf(stderr, "poc1b-lock verify-inputs: %s: %v\n", file.name, err)
			return 1
		}
		if err := imagepoc.VerifyFile(path, file.digest, info.Size()); err != nil {
			fmt.Fprintf(stderr, "poc1b-lock verify-inputs: %s: %v\n", file.name, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "POC 1B image inputs verified")
	return 0
}

func runRecordOutputs(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("record-outputs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	lockPath := flags.String("lock", "", "POC 1B source lock")
	prod := flags.String("prod", "", "production root image")
	dev := flags.String("dev", "", "development root image")
	kernel := flags.String("kernel", "", "reproduced zImage_dtb")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *lockPath == "" || *prod == "" || *dev == "" || *kernel == "" {
		fmt.Fprintln(stderr, "poc1b-lock record-outputs: --lock, --prod, --dev, and --kernel are required")
		return 2
	}
	lock, err := imagepoc.LoadPOC1B(*lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock record-outputs: %v\n", err)
		return 1
	}
	prodDigest, err := fileDigest(*prod)
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock record-outputs: prod: %v\n", err)
		return 1
	}
	devDigest, err := fileDigest(*dev)
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock record-outputs: dev: %v\n", err)
		return 1
	}
	kernelDigest, err := fileDigest(*kernel)
	if err != nil {
		fmt.Fprintf(stderr, "poc1b-lock record-outputs: kernel: %v\n", err)
		return 1
	}
	lock.Outputs = &imagepoc.Outputs{
		ProdRootFSSHA256:       prodDigest,
		DevRootFSSHA256:        devDigest,
		ReproducedKernelSHA256: kernelDigest,
	}
	if err := imagepoc.WritePOC1B(*lockPath, lock); err != nil {
		fmt.Fprintf(stderr, "poc1b-lock record-outputs: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "recorded outputs in %s\n", *lockPath)
	return 0
}

func defaultSources(containerDigest string) imagepoc.Sources {
	return imagepoc.Sources{
		Format: 1,
		Container: imagepoc.Container{
			Image:    containerImage,
			Platform: "linux/amd64",
			Digest:   containerDigest,
		},
		Buildroot: imagepoc.Buildroot{
			Version: "2021.02.4",
			Commit:  "004a792dcf10e6c474070c9571f7504411e786cc",
		},
		ImageCreator: imagepoc.ImageCreator{
			Commit:        "8aba321b2162e54b56522aa30758b22d97eec8da",
			RootFSSHA256:  "65d968c566e5971debd6f822ebf13cc4555ad1bd69fa4a81fa30367f2323c344",
			ModulesSHA256: "62086a04e09b98162cc5db6d4ac607a2fdb41bbec02035f3318074a85ab9f2fe",
			KernelSHA256:  "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae",
		},
		Kernel: imagepoc.Kernel{
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

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", fmt.Errorf("not a nonempty regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
