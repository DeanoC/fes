import hashlib
import os
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def _write_executable(path: Path, body: str) -> None:
    if not body.startswith("#!"):
        body = "#!/bin/sh\n" + body
    path.write_text(body, encoding="utf-8")
    path.chmod(0o755)


def _prepare_fixture(
    root: Path,
    log: Path,
    *,
    yosys_commit: str = "3a5accbd78c526ab7bae32640044a2e062a3cfc9",
    nextpnr_commit: str = "9128800fa6e77c4138088a7107a95376ba47ff15",
) -> Path:
    scripts = root / "scripts"
    scripts.mkdir(parents=True)
    for name in ("bootstrap.sh", "lockfile.py", "toolchain_cache.py"):
        shutil.copy2(ROOT / "scripts" / name, scripts / name)
    (scripts / "bootstrap.sh").chmod(0o755)
    shutil.copy2(ROOT / "toolchain.lock", root / "toolchain.lock")

    fake_bin = root / "fake-bin"
    fake_bin.mkdir()
    log_literal = str(log).replace("'", "'\\''")
    common = "LOG='" + log_literal + "'\nrecord_env() { printf 'kind=%s\\n' \"$1\" >> \"$LOG\"; env | sort >> \"$LOG\"; }\n"

    _write_executable(
        fake_bin / "git",
        (
            common
            + r'''
set -eu
if [ "$1" = --version ]; then
    record_env identity
    echo 'git fake 1.0'
    exit 0
fi
source_dir=
if [ "$1" = -C ]; then
    source_dir=$2
    shift 2
fi
case "$1" in
    clone)
        record_env build
        target=
        for arg in "$@"; do target=$arg; done
        mkdir -p "$target/.git"
        if [ "$(basename "$target")" = mistral ]; then
            mkdir -p "$target/data" "$target/libmistral"
            for file in data/m10k-mux.txt libmistral/cvd-sx120f.cc libmistral/cyclonev.h; do
                printf 'database %s\n' "$file" >"$target/$file"
            done
        fi
        if [ "$(basename "$target")" = verilator ]; then
            cat >"$target/configure" <<'EOF'
#!/bin/sh
set -eu
for arg in "$@"; do
    case "$arg" in
        --prefix=*) printf '%s\n' "$arg" | sed 's/^--prefix=//' > .fake-prefix ;;
    esac
done
exit 0
EOF
            chmod 755 "$target/configure"
        fi
        ;;
    status|fetch|checkout|submodule|sync) ;;
    rev-parse)
        case "$(basename "$source_dir")" in
            yosys) echo __YOSYS_COMMIT__ ;;
            mistral) echo 8fcc4cb41c51f8918f1d3ad70def2febcbf20d8f ;;
            nextpnr) echo __NEXTPNR_COMMIT__ ;;
            verilator) echo 5e4151e3e0c8ecf11d9845a93495f37a31b2f667 ;;
            openfpgaloader) echo 0c5ebaab1fa63c9d9c684abc0b8e68546ea8ea86 ;;
            *) exit 1 ;;
        esac
        ;;
esac
'''
        ).replace("__YOSYS_COMMIT__", yosys_commit).replace(
            "__NEXTPNR_COMMIT__", nextpnr_commit),
    )

    _write_executable(
        fake_bin / "fake-install",
        common
        + r'''
set -eu
record_env build
tool=$1
prefix=$2
mkdir -p "$prefix/bin" "$prefix/share/$tool"
case "$tool" in
    yosys) binary=yosys; output=fake-yosys ;;
    mistral) binary=mistral-cv; output=fake-mistral ;;
    nextpnr) binary=nextpnr-mistral; output=fake-nextpnr ;;
    verilator) binary=verilator; output=fake-verilator ;;
    openfpgaloader) binary=openFPGALoader; output=fake-openfpgaloader ;;
    *) exit 1 ;;
esac
printf '#!/bin/sh\necho %s\n' "$output" >"$prefix/bin/$binary"
chmod 755 "$prefix/bin/$binary"
printf 'support-%s\n' "$tool" >"$prefix/share/$tool/support.txt"
''',
    )

    _write_executable(
        fake_bin / "cmake",
        common
        + r'''
set -eu
if [ "$1" = --version ]; then
    record_env identity
    echo 'cmake fake 1.0'
    exit 0
fi
if [ "$1" = --install ]; then
    record_env build
    build=$2
    prefix=$(cat "$build/.fake-prefix")
    "$(dirname "$0")/fake-install" "$(basename "$build")" "$prefix"
    exit 0
fi
src= build= prefix= router=OFF architectures= cuda_architectures=
while [ $# -gt 0 ]; do
    case "$1" in
        -S) src=$2; shift 2; continue ;;
        -B) build=$2; shift 2; continue ;;
        -DCMAKE_INSTALL_PREFIX=*) prefix=$(printf '%s' "$1" | sed 's/^-DCMAKE_INSTALL_PREFIX=//') ;;
        -DGPU_ROUTER=*) router=$(printf '%s' "$1" | sed 's/^-DGPU_ROUTER=//') ;;
        -DCMAKE_HIP_ARCHITECTURES=*) architectures=$(printf '%s' "$1" | sed 's/^-DCMAKE_HIP_ARCHITECTURES=//') ;;
        -DCMAKE_CUDA_ARCHITECTURES=*) cuda_architectures=$(printf '%s' "$1" | sed 's/^-DCMAKE_CUDA_ARCHITECTURES=//') ;;
    esac
    shift
done
case "$src" in
    *misteross-cache-probe*) ;;
    *)
        if [ -f "$(dirname "$0")/fail-build" ]; then
            record_env build
            echo 'fake build failure' >&2
            exit 42
        fi
        ;;
esac
record_env build
mkdir -p "$build"
printf '%s\n' "$prefix" >"$build/.fake-prefix"
printf 'GPU_ROUTER:STRING=%s\n' "$router" >"$build/CMakeCache.txt"
if [ "$router" = HIP ]; then
    printf 'CMAKE_HIP_ARCHITECTURES:UNINITIALIZED=%s\n' "$architectures" >>"$build/CMakeCache.txt"
    printf 'DEFINES = -DNPNR_GPU_ROUTER_DEVICE=1 -D__HIP_PLATFORM_AMD__=1\n' >"$build/build.ninja"
elif [ "$router" = CUDA ]; then
    printf 'CMAKE_CUDA_ARCHITECTURES:STRING=%s\n' "$cuda_architectures" >>"$build/CMakeCache.txt"
    printf 'DEFINES = -DNPNR_GPU_ROUTER_DEVICE=1 -D__CUDACC__=1\n' >"$build/build.ninja"
else
    printf 'DEFINES = -DCPU_REFERENCE_BACKEND=1\n' >"$build/build.ninja"
fi
if [ -f "$src/CMakeLists.txt" ]; then
    identity=$(awk -F'"' '/file\(WRITE/ {print $2; exit}' "$src/CMakeLists.txt")
    if [ -n "$identity" ]; then
        if grep -q Boost "$src/CMakeLists.txt"; then probe=boost-components; else probe=eigen3; fi
        printf 'probe=%s\nBoost_VERSION=1.83.0\nBoost_VERSION_STRING=1.83.0\nBoost_DIR=/fixed/boost\nEigen3_VERSION_STRING=3.4.0\nEigen3_DIR=/fixed/eigen\n' "$probe" >"$identity"
    fi
fi
echo "Configuring done (/tmp/probe-$$, 0.$$/s)"
''',
    )

    _write_executable(
        fake_bin / "ninja",
        common
        + r'''
set -eu
if [ "$1" = --version ]; then
    record_env identity
    echo 'ninja fake 1.0'
    exit 0
fi
build= target=
while [ $# -gt 0 ]; do
    case "$1" in
        -C) build=$2; shift 2; continue ;;
        -j*) shift; continue ;;
        install|nextpnr-mistral) target=$1 ;;
    esac
    shift
done
record_env build
if [ "$target" = install ]; then
    prefix=$(cat "$build/.fake-prefix")
    "$(dirname "$0")/fake-install" "$(basename "$build")" "$prefix"
fi
''',
    )

    _write_executable(
        fake_bin / "make",
        common
        + r'''
set -eu
if [ "$1" = --version ]; then
    record_env identity
    echo 'make fake 1.0'
    exit 0
fi
source= install_mode=0
while [ $# -gt 0 ]; do
    case "$1" in
        -C) source=$2; shift 2; continue ;;
        install) install_mode=1 ;;
    esac
    shift
done
record_env build
if [ "$install_mode" = 1 ]; then
    "$(dirname "$0")/fake-install" verilator "$(cat "$source/.fake-prefix")"
fi
''',
    )

    commands = {
        "autoconf": "#!/bin/sh\nexit 0\n",
        "perl": "#!/bin/sh\necho perl-fake\n",
        "flex": "#!/bin/sh\necho flex-fake\n",
        "bison": "#!/bin/sh\necho bison-fake\n",
        "help2man": "#!/bin/sh\necho help2man-fake\n",
        "pkg-config": "#!/bin/sh\ncase \"$1\" in --exists) exit 0;; --modversion) echo 1.0;; --cflags|--libs) :;; esac\n",
        "cc": "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then echo cc-fake; exit 0; fi\ndepfile=\nwhile [ $# -gt 0 ]; do case \"$1\" in -MF) depfile=$2; shift 2;; *) shift;; esac; done\nif [ -n \"$depfile\" ]; then printf 'probe: /etc/hosts\\n' >\"$depfile\"; else cat >/dev/null; fi\n",
        "c++": "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then echo cxx-fake; exit 0; fi\ndepfile=\nwhile [ $# -gt 0 ]; do case \"$1\" in -MF) depfile=$2; shift 2;; *) shift;; esac; done\nif [ -n \"$depfile\" ]; then printf 'probe: /etc/hosts\\n' >\"$depfile\"; else cat >/dev/null; fi\n",
        "hipcc": "#!/bin/sh\necho hipcc-fake\n",
    }
    for command, body in commands.items():
        _write_executable(fake_bin / command, common + body)
    return fake_bin


