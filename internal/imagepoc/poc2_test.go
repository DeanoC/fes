package imagepoc_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/imagepoc"
)

const validPOC2 = `format = 1

[base]
poc1a_lock_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
poc1b_lock_sha256 = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
accepted_dev_root_sha256 = 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
accepted_kernel_sha256 = 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'

[outputs]
prod_rootfs_sha256 = 'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee'
dev_rootfs_sha256 = 'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'
`

func TestLoadPOC2RejectsMalformedOrFutureLocks(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unknown top-level field": validPOC2 + "future = true\n",
		"unknown base field": strings.Replace(validPOC2,
			"accepted_kernel_sha256 =", "future = 'value'\naccepted_kernel_sha256 =", 1),
		"future format":  strings.Replace(validPOC2, "format = 1", "format = 2", 1),
		"zero format":    strings.Replace(validPOC2, "format = 1", "format = 0", 1),
		"uppercase hash": strings.Replace(validPOC2, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
		"short hash":     strings.Replace(validPOC2, strings.Repeat("b", 64), strings.Repeat("b", 63), 1),
		"missing POC 1A base": strings.Replace(validPOC2,
			"poc1a_lock_sha256 = '"+strings.Repeat("a", 64)+"'\n", "", 1),
		"missing POC 1B base": strings.Replace(validPOC2,
			"poc1b_lock_sha256 = '"+strings.Repeat("b", 64)+"'\n", "", 1),
		"missing accepted dev root": strings.Replace(validPOC2,
			"accepted_dev_root_sha256 = '"+strings.Repeat("c", 64)+"'\n", "", 1),
		"missing accepted kernel": strings.Replace(validPOC2,
			"accepted_kernel_sha256 = '"+strings.Repeat("d", 64)+"'\n", "", 1),
		"missing production output": strings.Replace(validPOC2,
			"prod_rootfs_sha256 = '"+strings.Repeat("e", 64)+"'\n", "", 1),
		"missing development output": strings.Replace(validPOC2,
			"dev_rootfs_sha256 = '"+strings.Repeat("f", 64)+"'\n", "", 1),
	}
	for name, content := range tests {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := imagepoc.LoadPOC2(writeFixture(t, "poc2.toml", content)); err == nil {
				t.Fatal("invalid POC 2 lock loaded")
			}
		})
	}
}

func TestRecordPOC2WritesStableAtomicLockAndVerifyIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	poc1a, poc1b, prod, dev, kernel := writePOC2Inputs(t, dir)
	output := filepath.Join(dir, "outputs.poc2.lock.toml")
	if err := imagepoc.RecordPOC2(poc1a, poc1b, prod, dev, output); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := imagepoc.RecordPOC2(poc1a, poc1b, prod, dev, output); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || !strings.HasSuffix(string(first), "\n") {
		t.Fatalf("unstable output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if info, err := os.Stat(output); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("lock mode = %v, err = %v", info.Mode().Perm(), err)
	}

	lock, err := imagepoc.LoadPOC2(output)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Base.POC1ALockSHA256 != fileSHA256(t, poc1a) ||
		lock.Base.POC1BLockSHA256 != fileSHA256(t, poc1b) ||
		lock.Base.AcceptedDevRootSHA256 != "e038679bc82623b2911ef0e3876233ed95c6b3d546e77320cd6db2992647faa7" ||
		lock.Base.AcceptedKernelSHA256 != fileSHA256(t, kernel) ||
		lock.Outputs.ProdRootFSSHA256 != "6754af9632a2745e85c293e5aac0863370d9bd3330b9938c00cadfd215227d77" ||
		lock.Outputs.DevRootFSSHA256 != "ef260e9aa3c673af240d17a2660480361a8e081d1ffeca2a5ed0e3219fc18567" {
		t.Fatalf("recorded lock = %#v", lock)
	}
	if err := imagepoc.VerifyPOC2(output, poc1a, poc1b, prod, dev); err != nil {
		t.Fatal(err)
	}
	afterVerify, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterVerify) != string(first) {
		t.Fatal("verify rewrote the output lock")
	}
}

