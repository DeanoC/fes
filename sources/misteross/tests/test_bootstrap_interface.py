import hashlib
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BOOTSTRAP = ROOT / "scripts" / "bootstrap.sh"
ENV = ROOT / "scripts" / "env.sh"
LOCK_COMMITS = {
    "yosys": "fb879d81e0352f558297bdcc61bc7a4a922fa7b0",
    "mistral": "7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039",
    "nextpnr": "a93fe013af841214ecb4f7be3af0de65f3de3a0f",
    "verilator": "5e4151e3e0c8ecf11d9845a93495f37a31b2f667",
    "openfpgaloader": "0c5ebaab1fa63c9d9c684abc0b8e68546ea8ea86",
}

IDENTITY_BINARIES = {
    "yosys": "yosys",
    "mistral": "mistral-cv",
    "nextpnr": "nextpnr-mistral",
    "verilator": "verilator",
    "openfpgaloader": "openFPGALoader",
}


def _write_executable(path, body):
    path.write_text(body)
    path.chmod(0o755)


def _prepare_isolated_bootstrap(root):
    scripts = root / "scripts"
    scripts.mkdir(parents=True)
    shutil.copy2(BOOTSTRAP, scripts / "bootstrap.sh")
    shutil.copy2(ROOT / "scripts" / "lockfile.py", scripts / "lockfile.py")
    shutil.copy2(ROOT / "toolchain.lock", root / "toolchain.lock")
    (scripts / "bootstrap.sh").chmod(0o755)

    fake_bin = root / "fake-bin"
    fake_bin.mkdir()
    git_body = """#!/bin/sh
source_dir=
while [ "$#" -gt 0 ]; do
    if [ "$1" = "-C" ]; then
        source_dir="$2"
        shift 2
    else
        break
    fi
done
case "$1" in
    status|submodule) exit 0 ;;
    rev-parse)
        case "$(basename "$source_dir")" in
            yosys) commit="fb879d81e0352f558297bdcc61bc7a4a922fa7b0" ;;
            mistral) commit="7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039" ;;
            nextpnr) commit="a93fe013af841214ecb4f7be3af0de65f3de3a0f" ;;
            verilator) commit="5e4151e3e0c8ecf11d9845a93495f37a31b2f667" ;;
            openfpgaloader) commit="0c5ebaab1fa63c9d9c684abc0b8e68546ea8ea86" ;;
            *) exit 1 ;;
        esac
        printf '%s\n' "$commit"
        exit 0
        ;;
esac
exit 0
"""
    _write_executable(fake_bin / "git", git_body)
    _write_executable(
        fake_bin / "cmake",
        """#!/bin/sh
set -eu
original_args="$*"
build_dir=
gpu_router=OFF
hip_architectures=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -B) build_dir="$2"; shift 2; continue ;;
        -DGPU_ROUTER=*) gpu_router="${1#-DGPU_ROUTER=}" ;;
        -DCMAKE_HIP_ARCHITECTURES=*) hip_architectures="${1#-DCMAKE_HIP_ARCHITECTURES=}" ;;
    esac
    shift
done
if [ -n "${FAKE_CMAKE_LOG:-}" ]; then
    printf '%s\\n' "$original_args" >> "$FAKE_CMAKE_LOG"
fi
gpu_router="${FAKE_CMAKE_GPU_ROUTER:-$gpu_router}"
hip_architectures="${FAKE_CMAKE_HIP_ARCHITECTURES:-$hip_architectures}"
if [ -n "$build_dir" ]; then
    mkdir -p "$build_dir"
    printf 'GPU_ROUTER:STRING=%s\\n' "$gpu_router" > "$build_dir/CMakeCache.txt"
    if [ "$gpu_router" = HIP ]; then
        printf 'CMAKE_HIP_ARCHITECTURES:UNINITIALIZED=%s\\n' "$hip_architectures" >> "$build_dir/CMakeCache.txt"
        if [ "${FAKE_CMAKE_BAD_BACKEND:-0}" = 1 ]; then
            printf 'DEFINES = -DCPU_REFERENCE_BACKEND=1\\n' > "$build_dir/build.ninja"
        else
            printf 'DEFINES = -DNPNR_GPU_ROUTER_DEVICE=1 -D__HIP_PLATFORM_AMD__=1\\n' > "$build_dir/build.ninja"
        fi
    elif [ "$gpu_router" = CUDA ]; then
        printf 'DEFINES = -DNPNR_GPU_ROUTER_DEVICE=1 -D__CUDACC__=1\\n' > "$build_dir/build.ninja"
    else
        printf 'DEFINES = -DCPU_REFERENCE_BACKEND=1\\n' > "$build_dir/build.ninja"
    fi
fi
exit 0
""",
    )
    for command in ("ninja", "make", "autoconf"):
        _write_executable(fake_bin / command, "#!/bin/sh\nexit 0\n")

    toolchain = root / "build" / "toolchain"
    source_root = toolchain / "src"
    install_bin = toolchain / "install" / "bin"
    install_bin.mkdir(parents=True)
    for tool, commit in LOCK_COMMITS.items():
        source = source_root / tool
        (source / ".git").mkdir(parents=True)
        if tool == "verilator":
            _write_executable(source / "configure", "#!/bin/sh\nexit 0\n")
        build = toolchain / "build" / tool
        build.mkdir(parents=True)
        (build / f".built-{commit}").write_text(f"commit={commit}\n")
        binary = install_bin / IDENTITY_BINARIES[tool]
        _write_executable(binary, f"#!/bin/sh\necho fake-{tool}-identity\n")
        (build / f".identity-{commit}.txt").write_text(f"fake-{tool}-identity\n")
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        (build / f".digest-{commit}.sha256").write_text(f"{digest}\n")
        if tool == "nextpnr":
            (build / f".config-{commit}.txt").write_text(
                "gpu-router=OFF; hip-architectures=unused\n"
            )
    return fake_bin, toolchain