def _set_yosys_commit(lock: Path, commit: str) -> None:
    """Point a temporary lock's Yosys pin at commit. Repository locks stay put."""
    lines = lock.read_text(encoding="utf-8").splitlines(keepends=True)
    in_yosys = False
    for index, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("[tool."):
            in_yosys = stripped == "[tool.yosys]"
            continue
        if in_yosys and stripped.startswith("commit "):
            newline = "\n" if line.endswith("\n") else ""
            lines[index] = f'commit = "{commit}"{newline}'
            lock.write_text("".join(lines), encoding="utf-8")
            return
    raise AssertionError(f"no yosys commit in {lock}")


def _ready_installs(cache: Path) -> dict[str, list[Path]]:
    installs: dict[str, list[Path]] = {}
    for ready in sorted((cache / "tools").glob("*/*/ready.json")):
        installs.setdefault(ready.parent.parent.name, []).append(ready)
    return installs


def _plan(root: Path, lock: Path, cache: Path, environment: dict, *arguments: str) -> dict:
    result = subprocess.run(
        [sys.executable, str(root / "scripts" / "toolchain_cache.py"), "--root", str(root),
         "--lock", str(lock), "--cache", str(cache), *arguments, "plan"],
        cwd=root, env=environment, text=True, capture_output=True,
    )
    if result.returncode:
        raise AssertionError(result.stderr)
    return {tool: value["key"] for tool, value in json.loads(result.stdout).items()}


