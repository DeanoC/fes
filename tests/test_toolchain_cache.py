import hashlib
import fcntl
import json
import os
import platform
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import toolchain_cache


class ToolchainCacheContractTests(unittest.TestCase):
    def _request(self, root: Path, cache: Path, recipe: Path, *, lane: str = "OFF"):
        root.mkdir(parents=True, exist_ok=True)
        cache.mkdir(parents=True, exist_ok=True)
        cache.chmod(0o700)
        scripts = root / "scripts"
        scripts.mkdir(parents=True, exist_ok=True)
        shutil.copy2(Path(toolchain_cache.__file__).resolve().parent / "lockfile.py", scripts / "lockfile.py")
        lock = root / "toolchain.lock"
        shutil.copy2(Path(toolchain_cache.__file__).resolve().parents[1] / "toolchain.lock", lock)
        return toolchain_cache.ToolchainRequest(
            root=root,
            lock_path=lock,
            cache_root=cache,
            gpu_router=lane,
            hip_architectures="gfx1100;gfx1201",
            host_identity={"os": "Linux", "arch": "x86_64", "libc": "glibc-2.39"},
            compiler_identity={"cc": "cc-test-1", "cxx": "cxx-test-1"},
            environment={"CC": "cc", "CXX": "c++", "CFLAGS": ""},
            recipe_paths=(recipe,),
        )

    def test_key_ignores_worktree_path_but_changes_lane_lock_recipe_and_host_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            recipe_a = base / "recipe-a.sh"
            recipe_b = base / "recipe-b.sh"
            recipe_a.write_text("recipe\n", encoding="utf-8")
            recipe_b.write_text("recipe\n", encoding="utf-8")
            request_a = self._request(base / "checkout-a", base / "cache", recipe_a)
            request_a.lock_path.write_text("lock-content\n", encoding="utf-8")
            request_b = self._request(base / "checkout-b", base / "cache", recipe_b)
            request_b.lock_path.write_text("lock-content\n", encoding="utf-8")

            self.assertEqual(toolchain_cache.cache_key(request_a), toolchain_cache.cache_key(request_b))
            self.assertNotEqual(
                toolchain_cache.cache_key(request_a),
                toolchain_cache.cache_key(
                    toolchain_cache.ToolchainRequest(
                        **{**request_b.__dict__, "gpu_router": "HIP"}
                    )
                ),
            )
            request_b.lock_path.write_text("different-lock\n", encoding="utf-8")
            self.assertNotEqual(toolchain_cache.cache_key(request_a), toolchain_cache.cache_key(request_b))
            request_b.lock_path.write_text("lock-content\n", encoding="utf-8")
            recipe_b.write_text("recipe-changed\n", encoding="utf-8")
            self.assertNotEqual(toolchain_cache.cache_key(request_a), toolchain_cache.cache_key(request_b))
            request_host = toolchain_cache.ToolchainRequest(
                **{**request_a.__dict__, "compiler_identity": {"cc": "cc-test-2", "cxx": "cxx-test-1"}}
            )
            self.assertNotEqual(toolchain_cache.cache_key(request_a), toolchain_cache.cache_key(request_host))

    def test_publish_and_verify_covers_library_data_and_internal_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            root.mkdir()
            recipe = root / "bootstrap.sh"
            recipe.write_text("recipe\n", encoding="utf-8")
            cache = base / "cache"
            request = self._request(root, cache, recipe)
            slot = toolchain_cache.slot_path(request)
            (slot / "src").mkdir(parents=True)
            (slot / "build" / "yosys").mkdir(parents=True)
            install_bin = slot / "install" / "bin"
            install_share = slot / "install" / "share" / "yosys"
            install_bin.mkdir(parents=True)
            install_share.mkdir(parents=True)
            (slot / "evidence").mkdir(parents=True)
            binary = install_bin / "yosys"
            binary.write_text("fake yosys\n", encoding="utf-8")
            (install_bin / "yosys-real").write_text("same target\n", encoding="utf-8")
            (install_bin / "yosys-link").symlink_to("yosys")
            (install_share / "datdir.txt").write_text("support\n", encoding="utf-8")
            evidence = slot / "evidence" / "yosys"
            evidence.mkdir()
            (evidence / ".built-pin").write_text("commit=pin\n", encoding="utf-8")
            (evidence / ".digest-pin.sha256").write_text(
                hashlib.sha256(binary.read_bytes()).hexdigest() + "\n", encoding="utf-8"
            )
            (evidence / ".identity-pin.txt").write_text("fake\n", encoding="utf-8")

            from scripts.lockfile import load_lock

            pins = load_lock(request.lock_path)
            tools = {
                "yosys": {
                    "binary": "bin/yosys",
                    "commit": pins["yosys"].commit,
                    "identity": "fake",
                }
            }
            for name, binary_name in toolchain_cache.TOOL_BINARY_NAMES.items():
                if name == "yosys":
                    continue
                binary_path = install_bin / binary_name
                binary_path.write_text(f"fake {name}\n", encoding="utf-8")
                tool_evidence = evidence / name
                tool_evidence.mkdir()
                (tool_evidence / f".built-{pins[name].commit}").write_text(
                    f"commit={pins[name].commit}\n", encoding="utf-8"
                )
                (tool_evidence / f".identity-{pins[name].commit}.txt").write_text(
                    f"fake-{name}\n", encoding="utf-8"
                )
                (tool_evidence / f".digest-{pins[name].commit}.sha256").write_text(
                    hashlib.sha256(binary_path.read_bytes()).hexdigest() + "\n", encoding="utf-8"
                )
                tools[name] = {
                    "binary": f"bin/{binary_name}",
                    "commit": pins[name].commit,
                    "identity": f"fake-{name}",
                }

            with toolchain_cache.acquire_build_lock(request):
                manifest = toolchain_cache.publish_ready(
                    request,
                    tools=tools,
                )
            self.assertEqual(manifest.install, slot / "install")
            self.assertTrue(toolchain_cache.ready_path(request).is_file())
            self.assertIn("install/share/yosys/datdir.txt", manifest.files)
            self.assertIn("install/bin/yosys-link", manifest.files)
            self.assertEqual(manifest.files["install/bin/yosys-link"]["target"], "yosys")
            self.assertFalse(stat.S_IMODE(binary.stat().st_mode) & stat.S_IWUSR)
            verified = toolchain_cache.verify_ready(request)
            self.assertEqual(verified.key, manifest.key)

    def test_verify_rejects_tampered_support_file_and_escaping_link(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            root.mkdir()
            recipe = root / "bootstrap.sh"
            recipe.write_text("recipe\n", encoding="utf-8")
            request = self._request(root, base / "cache", recipe)
            slot = toolchain_cache.slot_path(request)
            (slot / "src").mkdir(parents=True)
            (slot / "build").mkdir()
            (slot / "install" / "bin").mkdir(parents=True)
            (slot / "evidence").mkdir()
            binary = slot / "install" / "bin" / "yosys"
            binary.write_text("fake\n", encoding="utf-8")
            (slot / "evidence" / "record").write_text("evidence\n", encoding="utf-8")
            with toolchain_cache.acquire_build_lock(request):
                toolchain_cache.publish_ready(
                    request,
                    tools={"yosys": {"binary": "bin/yosys", "commit": "pin", "identity": "fake"}},
                )
            binary.chmod(0o644)
            binary.write_text("tampered\n", encoding="utf-8")
            with self.assertRaises(toolchain_cache.CacheError):
                toolchain_cache.verify_ready(request)

            # A new key/slot is needed because the previous install is frozen.
            recipe.write_text("recipe-2\n", encoding="utf-8")
            request_link = self._request(root, base / "cache", recipe)
            link_slot = toolchain_cache.slot_path(request_link)
            (link_slot / "src").mkdir(parents=True)
            (link_slot / "build").mkdir()
            (link_slot / "install" / "bin").mkdir(parents=True)
            (link_slot / "evidence").mkdir()
            (link_slot / "install" / "bin" / "escape").symlink_to("../../outside")
            with self.assertRaises(toolchain_cache.CacheError):
                with toolchain_cache.acquire_build_lock(request_link):
                    toolchain_cache.publish_ready(
                        request_link,
                        tools={"yosys": {"binary": "bin/yosys", "commit": "pin", "identity": "fake"}},
                    )

    def test_partial_slot_has_no_ready_and_missing_manifest_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            root.mkdir()
            recipe = root / "bootstrap.sh"
            recipe.write_text("recipe\n", encoding="utf-8")
            request = self._request(root, base / "cache", recipe)
            (toolchain_cache.slot_path(request) / "install").mkdir(parents=True)
            self.assertFalse(toolchain_cache.ready_path(request).exists())
            with self.assertRaises(toolchain_cache.CacheError):
                toolchain_cache.resolve_ready(request)

    def test_host_command_probe_resolves_internal_symlink_before_hashing(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            target = base / "cc.real"
            target.write_text("#!/bin/sh\nprintf 'cc test\\n'\n", encoding="utf-8")
            target.chmod(0o755)
            (base / "cc").symlink_to(target.name)
            with mock.patch.dict(os.environ, {"PATH": str(base)}, clear=False):
                identity = toolchain_cache._command_identity("cc")
            self.assertEqual(identity["path"], str(target))
            self.assertEqual(identity["sha256"], hashlib.sha256(target.read_bytes()).hexdigest())

    def test_shared_environment_rejects_nondefault_compiler_override_but_allows_lane(self):
        with self.assertRaises(toolchain_cache.CacheError):
            toolchain_cache.validate_shared_environment({"CC": "clang"})
        with self.assertRaises(toolchain_cache.CacheError):
            toolchain_cache.validate_shared_environment(
                {
                    "FES_TOOLCHAIN_CACHE_RESOLVED": "1",
                    "LD_LIBRARY_PATH": "/caller/override",
                    "PKG_CONFIG_PATH": "/caller/pkgconfig",
                    "CMAKE_PREFIX_PATH": "/caller/cmake",
                }
            )
        toolchain_cache.validate_shared_environment(
            {
                "FES_TOOLCHAIN_GPU_ROUTER": "HIP",
                "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1100;gfx1201",
                "CMAKE_HIP_ARCHITECTURES": "gfx1100;gfx1201",
            },
            gpu_router="HIP",
            hip_architectures="gfx1100;gfx1201",
        )

    def test_lane_selector_defaults_are_canonical(self):
        """Implicit and explicit defaults select one semantic cache lane."""

        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            root.mkdir()
            (root / "toolchain.lock").write_text("lock\n", encoding="utf-8")
            (root / "scripts").mkdir()
            for name in ("bootstrap.sh", "lockfile.py", "toolchain_cache.py"):
                (root / "scripts" / name).write_text(f"{name}\n", encoding="utf-8")
            cache = base / "cache"
            host = {"system": "Linux", "machine": "x86_64", "libc": "glibc-test"}
            compiler = {"commands": {"cc": {"path": "/usr/bin/cc", "sha256": "cc"}}}
            clean = {
                "PATH": os.environ.get("PATH", ""),
                "HOME": os.environ.get("HOME", str(base)),
                "USER": os.environ.get("USER", "test"),
            }
            with mock.patch.object(toolchain_cache, "host_identity", return_value=host), mock.patch.object(
                toolchain_cache, "compiler_inventory", return_value=compiler
            ), mock.patch.dict(os.environ, clean, clear=True):
                implicit_off = toolchain_cache.request_from_environment(root, cache_root=cache)
                os.environ["FES_TOOLCHAIN_GPU_ROUTER"] = "OFF"
                explicit_off = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertEqual(toolchain_cache.cache_key(implicit_off), toolchain_cache.cache_key(explicit_off))

                os.environ["FES_TOOLCHAIN_GPU_ROUTER"] = "HIP"
                os.environ.pop("FES_TOOLCHAIN_HIP_ARCHITECTURES", None)
                implicit_hip = toolchain_cache.request_from_environment(root, cache_root=cache)
                os.environ["FES_TOOLCHAIN_HIP_ARCHITECTURES"] = toolchain_cache.DEFAULT_HIP_ARCHITECTURES
                explicit_hip = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertEqual(toolchain_cache.cache_key(implicit_hip), toolchain_cache.cache_key(explicit_hip))

                os.environ["ROCM_PATH"] = "/opt/rocm-a"
                changed_hip_root = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertNotEqual(toolchain_cache.cache_key(explicit_hip), toolchain_cache.cache_key(changed_hip_root))

    def test_dependency_fixture_discards_cmake_temp_and_timing_output(self):
        """CMake's unstable log is never part of dependency identity."""

        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            bin_dir = base / "bin"
            bin_dir.mkdir()
            (bin_dir / "pkg-config").write_text(
                "#!/bin/sh\ncase \"$1\" in --modversion) echo 1.2.3;; --cflags|--libs) :;; esac\n",
                encoding="utf-8",
            )
            compiler_fixture = (
                "#!/bin/sh\n"
                "depfile=\n"
                "while [ $# -gt 0 ]; do\n"
                "  case \"$1\" in\n"
                "    -MF) depfile=$2; shift 2 ;;\n"
                "    *) shift ;;\n"
                "  esac\n"
                "done\n"
                "if [ -n \"$depfile\" ]; then printf 'probe: /etc/hosts\\n' >\"$depfile\"; else cat >/dev/null; fi\n"
                "exit 0\n"
            )
            (bin_dir / "cc").write_text(compiler_fixture, encoding="utf-8")
            (bin_dir / "c++").write_text(compiler_fixture, encoding="utf-8")
            (bin_dir / "python3-config").write_text("#!/bin/sh\necho -I/fixed/python\n", encoding="utf-8")
            (bin_dir / "cmake").write_text(
                "#!/bin/sh\n"
                "src= build=\n"
                "while [ $# -gt 0 ]; do case $1 in -S) src=$2; shift 2;; -B) build=$2; shift 2;; *) shift;; esac; done\n"
                "mkdir -p \"$build\"\n"
                "identity=$(awk -F'\\\"' '/file\\(WRITE/ {print $2; exit}' \"$src/CMakeLists.txt\")\n"
                "if grep -q 'Boost' \"$src/CMakeLists.txt\"; then probe=boost-components; else probe=eigen3; fi\n"
                "printf '%s\\nBoost_VERSION=1.83.0\\nBoost_VERSION_STRING=1.83.0\\nBoost_DIR=/fixed/boost\\nEigen3_VERSION_STRING=3.4.0\\nEigen3_DIR=/fixed/eigen\\n' \"probe=$probe\" > \"$identity\"\n"
                "echo \"Configuring done (/tmp/probe-$$, 0.$$/s)\"\n"
                "exit 0\n",
                encoding="utf-8",
            )
            for executable in bin_dir.iterdir():
                executable.chmod(0o755)
            commands = {
                "pkg-config": {"path": str(bin_dir / "pkg-config")},
                "cc": {"path": str(bin_dir / "cc")},
                "c++": {"path": str(bin_dir / "c++")},
                "python3-config": {"path": str(bin_dir / "python3-config")},
                "cmake": {"path": str(bin_dir / "cmake")},
            }
            first = toolchain_cache._dependency_inventory(commands)
            second = toolchain_cache._dependency_inventory(commands)
            self.assertEqual(first, second)
            serialized = json.dumps(first, sort_keys=True)
            self.assertNotIn("Configuring done", serialized)
            self.assertNotIn("/tmp/probe-", serialized)

    def test_dependency_inventory_fingerprints_resolved_content(self):
        """Dependency bytes, not only package metadata, select the cache lane."""

        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            bin_dir = base / "bin"
            include_dir = base / "include"
            library_dir = base / "lib"
            bin_dir.mkdir()
            include_dir.mkdir()
            library_dir.mkdir()
            headers = (
                "Python.h",
                "ffi.h",
                "readline/readline.h",
                "tcl.h",
                "zlib.h",
                "lzma.h",
                "libusb-1.0/libusb.h",
                "libftdi1/ftdi.h",
                "boost/version.hpp",
                "boost/iostreams/device/array.hpp",
                "boost/program_options.hpp",
                "boost/thread.hpp",
                "Eigen/Core",
            )
            for relative in headers:
                path = include_dir / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(f"// {relative}\n", encoding="utf-8")
            library = library_dir / "libfixture.so"
            library.write_bytes(b"fixture-v1\n")

            (bin_dir / "pkg-config").write_text(
                "#!/bin/sh\n"
                "case \"$1\" in\n"
                "  --modversion) echo 1.2.3 ;;\n"
                "  --cflags) printf '%s\\n' \"-I$FAKE_INCLUDE\" ;;\n"
                "  --libs) printf '%s\\n' \"-L$FAKE_LIB -lfixture\" ;;\n"
                "esac\n",
                encoding="utf-8",
            )
            (bin_dir / "cc").write_text(
                "#!/bin/sh\n"
                "case \"${1:-}\" in\n"
                "  -print-file-name=*) printf '%s\\n' \"$FAKE_LIB/libfixture.so\"; exit 0 ;;\n"
                "esac\n"
                "depfile= source=\n"
                "while [ $# -gt 0 ]; do\n"
                "  case \"$1\" in\n"
                "    -MF) depfile=$2; shift 2 ;;\n"
                "    -M) shift ;;\n"
                "    -MT) shift 2 ;;\n"
                "    -*) shift ;;\n"
                "    *) source=$1; shift ;;\n"
                "  esac\n"
                "done\n"
                "if [ -n \"$depfile\" ]; then\n"
                "  header=$(sed -n 's/^#include [<\\\"]\\([^>\\\"]*\\).*$/\\1/p' \"$source\" | head -n 1)\n"
                "  printf 'probe: %s\\n' \"$FAKE_INCLUDE/$header\" >\"$depfile\"\n"
                "else\n"
                "  cat >/dev/null\n"
                "fi\n",
                encoding="utf-8",
            )
            (bin_dir / "c++").write_text(
                "#!/bin/sh\n"
                "exec \"$(dirname \"$0\")/cc\" \"$@\"\n",
                encoding="utf-8",
            )
            (bin_dir / "python3-config").write_text(
                "#!/bin/sh\nprintf '%s\\n' \"-I$FAKE_INCLUDE\"\n",
                encoding="utf-8",
            )
            (bin_dir / "cmake").write_text(
                "#!/bin/sh\n"
                "src= build=\n"
                "while [ $# -gt 0 ]; do case $1 in -S) src=$2; shift 2;; -B) build=$2; shift 2;; *) shift;; esac; done\n"
                "mkdir -p \"$build\"\n"
                "identity=$(awk -F'\\\"' '/file\\(WRITE/ {print $2; exit}' \"$src/CMakeLists.txt\")\n"
                "if grep -q 'Boost' \"$src/CMakeLists.txt\"; then probe=boost-components; else probe=eigen3; fi\n"
                "printf '%s\\nBoost_VERSION=1.83.0\\nBoost_VERSION_STRING=1.83.0\\nBoost_DIR=/fixed/boost\\nEigen3_VERSION_STRING=3.4.0\\nEigen3_DIR=/fixed/eigen\\n' \"probe=$probe\" > \"$identity\"\n"
                "printf 'link-libraries=%s/libfixture.so\\n' \"$FAKE_LIB\" >> \"$identity\"\n"
                "exit 0\n",
                encoding="utf-8",
            )
            for executable in bin_dir.iterdir():
                executable.chmod(0o755)

            commands = {
                "pkg-config": {"path": str(bin_dir / "pkg-config")},
                "cc": {"path": str(bin_dir / "cc")},
                "c++": {"path": str(bin_dir / "c++")},
                "python3-config": {"path": str(bin_dir / "python3-config")},
                "cmake": {"path": str(bin_dir / "cmake")},
            }
            environment = {
                "PATH": os.environ.get("PATH", ""),
                "HOME": str(base),
                "FAKE_INCLUDE": str(include_dir),
                "FAKE_LIB": str(library_dir),
                "CPLUS_INCLUDE_PATH": str(include_dir),
                "LANG": "C",
                "LC_ALL": "C",
            }

            first = toolchain_cache._dependency_inventory(commands, environment=environment)
            first_header_paths = {
                record["path"]
                for records in first["content"]["headers"].values()
                for record in records
            }
            first_library_paths = {
                record["path"]
                for records in first["content"]["libraries"].values()
                for record in records
            }
            self.assertIn(str((include_dir / "ffi.h").resolve()), first_header_paths)
            self.assertIn(str(library.resolve()), first_library_paths)
            first_cmake_library_paths = {
                record["path"]
                for records in first["content"]["cmake-libraries"].values()
                for record in records
            }
            self.assertIn(str(library.resolve()), first_cmake_library_paths)

            recipe = base / "recipe.sh"
            recipe.write_text("recipe\n", encoding="utf-8")
            request = self._request(base / "checkout", base / "cache", recipe)
            first_request = toolchain_cache.ToolchainRequest(
                **{**request.__dict__, "compiler_identity": first}
            )

            (include_dir / "ffi.h").write_text("// changed\n", encoding="utf-8")
            library.write_bytes(b"fixture-v2\n")
            second = toolchain_cache._dependency_inventory(commands, environment=environment)
            second_request = toolchain_cache.ToolchainRequest(
                **{**request.__dict__, "compiler_identity": second}
            )

            self.assertNotEqual(first["content"], second["content"])
            self.assertNotEqual(
                toolchain_cache.cache_key(first_request),
                toolchain_cache.cache_key(second_request),
            )

    @unittest.skipUnless(
        platform.system() == "Linux" and platform.machine() == "x86_64",
        "the repeated host-probe check targets the supported powerboat lane",
    )
    def test_real_request_and_cli_key_repeat_is_stable(self):
        """Run the actual dependency probes twice, including the CLI path."""

        root = Path(__file__).resolve().parents[1]
        clean = {
            "PATH": os.environ.get("PATH", ""),
            "HOME": os.environ.get("HOME", "/tmp"),
            "USER": os.environ.get("USER", "test"),
            "LANG": "C",
            "LC_ALL": "C",
        }
        required = (
            "git",
            "cc",
            "c++",
            "cmake",
            "ninja",
            "make",
            "perl",
            "python3",
            "python3-config",
            "pkg-config",
            "autoconf",
            "flex",
            "bison",
            "help2man",
        )
        if any(shutil.which(command, path=clean["PATH"]) is None for command in required):
            self.skipTest("powerboat host tool inventory is incomplete")
        with tempfile.TemporaryDirectory(prefix="misteross-cache-key-") as directory:
            cache = Path(directory)
            with mock.patch.dict(os.environ, clean, clear=True):
                first = toolchain_cache.request_from_environment(root, cache_root=cache)
                second = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertEqual(toolchain_cache.cache_key(first), toolchain_cache.cache_key(second))

            cli_env = dict(clean)
            command = [
                str(root / "scripts" / "toolchain_cache.py"),
                "--root",
                str(root),
                "--lock",
                str(root / "toolchain.lock"),
                "--cache",
                str(cache),
                "plan",
                "--field",
                "key",
            ]
            first_cli = subprocess.run(
                ["python3", *command], env=cli_env, check=True, capture_output=True, text=True
            ).stdout.strip()
            second_cli = subprocess.run(
                ["python3", *command], env=cli_env, check=True, capture_output=True, text=True
            ).stdout.strip()
            self.assertEqual(first_cli, second_cli)
            cli_env["FES_TOOLCHAIN_GPU_ROUTER"] = "OFF"
            explicit_cli = subprocess.run(
                ["python3", *command], env=cli_env, check=True, capture_output=True, text=True
            ).stdout.strip()
            self.assertEqual(first_cli, explicit_cli)

    def test_manifest_path_is_not_relocatable(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            root.mkdir()
            recipe = root / "bootstrap.sh"
            recipe.write_text("recipe\n", encoding="utf-8")
            request = self._request(root, base / "cache", recipe)
            slot = toolchain_cache.slot_path(request)
            (slot / "src").mkdir(parents=True)
            (slot / "build").mkdir()
            (slot / "install" / "bin").mkdir(parents=True)
            (slot / "evidence").mkdir()
            (slot / "install" / "bin" / "yosys").write_text("fake\n", encoding="utf-8")
            (slot / "evidence" / "record").write_text("evidence\n", encoding="utf-8")
            with toolchain_cache.acquire_build_lock(request):
                toolchain_cache.publish_ready(
                    request,
                    tools={"yosys": {"binary": "bin/yosys", "commit": "pin", "identity": "fake"}},
                )
            copied = base / "copied"
            copied.mkdir()
            # Reuse the manifest bytes at another path, without copying the slot.
            relocated = json.loads(toolchain_cache.ready_path(request).read_text(encoding="utf-8"))
            relocated["slot"] = str(copied)
            toolchain_cache.ready_path(request).chmod(0o644)
            toolchain_cache.ready_path(request).write_text(
                json.dumps(relocated, sort_keys=True) + "\n", encoding="utf-8"
            )
            with self.assertRaises(toolchain_cache.CacheError):
                toolchain_cache.verify_ready(request)

    def test_external_publish_lock_requires_exclusive_flock(self):
        with tempfile.TemporaryDirectory() as directory:
            lock = Path(directory) / "shared.lock"
            lock.touch()
            fd = os.open(lock, os.O_RDWR)
            try:
                fcntl.flock(fd, fcntl.LOCK_SH)
                with self.assertRaises(toolchain_cache.CacheError):
                    toolchain_cache.assert_external_lock(lock, fd)
            finally:
                fcntl.flock(fd, fcntl.LOCK_UN)
                os.close(fd)


if __name__ == "__main__":
    unittest.main()