class BootstrapInterfaceTests(unittest.TestCase):
    def test_print_plan_lists_pins_in_lock_order_without_creating_build_tree(self):
        build_root = ROOT / "build" / "toolchain"
        before = build_root.stat() if build_root.exists() else None

        with tempfile.TemporaryDirectory() as directory:
            guard_dir = Path(directory)
            marker = guard_dir / "git-invoked"
            guard = guard_dir / "git"
            guard.write_text(f"#!/bin/sh\nprintf '%s\\n' git > {marker}\nexit 99\n")
            guard.chmod(0o755)
            result = subprocess.run(
                [str(BOOTSTRAP), "--print-plan"],
                cwd=ROOT,
                text=True,
                capture_output=True,
                env={"PATH": f"{guard_dir}:{os.environ['PATH']}"},
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(marker.exists(), result.stderr)
        output = result.stdout
        positions = []
        for tool, commit in LOCK_COMMITS.items():
            self.assertIn(commit, output)
            positions.append(output.index(f"tool: {tool}"))
        self.assertEqual(positions, sorted(positions))
        self.assertLess(output.index("tool: mistral"), output.index("tool: nextpnr"))
        self.assertIn("source:", output)
        self.assertIn("build:", output)
        self.assertIn("install:", output)
        after = build_root.stat() if build_root.exists() else None
        self.assertEqual(before is not None, after is not None)
        if before is not None and after is not None:
            self.assertEqual(before.st_mtime_ns, after.st_mtime_ns)

    def test_print_plan_honors_core_lock_root_and_gpu_overrides(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            scripts = root / "scripts"
            scripts.mkdir()
            shutil.copy2(BOOTSTRAP, scripts / "bootstrap.sh")
            shutil.copy2(ROOT / "scripts" / "lockfile.py", scripts / "lockfile.py")
            lock_path = root / "coleco.lock"
            shutil.copy2(ROOT / "toolchains/registered-memory.lock", lock_path)
            toolchain_root = root / "coleco-toolchain"
            environment = os.environ.copy()
            environment.update(
                {
                    "FES_TOOLCHAIN_LOCKFILE": str(lock_path),
                    "FES_TOOLCHAIN_ROOT": str(toolchain_root),
                    "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                    "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                }
            )
            result = subprocess.run(
                [str(scripts / "bootstrap.sh"), "--print-plan"],
                cwd=root,
                text=True,
                capture_output=True,
                env=environment,
            )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("e2d425dee148cc60c50f4e9b354a10d90eab15f4", result.stdout)
        self.assertIn(f"source: {toolchain_root}/src/yosys", result.stdout)
        self.assertIn(f"lock: {lock_path}", result.stdout)
        self.assertIn("gpu-router: HIP", result.stdout)
        self.assertIn("hip-architectures: gfx1100;gfx1201", result.stdout)

    def test_check_prereqs_reports_without_invoking_package_managers(self):
        marker = ROOT / "build" / "toolchain-prereq-test-marker"
        if marker.exists():
            marker.unlink()
        guard_dir = marker.parent / "prereq-guard-bin"
        guard_dir.mkdir(parents=True, exist_ok=True)
        for command in ("apt", "apt-get", "dnf", "pacman", "sudo"):
            guard = guard_dir / command
            guard.write_text(f"#!/bin/sh\nprintf '%s\\n' {command} > {marker}\nexit 99\n")
            guard.chmod(0o755)
        try:
            result = subprocess.run(
                [str(BOOTSTRAP), "--check-prereqs"],
                cwd=ROOT,
                text=True,
                capture_output=True,
                env={"PATH": f"{guard_dir}:{os.environ['PATH']}"},
            )
            self.assertNotEqual(result.returncode, 99)
            self.assertFalse(marker.exists(), result.stderr)
            self.assertNotIn("executing apt", result.stdout + result.stderr)
            self.assertNotIn("executing dnf", result.stdout + result.stderr)
            self.assertNotIn("executing pacman", result.stdout + result.stderr)
            self.assertNotIn("executing sudo", result.stdout + result.stderr)
        finally:
            for child in guard_dir.iterdir():
                child.unlink()
            guard_dir.rmdir()
            if marker.exists():
                marker.unlink()

    def test_check_prereqs_reports_boost_link_components(self):
        result = subprocess.run(
            [str(BOOTSTRAP), "--check-prereqs"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertIn("Boost components", result.stdout + result.stderr)

    def test_check_prereqs_requires_perl(self):
        result = subprocess.run(
            [str(BOOTSTRAP), "--check-prereqs"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertIn("perl", (result.stdout + result.stderr).lower())

    def test_check_prereqs_invokes_cmake_probes_without_repo_mutation(self):
        build_root = ROOT / "build" / "toolchain"
        before = build_root.stat() if build_root.exists() else None
        with tempfile.TemporaryDirectory() as directory:
            guard_dir = Path(directory)
            marker = guard_dir / "cmake-invocations"
            guard = guard_dir / "cmake"
            _write_executable(
                guard,
                f"#!/bin/sh\nprintf '%s\\n' \"$*\" >> {marker}\nexit 0\n",
            )
            result = subprocess.run(
                [str(BOOTSTRAP), "--check-prereqs"],
                cwd=ROOT,
                text=True,
                capture_output=True,
                env={"PATH": f"{guard_dir}:{os.environ['PATH']}"},
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            invocations = marker.read_text().splitlines()
            self.assertGreaterEqual(len(invocations), 2)
            self.assertTrue(
                all("-S" in invocation and "-B" in invocation for invocation in invocations)
            )
        after = build_root.stat() if build_root.exists() else None
        self.assertEqual(before is not None, after is not None)
        if before is not None and after is not None:
            self.assertEqual(before.st_mtime_ns, after.st_mtime_ns)

    def test_stamped_tool_with_replaced_binary_rebuilds_instead_of_skipping(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_bin, toolchain = _prepare_isolated_bootstrap(root)
            binary = toolchain / "install" / "bin" / IDENTITY_BINARIES["yosys"]
            _write_executable(binary, "#!/bin/sh\necho replaced-yosys\n")
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                text=True,
                capture_output=True,
                env={"PATH": f"{fake_bin}:{os.environ['PATH']}"},
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("==> building yosys", result.stdout)
            self.assertNotIn("==> yosys already built (identity verified)", result.stdout)
            digest = toolchain / "build" / "yosys" / f".digest-{LOCK_COMMITS['yosys']}.sha256"
            self.assertRegex(digest.read_text(), r"^[0-9a-f]{64}\n$")

    def test_stamped_tool_without_digest_rebuilds_instead_of_skipping(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_bin, toolchain = _prepare_isolated_bootstrap(root)
            digest = toolchain / "build" / "yosys" / f".digest-{LOCK_COMMITS['yosys']}.sha256"
            digest.unlink()
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                text=True,
                capture_output=True,
                env={"PATH": f"{fake_bin}:{os.environ['PATH']}"},
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("==> building yosys", result.stdout)
            self.assertNotIn("==> yosys already built (identity verified)", result.stdout)
            self.assertRegex(digest.read_text(), r"^[0-9a-f]{64}\n$")

    def test_nextpnr_cache_attestation_rebuilds_for_stale_gpu_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_bin, toolchain = _prepare_isolated_bootstrap(root)
            commit = LOCK_COMMITS["nextpnr"]
            config = toolchain / "build" / "nextpnr" / f".config-{commit}.txt"
            config.write_text("gpu-router=CUDA; hip-architectures=unused\n")
            environment = os.environ.copy()
            environment.update(
                {
                    "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                    "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                }
            )
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                text=True,
                capture_output=True,
                env={"PATH": f"{fake_bin}:{environment['PATH']}", **{
                    key: value for key, value in environment.items()
                    if key != "PATH"
                }},
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("==> building nextpnr", result.stdout)
            self.assertEqual(
                config.read_text(),
                "gpu-router=HIP; hip-architectures=gfx1100;gfx1201\n",
            )

    def test_nextpnr_configuration_attestation_rejects_mismatched_cmake_cache(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_bin, _ = _prepare_isolated_bootstrap(root)
            environment = os.environ.copy()
            environment.update(
                {
                    "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                    "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                    "FAKE_CMAKE_GPU_ROUTER": "CUDA",
                }
            )
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                text=True,
                capture_output=True,
                env={"PATH": f"{fake_bin}:{environment['PATH']}", **{
                    key: value for key, value in environment.items()
                    if key != "PATH"
                }},
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("CMake configuration or compiled backend", result.stderr)

    def test_nextpnr_configuration_attestation_rejects_mismatched_compiled_backend(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_bin, _ = _prepare_isolated_bootstrap(root)
            environment = os.environ.copy()
            environment.update(
                {
                    "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                    "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                    "FAKE_CMAKE_BAD_BACKEND": "1",
                }
            )
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                text=True,
                capture_output=True,
                env={"PATH": f"{fake_bin}:{environment['PATH']}", **{
                    key: value for key, value in environment.items()
                    if key != "PATH"
                }},
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("CMake configuration or compiled backend", result.stderr)

    def test_nextpnr_off_configuration_is_explicit(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_bin, _ = _prepare_isolated_bootstrap(root)
            cmake_log = root / "cmake.log"
            environment = os.environ.copy()
            environment["FAKE_CMAKE_LOG"] = str(cmake_log)
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                text=True,
                capture_output=True,
                env={"PATH": f"{fake_bin}:{environment['PATH']}", **{
                    key: value for key, value in environment.items()
                    if key != "PATH"
                }},
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(
                any("-DGPU_ROUTER=OFF" in line for line in cmake_log.read_text().splitlines())
            )

    def test_environment_prepends_repository_local_install_bin(self):
        command = (
            "set -e; unset OPEN_MISTER_ROOT; unset TOOLCHAIN_ROOT; "
            f"source {ENV}; "
            "printf '%s\\n' \"$OPEN_MISTER_ROOT\"; "
            "printf '%s\\n' \"${PATH%%:*}\"; "
            "printf '%s\\n' \"$PKG_CONFIG_PATH\""
        )
        result = subprocess.run(
            ["bash", "--noprofile", "--norc", "-c", command],
            cwd=ROOT,
            text=True,
            capture_output=True,
            env={"PATH": os.environ["PATH"]},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(lines[0], str(ROOT))
        self.assertEqual(lines[1], str(ROOT / "build" / "toolchain" / "install" / "bin"))
        self.assertIn(str(ROOT / "build" / "toolchain" / "install" / "lib"), lines[2])
        self.assertNotIn("quartus", result.stdout.lower() + result.stderr.lower())


if __name__ == "__main__":
    unittest.main()