TOOLS = ("yosys", "mistral", "nextpnr", "verilator", "openfpgaloader")


class SharedBootstrapEnvironmentTests(unittest.TestCase):
    def test_shared_check_prereqs_reports_missing_capabilities_before_cache_planning(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(base / "cache"),
                "PYTHON_CONFIG": "missing-python-config",
            }
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh"), "--check-prereqs"],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(result.returncode, 0)
            output = result.stdout + result.stderr
            self.assertIn("Required build commands:", output)
            self.assertIn("Required development headers:", output)
            self.assertIn("[missing] Python development headers (python3-dev)", output)
            self.assertNotIn("cannot derive shared toolchain cache identity", output)

    def test_shared_build_scrubs_unknown_environment_and_handles_special_lock_path(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            cache = base / "cache with spaces $() and 'quotes'"
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
                "FAKE_COMPILER_SETTING": "caller-secret",
            }
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            installs = _ready_installs(cache)
            self.assertEqual(sorted(installs), sorted(TOOLS), result.stdout + result.stderr)
            self.assertTrue(all(len(ready) == 1 for ready in installs.values()), installs)
            yosys = installs["yosys"][0].parent
            self.assertTrue((yosys / "install" / "bin" / "yosys").is_file())
            mistral = installs["mistral"][0].parent
            self.assertTrue((mistral / "install/share/mistral/source/libmistral/cyclonev.h").is_file())
            for ready in (path for paths in installs.values() for path in paths):
                # A verified install keeps install/ and evidence/ only.
                self.assertEqual(
                    sorted(entry.name for entry in ready.parent.iterdir()),
                    ["evidence", "install", "ready.json"],
                )
            verified = subprocess.run(
                [
                    sys.executable,
                    str(root / "scripts" / "toolchain_cache.py"),
                    "--root",
                    str(root),
                    "--lock",
                    str(root / "toolchain.lock"),
                    "--cache",
                    str(cache),
                    "verify",
                ],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertEqual(verified.returncode, 0, verified.stderr)
            records = json.loads(verified.stdout)
            self.assertEqual(sorted(records), sorted(TOOLS))
            self.assertEqual(records["yosys"]["executable"], str(yosys / "install" / "bin" / "yosys"))
            self.assertEqual(records["yosys"]["binary"], "bin/yosys")
            snapshots = log.read_text(encoding="utf-8").split("kind=")
            self.assertTrue(snapshots, log.read_text(encoding="utf-8"))
            self.assertTrue(all("FAKE_COMPILER_SETTING=caller-secret" not in snapshot for snapshot in snapshots))
            builds = log.read_text(encoding="utf-8").split("kind=build\n")[1:]
            self.assertTrue(builds)
            # No tool links against another's install, so builds get no
            # library search path.
            self.assertTrue(all("LD_LIBRARY_PATH=" not in snapshot for snapshot in builds))

    def test_fresh_per_core_installs_provision_when_repo_is_exported(self):
        coleco_commit = "5391eeb1e91b38a3d0e96d04f24cf921743c9c78"
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(
                root, log, yosys_commit=coleco_commit,
                nextpnr_commit="bec82e36d436acec7cbeb248725acfde72b8f220")
            core_lock = root / "cores" / "fes-coleco" / "toolchain.lock"
            core_lock.parent.mkdir(parents=True)
            shutil.copy2(ROOT / "cores" / "fes-coleco" / "toolchain.lock", core_lock)
            # The temporary lock names the fixture Yosys commit while keeping
            # Coleco's distinct nextpnr pin from its checked-in lock.
            _set_yosys_commit(core_lock, coleco_commit)
            sibling_lock = root / "cores" / "fes-pong" / "toolchain.lock"
            sibling_lock.parent.mkdir(parents=True)
            shutil.copy2(root / "toolchain.lock", sibling_lock)
            cache = base / "cache"
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
                "FES_TOOLCHAIN_LOCKFILE": str(core_lock),
                "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                "ROCM_PATH": "/opt/rocm",
                "HIPCC": "hipcc",
                "REPO": "DeanoC/fes",
                "COMMIT": "not-a-pin",
                "ORDER": "not-a-pin",
                "TOOLS": "not-a-tool-list",
                "FAKE_COMPILER_SETTING": "caller-secret",
            }

            def plan_keys(lock: Path) -> dict:
                return _plan(root, lock, cache, environment,
                             "--gpu-router", "HIP", "--hip-architectures", "gfx1100;gfx1201")

            coleco_keys = plan_keys(core_lock)
            shared_keys = plan_keys(root / "toolchain.lock")
            sibling_keys = plan_keys(sibling_lock)
            self.assertNotEqual(coleco_keys["yosys"], shared_keys["yosys"])
            self.assertNotEqual(coleco_keys["nextpnr"], shared_keys["nextpnr"])
            # Coleco pins the same Mistral, Verilator and openFPGALoader.
            for tool in ("mistral", "verilator", "openfpgaloader"):
                self.assertEqual(coleco_keys[tool], shared_keys[tool])
            self.assertEqual(shared_keys, sibling_keys)

            built = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertEqual(built.returncode, 0, built.stdout + built.stderr)
            self.assertNotIn("unbound variable", built.stderr)
            self.assertIn(coleco_commit, built.stdout)
            installs = _ready_installs(cache)
            self.assertEqual(sorted(installs), sorted(TOOLS), built.stdout + built.stderr)
            for tool in TOOLS:
                self.assertEqual(len(installs[tool]), 1)
                ready = json.loads(installs[tool][0].read_text(encoding="utf-8"))
                self.assertEqual(ready["key"], coleco_keys[tool])
                self.assertEqual(installs[tool][0].parent.name, coleco_keys[tool])
            ready = json.loads(installs["yosys"][0].read_text(encoding="utf-8"))
            self.assertEqual(ready["record"]["commit"], coleco_commit)
            # Only the pin keys an install, never the lock's bytes.
            self.assertEqual(set(ready["build"]["pin"]), {"repo", "commit"})
            self.assertNotIn(hashlib.sha256(core_lock.read_bytes()).hexdigest(), installs["yosys"][0].read_text())
            snapshots = log.read_text(encoding="utf-8").split("kind=")
            self.assertTrue(snapshots)
            for snapshot in snapshots:
                self.assertNotIn("REPO=DeanoC/fes", snapshot)
                self.assertNotIn("FAKE_COMPILER_SETTING=caller-secret", snapshot)
                self.assertNotIn("COMMIT=not-a-pin", snapshot)
                self.assertNotIn("ORDER=not-a-pin", snapshot)
                self.assertNotIn("TOOLS=not-a-tool-list", snapshot)

    def test_shared_request_rejects_caller_search_path_before_build(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(base / "cache"),
                "CMAKE_PREFIX_PATH": str(base / "caller-prefix"),
            }
            result = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh"), "--print-plan"],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("CMAKE_PREFIX_PATH", result.stderr)
            self.assertFalse(log.exists())

    def test_hip_key_is_preserved_through_publish_with_backend_root(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            cache = base / "cache"
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
                "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                "ROCM_PATH": "/opt/rocm",
                "HIPCC": "hipcc",
            }
            plan = subprocess.run(
                [
                    sys.executable,
                    str(root / "scripts" / "toolchain_cache.py"),
                    "--root",
                    str(root),
                    "--lock",
                    str(root / "toolchain.lock"),
                    "--cache",
                    str(cache),
                    "--gpu-router",
                    "HIP",
                    "--hip-architectures",
                    "gfx1100;gfx1201",
                    "plan",
                ],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
                check=True,
            ).stdout.strip()
            plan = {tool: value["key"] for tool, value in json.loads(plan).items()}
            built = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertEqual(built.returncode, 0, built.stderr)
            installs = _ready_installs(cache)
            self.assertEqual(
                {tool: json.loads(paths[0].read_text(encoding="utf-8"))["key"] for tool, paths in installs.items()},
                plan,
            )

    def test_shared_build_serializes_same_key(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            cache = base / "cache"
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
                "FAKE_COMPILER_SETTING": "caller-secret",
            }
            processes = [
                subprocess.Popen(
                    [str(root / "scripts" / "bootstrap.sh")],
                    cwd=root,
                    env=environment,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                )
                for _ in range(2)
            ]
            results = [process.communicate(timeout=60) for process in processes]
            self.assertTrue(all(process.returncode == 0 for process in processes), results)
            output = "\n".join(stdout for stdout, _ in results)
            for tool in TOOLS:
                self.assertEqual(output.count(f"shared {tool} install published"), 1, output)
                self.assertEqual(output.count(f"shared {tool} install hit"), 1, output)

    def test_shared_cache_hit_is_reusable_from_another_worktree(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root_a = base / "checkout-a"
            root_b = base / "checkout-b"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root_a, log)
            scripts = root_b / "scripts"
            scripts.mkdir(parents=True)
            for name in ("bootstrap.sh", "lockfile.py", "toolchain_cache.py"):
                shutil.copy2(ROOT / "scripts" / name, scripts / name)
            (scripts / "bootstrap.sh").chmod(0o755)
            shutil.copy2(ROOT / "toolchain.lock", root_b / "toolchain.lock")
            cache = base / "cache"
            common = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
            }
            first = subprocess.run(
                [str(root_a / "scripts" / "bootstrap.sh")],
                cwd=root_a,
                env=common,
                text=True,
                capture_output=True,
            )
            self.assertEqual(first.returncode, 0, first.stderr)
            second = subprocess.run(
                [str(root_b / "scripts" / "bootstrap.sh")],
                cwd=root_b,
                env=common,
                text=True,
                capture_output=True,
            )
            self.assertEqual(second.returncode, 0, second.stderr)
            for tool in TOOLS:
                self.assertIn(f"shared {tool} install hit", second.stdout)
            self.assertEqual(sum(len(paths) for paths in _ready_installs(cache).values()), len(TOOLS))

    def test_failed_shared_build_is_rebuilt_from_scratch(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            (fake_bin / "fail-build").touch()
            cache = base / "cache"
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
            }
            first = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(first.returncode, 0)
            partial = list((cache / "tools").glob("*/*/build"))
            self.assertEqual(len(partial), 1)
            self.assertFalse((partial[0].parent / "ready.json").exists())
            # Under the install's exclusive lock a directory without
            # ready.json can only be a failed build; it is removed and rebuilt.
            (fake_bin / "fail-build").unlink()
            second = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertEqual(second.returncode, 0, second.stdout + second.stderr)
            self.assertIn("removing failed", second.stdout)
            self.assertTrue((partial[0].parent / "ready.json").is_file())
            self.assertFalse(partial[0].exists())

    def test_unlocked_publish_cli_is_denied(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            log = base / "child-env.log"
            fake_bin = _prepare_fixture(root, log)
            cache = base / "cache"
            environment = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                "HOME": str(base),
                "USER": "test",
                "LANG": "C",
                "LC_ALL": "C",
                "FES_TOOLCHAIN_CACHE_ROOT": str(cache),
            }
            built = subprocess.run(
                [str(root / "scripts" / "bootstrap.sh")],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertEqual(built.returncode, 0, built.stderr)
            result = subprocess.run(
                [
                    sys.executable,
                    str(root / "scripts" / "toolchain_cache.py"),
                    "--root",
                    str(root),
                    "--lock",
                    str(root / "toolchain.lock"),
                    "--cache",
                    str(cache),
                    "--tool",
                    "yosys",
                    "publish",
                ],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("per-install build lock", result.stderr)