func TestVerifyPOC2RejectsChangedBaseLockKernelAndOutputsWithoutRewriting(t *testing.T) {
	tests := map[string]func(t *testing.T, poc1a, poc1b, prod, dev, kernel string){
		"POC 1A lock": func(t *testing.T, poc1a, _, _, _, _ string) {
			if err := os.WriteFile(poc1a, []byte("changed base lock\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"POC 1B lock": func(t *testing.T, _, poc1b, _, _, _ string) {
			if err := os.WriteFile(poc1b, []byte("changed source lock\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"kernel": func(t *testing.T, _, _, _, _, kernel string) {
			if err := os.WriteFile(kernel, []byte("changed kernel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"production root": func(t *testing.T, _, _, prod, _, _ string) {
			if err := os.WriteFile(prod, []byte("changed prod\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"development root": func(t *testing.T, _, _, _, dev, _ string) {
			if err := os.WriteFile(dev, []byte("changed dev\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			poc1a, poc1b, prod, dev, kernel := writePOC2Inputs(t, dir)
			output := filepath.Join(dir, "outputs.poc2.lock.toml")
			if err := imagepoc.RecordPOC2(poc1a, poc1b, prod, dev, output); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			mutate(t, poc1a, poc1b, prod, dev, kernel)
			if err := imagepoc.VerifyPOC2(output, poc1a, poc1b, prod, dev); err == nil {
				t.Fatal("changed provenance input verified")
			}
			after, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("failed verify rewrote the lock")
			}
		})
	}
}

func TestRecordPOC2RejectsKernelThatDiffersFromAcceptedPOC1BOutput(t *testing.T) {
	dir := t.TempDir()
	poc1a, poc1b, prod, dev, kernel := writePOC2Inputs(t, dir)
	if err := os.WriteFile(kernel, []byte("wrong-kernel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "outputs.poc2.lock.toml")
	if err := imagepoc.RecordPOC2(poc1a, poc1b, prod, dev, output); err == nil {
		t.Fatal("record accepted a kernel other than the accepted POC 1B output")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed record created output: %v", err)
	}
}

func TestPOC2CommittedLockContainsRecordedValues(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..")
	lockPath := filepath.Join(repo, "build", "outputs.poc2.lock.toml")
	lock, err := imagepoc.LoadPOC2(lockPath)
	if err != nil {
		t.Fatalf("POC 2 output lock is unavailable: %v", err)
	}
	if lock.Base.POC1ALockSHA256 != "8ef39d9d603c1a7a7bd20550f8f7c05dfb05d770509065c4f3e3663e81430d99" ||
		lock.Base.POC1BLockSHA256 != "8b395f61bdab5c9807ded401279eabfa29df20eaf52c03eb6954b9d7dc2641af" ||
		lock.Base.AcceptedDevRootSHA256 != "e038679bc82623b2911ef0e3876233ed95c6b3d546e77320cd6db2992647faa7" ||
		lock.Base.AcceptedKernelSHA256 != "cb66e22edb04a44d883e82f62fa7eeca0d7d2b715b08ab72ad2dc5bc2a3178c5" ||
		lock.Outputs.ProdRootFSSHA256 != "35fa654326d2c2722e8c2dae81e463750d5cb0ee1f2fbf0c42ea3909860a1bc6" ||
		lock.Outputs.DevRootFSSHA256 != "3e66d1bba5aeda791b238aa549fbb55c15d06ff30152fdfd29bef5359cd08daa" {
		t.Fatalf("committed lock = %#v", lock)
	}
}

func writePOC2Inputs(t *testing.T, dir string) (poc1a, poc1b, prod, dev, kernel string) {
	t.Helper()
	poc1a = filepath.Join(dir, "sources.poc1a.lock.toml")
	poc1b = filepath.Join(dir, "sources.poc1b.lock.toml")
	prod = writePOC2File(t, dir, "prod.img", "prod")
	dev = writePOC2File(t, dir, "dev.img", "dev")
	kernel = writePOC2File(t, filepath.Join(dir, "output", "poc1b", "kernel"), "zImage_dtb", "kernel")
	if err := os.WriteFile(poc1a, []byte(`format = 1

[[artifacts]]
name = "main"
path = "/media/fat/MiSTer"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
size = 1
source = "fixture"

[runtime]
kernel_release = "5.15.1-MiSTer"

[[libraries]]
path = "/lib/libc.so.6"
resolved_path = "/usr/lib/libc.so.6"
sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
size = 1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	kernelSHA := fileSHA256(t, kernel)
	poc1bContent := strings.Replace(validPOC1B, "[kernel]", `[outputs]
prod_rootfs_sha256 = 'dbcf1cddfbd50b949b423ab7e5a09f478539ee400858295033c84d73aa023be2'
dev_rootfs_sha256 = 'e038679bc82623b2911ef0e3876233ed95c6b3d546e77320cd6db2992647faa7'
reproduced_kernel_sha256 = '`+kernelSHA+`'

[kernel]`, 1)
	if err := os.WriteFile(poc1b, []byte(poc1bContent), 0o600); err != nil {
		t.Fatal(err)
	}
	return poc1a, poc1b, prod, dev, kernel
}

func writePOC2File(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
