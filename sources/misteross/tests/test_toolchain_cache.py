import hashlib
import fcntl
import io
import json
import os
import platform
import re
import shutil
import stat
import subprocess
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from scripts import toolchain_cache

SOURCE = Path(toolchain_cache.__file__).resolve().parents[1]
HOST = {"system": "Linux", "machine": "x86_64", "libc": "glibc-test"}
COMPILER = {"commands": {"cc": {"path": "/usr/bin/cc", "sha256": "cc"}}}


def _set_pin(lock: Path, tool: str, field: str, value: str) -> None:
    text, count = re.subn(
        rf'(\[tool\.{tool}\][^\[]*?{field} = ")[^"]*"', lambda match: f'{match.group(1)}{value}"',
        lock.read_text(encoding="utf-8"),
    )
    assert count == 1, (tool, field)
    lock.write_text(text, encoding="utf-8")


def _publish(request: toolchain_cache.ToolchainRequest, tool: str, *, content: str = "fake") -> toolchain_cache.InstallManifest:
    """Publish a fake install of *tool* through the real cache API."""

    directory = toolchain_cache.install_directory(request, tool)
    (directory / "install" / "bin").mkdir(parents=True)
    (directory / "evidence").mkdir()
    (directory / "install" / "bin" / toolchain_cache.TOOL_BINARY_NAMES[tool]).write_text(
        f"{content} {tool}\n", encoding="utf-8"
    )
    (directory / "evidence" / "record").write_text("evidence\n", encoding="utf-8")
    if tool == "mistral":
        database = directory / "install" / toolchain_cache.MISTRAL_DATABASE
        database.mkdir(parents=True)
        (database / "fixture").write_text("database\n", encoding="utf-8")
    with toolchain_cache.acquire_build_lock(request, tool):
        return toolchain_cache.publish_install(request, tool, identity=f"{content} {tool}")


class ToolchainCacheContractTests(unittest.TestCase):
    def _request(self, root: Path, cache: Path, *, lane: str = "OFF", **fields):
        """A request over a checkout holding the real lock parser, recipe and lock."""

        scripts = root / "scripts"
        if not scripts.exists():
            scripts.mkdir(parents=True)
            shutil.copy2(SOURCE / "scripts" / "lockfile.py", scripts / "lockfile.py")
            shutil.copy2(SOURCE / "scripts" / "bootstrap.sh", scripts / "bootstrap.sh")
            shutil.copy2(SOURCE / "toolchain.lock", root / "toolchain.lock")
        cache.mkdir(parents=True, exist_ok=True)
        cache.chmod(0o700)
        values = {
            "root": root,
            "lock_path": root / "toolchain.lock",
            "cache_root": cache,
            "gpu_router": lane,
            "hip_architectures": "gfx1100;gfx1201",
            "host_identity": HOST,
            "compiler_identity": COMPILER,
            "environment": {},
        }
        values.update(fields)
        return toolchain_cache.ToolchainRequest(**values)

    def test_key_ignores_worktree_path_and_rationale_but_follows_pins_and_host(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request_a = self._request(base / "checkout-a", base / "cache")
            request_b = self._request(base / "checkout-b", base / "cache")
            keys = toolchain_cache.tool_keys(request_a)
            self.assertEqual(keys, toolchain_cache.tool_keys(request_b))
            self.assertEqual(len(set(keys.values())), len(toolchain_cache.TOOLS))

            # Rationale prose, order and comments are not pins.
            lock = request_b.lock_path
            _set_pin(lock, "yosys", "rationale", "Rewritten rationale text.")
            lock.write_text("# a comment\n" + lock.read_text(encoding="utf-8"), encoding="utf-8")
            self.assertEqual(keys, toolchain_cache.tool_keys(request_b))

            # A pin moves only its own install (and nextpnr's, for Mistral).
            _set_pin(lock, "yosys", "commit", "a" * 40)
            moved = toolchain_cache.tool_keys(request_b)
            self.assertNotEqual(moved.pop("yosys"), keys["yosys"])
            self.assertEqual(moved, {tool: key for tool, key in keys.items() if tool != "yosys"})
            _set_pin(lock, "mistral", "commit", "b" * 40)
            moved = toolchain_cache.tool_keys(request_b)
            self.assertNotEqual(moved["mistral"], keys["mistral"])
            self.assertNotEqual(moved["nextpnr"], keys["nextpnr"])
            self.assertEqual(moved["verilator"], keys["verilator"])

            changed_host = self._request(base / "checkout-a", base / "cache", host_identity={**HOST, "libc": "other"})
            changed_compiler = self._request(
                base / "checkout-a", base / "cache", compiler_identity={"commands": {"cc": {"sha256": "cc2"}}}
            )
            for request in (changed_host, changed_compiler):
                other = toolchain_cache.tool_keys(request)
                self.assertTrue(all(other[tool] != keys[tool] for tool in toolchain_cache.TOOLS))

    def test_only_nextpnr_follows_the_gpu_lane_accelerator_and_backend_root(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            off = self._request(root, base / "cache")
            hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "h"}
            hip = self._request(
                root, base / "cache", lane="HIP",
                compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc}},
                environment={"ROCM_PATH": "/opt/rocm"},
            )
            cuda = self._request(root, base / "cache", lane="CUDA")
            keys = {name: toolchain_cache.tool_keys(request) for name, request in
                    (("off", off), ("hip", hip), ("cuda", cuda))}
            for tool in ("yosys", "mistral", "verilator", "openfpgaloader"):
                self.assertEqual(len({keys[name][tool] for name in keys}), 1, tool)
            self.assertEqual(len({keys[name]["nextpnr"] for name in keys}), 3)
            hip_root = self._request(
                root, base / "cache", lane="HIP",
                compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc}},
                environment={"ROCM_PATH": "/opt/rocm-b"},
            )
            self.assertNotEqual(toolchain_cache.tool_key(hip_root, "nextpnr"), keys["hip"]["nextpnr"])
            self.assertEqual(toolchain_cache.tool_key(hip_root, "yosys"), keys["hip"]["yosys"])
            self.assertEqual(
                toolchain_cache.build_identity(hip, "nextpnr")["sources"]["mistral"],
                toolchain_cache.locked_pins(hip)["mistral"],
            )

    def test_complete_bootstrap_script_keys_each_install(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            keys = toolchain_cache.tool_keys(request)
            bootstrap = request.root / "scripts" / "bootstrap.sh"
            text = bootstrap.read_text(encoding="utf-8")
            for tool in toolchain_cache.TOOLS:
                self.assertIn(f"build_{tool}() {{", toolchain_cache.tool_recipe(text, tool))
            for helper in ("checkout_pin", "prepare_shared_build_environment"):
                changed, count = re.subn(
                    rf"(?m)^({re.escape(helper)}\(\) \{{\n)",
                    lambda match: match.group(1) + "    # changes the shared build identity\n",
                    text,
                    count=1,
                )
                self.assertEqual(count, 1, helper)
                bootstrap.write_text(changed, encoding="utf-8")
                moved = toolchain_cache.tool_keys(request)
                self.assertTrue(
                    all(moved[tool] != keys[tool] for tool in toolchain_cache.TOOLS), helper
                )
            bootstrap.write_text(text.replace("build_yosys() {", "build_yosys_gone() {"), encoding="utf-8")
            with self.assertRaisesRegex(toolchain_cache.CacheError, "build_yosys"):
                toolchain_cache.tool_keys(request)

    def test_publish_and_verify_covers_library_data_and_internal_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            directory_path = toolchain_cache.install_directory(request, "yosys")
            self.assertEqual(directory_path.parent, request.cache_root / "tools" / "yosys")
            install_bin = directory_path / "install" / "bin"
            install_share = directory_path / "install" / "share" / "yosys"
            install_bin.mkdir(parents=True)
            install_share.mkdir(parents=True)
            (directory_path / "evidence").mkdir()
            binary = install_bin / "yosys"
            binary.write_text("fake yosys\n", encoding="utf-8")
            (install_bin / "yosys-link").symlink_to("yosys")
            (install_share / "datdir.txt").write_text("support\n", encoding="utf-8")
            commit = toolchain_cache.locked_pins(request)["yosys"]["commit"]
            (directory_path / "evidence" / f".digest-{commit}.sha256").write_text(
                hashlib.sha256(binary.read_bytes()).hexdigest() + "\n", encoding="utf-8"
            )
            with self.assertRaisesRegex(toolchain_cache.CacheError, "build lock"):
                toolchain_cache.publish_install(request, "yosys", identity="fake")
            with toolchain_cache.acquire_build_lock(request, "yosys"):
                manifest = toolchain_cache.publish_install(request, "yosys", identity="fake")
            self.assertEqual(manifest.install, directory_path / "install")
            self.assertEqual(manifest.binary, binary)
            self.assertEqual(manifest.record["commit"], commit)
            self.assertTrue(toolchain_cache.ready_path(request, "yosys").is_file())
            self.assertIn("install/share/yosys/datdir.txt", manifest.files)
            self.assertIn(f"evidence/.digest-{commit}.sha256", manifest.files)
            self.assertEqual(manifest.files["install/bin/yosys-link"]["target"], "yosys")
            self.assertFalse(stat.S_IMODE(binary.stat().st_mode) & stat.S_IWUSR)
            self.assertEqual(toolchain_cache.verify_install(request, "yosys").key, manifest.key)
            self.assertNotIn("lock", json.loads(toolchain_cache.ready_path(request, "yosys").read_text()))
            with toolchain_cache.acquire_build_lock(request, "yosys"), self.assertRaisesRegex(
                toolchain_cache.CacheError, "refusing to relabel"
            ):
                toolchain_cache.publish_install(request, "yosys", identity="fake")

    def test_mistral_install_requires_its_database_source(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            directory_path = toolchain_cache.install_directory(request, "mistral")
            (directory_path / "install" / "bin").mkdir(parents=True)
            (directory_path / "evidence").mkdir()
            (directory_path / "install" / "bin" / "mistral-cv").write_text("fake\n", encoding="utf-8")
            (directory_path / "evidence" / "record").write_text("evidence\n", encoding="utf-8")
            with toolchain_cache.acquire_build_lock(request, "mistral"), self.assertRaisesRegex(
                toolchain_cache.CacheError, "database source"
            ):
                toolchain_cache.publish_install(request, "mistral", identity="fake")

    def test_verify_rejects_tampered_support_file_and_escaping_link(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            manifest = _publish(request, "yosys")
            manifest.binary.chmod(0o644)
            manifest.binary.write_text("tampered\n", encoding="utf-8")
            with self.assertRaises(toolchain_cache.CacheError):
                toolchain_cache.verify_install(request, "yosys")

            link_dir = toolchain_cache.install_directory(request, "verilator")
            (link_dir / "install" / "bin").mkdir(parents=True)
            (link_dir / "evidence").mkdir()
            (link_dir / "install" / "bin" / "verilator").write_text("fake\n", encoding="utf-8")
            (link_dir / "evidence" / "record").write_text("evidence\n", encoding="utf-8")
            (link_dir / "install" / "bin" / "escape").symlink_to("../../outside")
            with self.assertRaises(toolchain_cache.CacheError):
                with toolchain_cache.acquire_build_lock(request, "verilator"):
                    toolchain_cache.publish_install(request, "verilator", identity="fake")

    def test_partial_install_has_no_ready_and_resolve_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            for tool in toolchain_cache.TOOLS:
                if tool != "openfpgaloader":
                    _publish(request, tool)
            (toolchain_cache.install_directory(request, "openfpgaloader") / "install").mkdir(parents=True)
            self.assertFalse(toolchain_cache.ready_path(request, "openfpgaloader").exists())
            with self.assertRaisesRegex(toolchain_cache.CacheError, "openfpgaloader install is not ready"):
                toolchain_cache.resolve_ready(request)
            with self.assertRaises(toolchain_cache.CacheError):
                toolchain_cache.verify_ready(request)
            self.assertEqual(toolchain_cache.resolve_install(request, "verilator").tool, "verilator")

    def test_resolve_holds_a_shared_lock_and_records_the_use(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            for tool in toolchain_cache.TOOLS:
                _publish(request, tool)
            locks = {tool: toolchain_cache.lock_path(request, tool) for tool in toolchain_cache.TOOLS}
            for path in locks.values():
                os.utime(path, (1, 1))
            try:
                toolchain_cache.verify_ready(request)
                self.assertTrue(all(path.stat().st_mtime == 1 for path in locks.values()))
                resolution = toolchain_cache.resolve_ready(request)
                self.assertEqual(sorted(resolution.tools), sorted(toolchain_cache.TOOLS))
                for tool, path in locks.items():
                    self.assertGreater(path.stat().st_mtime, 1)
                    self.assertFalse(toolchain_cache._lock_is_free(path), tool)
                # A second resolve reuses the held descriptor.
                held = dict(toolchain_cache._READER_LOCKS)
                toolchain_cache.resolve_ready(request)
                self.assertEqual(held, toolchain_cache._READER_LOCKS)
                # Readers share; only an exclusive taker is refused.
                probe = os.open(locks["yosys"], os.O_RDONLY)
                try:
                    fcntl.flock(probe, fcntl.LOCK_SH | fcntl.LOCK_NB)
                finally:
                    os.close(probe)
            finally:
                for key in [key for key in toolchain_cache._READER_LOCKS if key.startswith(str(request.cache_root))]:
                    os.close(toolchain_cache._READER_LOCKS.pop(key))

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

    def test_host_identity_leaves_out_the_kernel_release(self):
        if platform.system() != "Linux" or platform.machine() != "x86_64":
            self.skipTest("the shared cache supports Linux x86-64 only")
        self.assertNotIn("release", toolchain_cache.host_identity())

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

    def test_single_lane_independent_tool_ignores_inherited_gpu_compiler_environment(self):
        inherited = {
            "PATH": os.environ.get("PATH", ""),
            "HIPCC": "/opt/rocm/bin/hipcc",
            "ROCM_PATH": "/opt/rocm",
            "CUDA_HOME": "/opt/cuda",
            "CUDACXX": "/opt/cuda/bin/nvcc",
            "CUDA_PATH": "/opt/cuda",
            "CMAKE_HIP_COMPILER": "/opt/rocm/bin/hipcc",
            "CMAKE_CUDA_COMPILER": "/opt/cuda/bin/nvcc",
            "CMAKE_HIP_FLAGS": "-O2",
            "CMAKE_CUDA_FLAGS": "-O2",
            "HIPARCHS": "gfx1100;gfx1201",
            "CUDAARCHS": "86",
        }
        with tempfile.TemporaryDirectory() as directory:
            cache = Path(directory) / "cache"
            with mock.patch.object(toolchain_cache, "host_identity", return_value=HOST), mock.patch.object(
                toolchain_cache, "compiler_inventory", return_value=COMPILER
            ), mock.patch.dict(os.environ, inherited, clear=True):
                request = toolchain_cache.request_from_environment(
                    SOURCE, cache_root=cache, gpu_router="OFF", tool="yosys"
                )
                self.assertEqual(request.environment, {})
                with self.assertRaisesRegex(toolchain_cache.CacheError, "ROCM_PATH|HIPCC"):
                    toolchain_cache.request_from_environment(SOURCE, cache_root=cache, gpu_router="OFF")

    def test_hip_and_off_identities_are_unchanged_by_cuda_architectures(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            hip = self._request(root, base / "cache", lane="HIP")
            off = self._request(root, base / "cache", lane="OFF")
            self.assertEqual(
                toolchain_cache._normalise_configuration(hip),
                {"gpu-router": "HIP", "hip-architectures": "gfx1100;gfx1201"},
            )
            self.assertEqual(
                toolchain_cache._normalise_configuration(off),
                {"gpu-router": "OFF", "hip-architectures": "unused"},
            )
            for lane in ("HIP", "OFF"):
                self.assertEqual(
                    toolchain_cache.tool_keys(self._request(root, base / "cache", lane=lane)),
                    toolchain_cache.tool_keys(
                        self._request(root, base / "cache", lane=lane, cuda_architectures="75;89")
                    ),
                )

    def test_cuda_identity_includes_architectures(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            default = self._request(root, base / "cache", lane="CUDA")
            self.assertEqual(
                toolchain_cache._normalise_configuration(default),
                {"gpu-router": "CUDA", "hip-architectures": "unused", "cuda-architectures": "86"},
            )
            self.assertEqual(
                toolchain_cache.configuration_text(toolchain_cache._normalise_configuration(default)),
                "gpu-router=CUDA; hip-architectures=unused; cuda-architectures=86",
            )
            keys = {
                toolchain_cache.tool_key(
                    self._request(root, base / "cache", lane="CUDA", cuda_architectures=value), "nextpnr"
                )
                for value in ("86", "75;86", "86-real", "89")
            }
            self.assertEqual(len(keys), 4)
            self.assertIn(toolchain_cache.tool_key(default, "nextpnr"), keys)
            for value in ("native", "all", "all-major", "sm_86", "86;", "86 ", "86,89", "compute_86"):
                with self.subTest(value=value), self.assertRaises(toolchain_cache.CacheError):
                    toolchain_cache.tool_keys(
                        self._request(root, base / "cache", lane="CUDA", cuda_architectures=value)
                    )

    def test_cuda_architectures_are_read_from_cli_and_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            clean = {"PATH": os.environ.get("PATH", ""), "FES_TOOLCHAIN_CACHE_ROOT": str(base / "cache")}
            with mock.patch.object(toolchain_cache, "host_identity", return_value=HOST), mock.patch.object(
                toolchain_cache, "compiler_inventory", return_value=COMPILER
            ), mock.patch.dict(os.environ, clean, clear=True):
                request, _ = toolchain_cache._load_request_arguments(
                    ["--root", str(base), "--gpu-router", "CUDA", "--cuda-architectures", "75;86", "plan"]
                )
                self.assertEqual(request.cuda_architectures, "75;86")
                os.environ["FES_TOOLCHAIN_CUDA_ARCHITECTURES"] = "89"
                request = toolchain_cache.request_from_environment(base, gpu_router="CUDA")
                self.assertEqual(request.cuda_architectures, "89")
                os.environ.pop("FES_TOOLCHAIN_CUDA_ARCHITECTURES")
                request = toolchain_cache.request_from_environment(base, gpu_router="CUDA")
                self.assertEqual(request.cuda_architectures, toolchain_cache.DEFAULT_CUDA_ARCHITECTURES)
                os.environ["CUDAARCHS"] = "75"
                with self.assertRaises(toolchain_cache.CacheError):
                    toolchain_cache.request_from_environment(base, gpu_router="CUDA")

    def test_cmake_architecture_selectors_must_match_the_requested_lane(self):
        toolchain_cache.validate_shared_environment(
            {
                "FES_TOOLCHAIN_GPU_ROUTER": "CUDA",
                "FES_TOOLCHAIN_CUDA_ARCHITECTURES": "75;86",
                "CUDAARCHS": "75;86",
                "CMAKE_CUDA_ARCHITECTURES": "75;86",
            }
        )
        toolchain_cache.validate_shared_environment({"CUDAARCHS": "86"}, gpu_router="CUDA")
        toolchain_cache.validate_shared_environment({"HIPARCHS": "gfx1100;gfx1201"}, gpu_router="HIP")
        rejected = (
            ({"CUDAARCHS": "75"}, {"gpu_router": "CUDA"}),
            ({"CUDAARCHS": "86", "FES_TOOLCHAIN_CUDA_ARCHITECTURES": "89"}, {"gpu_router": "CUDA"}),
            ({"CUDAARCHS": "86"}, {"gpu_router": "HIP"}),
            ({"CUDAARCHS": "86"}, {"gpu_router": "OFF"}),
            ({"CMAKE_CUDA_ARCHITECTURES": "75"}, {"gpu_router": "CUDA"}),
            ({"HIPARCHS": "gfx1030"}, {"gpu_router": "HIP"}),
            ({"HIPARCHS": "gfx1100;gfx1201"}, {"gpu_router": "CUDA"}),
        )
        for values, selection in rejected:
            with self.subTest(values=values, selection=selection), self.assertRaises(
                toolchain_cache.CacheError
            ):
                toolchain_cache.validate_shared_environment(values, **selection)

    def test_lane_selector_defaults_are_canonical(self):
        """Implicit and explicit defaults select one semantic cache lane."""

        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / "checkout"
            self._request(root, base / "cache")
            cache = base / "cache"
            clean = {
                "PATH": os.environ.get("PATH", ""),
                "HOME": os.environ.get("HOME", str(base)),
                "USER": os.environ.get("USER", "test"),
            }
            with mock.patch.object(toolchain_cache, "host_identity", return_value=HOST), mock.patch.object(
                toolchain_cache, "compiler_inventory", return_value=COMPILER
            ), mock.patch.dict(os.environ, clean, clear=True):
                implicit_off = toolchain_cache.request_from_environment(root, cache_root=cache)
                os.environ["FES_TOOLCHAIN_GPU_ROUTER"] = "OFF"
                explicit_off = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertEqual(toolchain_cache.tool_keys(implicit_off), toolchain_cache.tool_keys(explicit_off))

                os.environ["FES_TOOLCHAIN_GPU_ROUTER"] = "HIP"
                os.environ.pop("FES_TOOLCHAIN_HIP_ARCHITECTURES", None)
                implicit_hip = toolchain_cache.request_from_environment(root, cache_root=cache)
                os.environ["FES_TOOLCHAIN_HIP_ARCHITECTURES"] = toolchain_cache.DEFAULT_HIP_ARCHITECTURES
                explicit_hip = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertEqual(toolchain_cache.tool_keys(implicit_hip), toolchain_cache.tool_keys(explicit_hip))

                os.environ["ROCM_PATH"] = "/opt/rocm-a"
                changed_hip_root = toolchain_cache.request_from_environment(root, cache_root=cache)
                self.assertNotEqual(
                    toolchain_cache.tool_key(explicit_hip, "nextpnr"),
                    toolchain_cache.tool_key(changed_hip_root, "nextpnr"),
                )
                self.assertEqual(
                    toolchain_cache.tool_key(explicit_hip, "yosys"),
                    toolchain_cache.tool_key(changed_hip_root, "yosys"),
                )

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

            request = self._request(base / "checkout", base / "cache")
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
            first_keys = toolchain_cache.tool_keys(first_request)
            second_keys = toolchain_cache.tool_keys(second_request)
            self.assertTrue(all(first_keys[tool] != second_keys[tool] for tool in toolchain_cache.TOOLS))


    @unittest.skipUnless(
        platform.system() == "Linux" and platform.machine() == "x86_64",
        "the repeated host-probe check targets the supported powerboat lane",
    )
    def test_real_request_and_cli_key_repeat_is_stable(self):
        """Run the actual dependency probes twice, including the CLI path."""

        root = SOURCE
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
                self.assertEqual(toolchain_cache.tool_keys(first), toolchain_cache.tool_keys(second))

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
            ]

            def plan(environment):
                result = subprocess.run(
                    ["python3", *command], env=environment, check=True, capture_output=True, text=True
                )
                return json.loads(result.stdout)

            first_cli = plan(cli_env)
            self.assertEqual(sorted(first_cli), sorted(toolchain_cache.TOOLS))
            self.assertEqual(
                {tool: value["key"] for tool, value in first_cli.items()}, toolchain_cache.tool_keys(first)
            )
            self.assertEqual(first_cli, plan(cli_env))
            cli_env["FES_TOOLCHAIN_GPU_ROUTER"] = "OFF"
            self.assertEqual(first_cli, plan(cli_env))

    def test_manifest_path_is_not_relocatable(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache")
            _publish(request, "yosys")
            copied = base / "copied"
            copied.mkdir()
            ready = toolchain_cache.ready_path(request, "yosys")
            # Reuse the manifest bytes at another path, without copying the install.
            relocated = json.loads(ready.read_text(encoding="utf-8"))
            relocated["directory"] = str(copied)
            ready.chmod(0o644)
            ready.write_text(json.dumps(relocated, sort_keys=True) + "\n", encoding="utf-8")
            with self.assertRaisesRegex(toolchain_cache.CacheError, "non-relocatable"):
                toolchain_cache.verify_install(request, "yosys")

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


class ToolchainCachePruneTests(unittest.TestCase):
    """The supported prune keeps pinned, recent and held installs."""

    def setUp(self):
        self._directory = tempfile.TemporaryDirectory()
        self.base = Path(self._directory.name)
        # A Git repository whose committed locks are the prune's references.
        self.repository = self.base / "repo"
        self.root = self.repository / "sources" / "misteross"
        (self.root / "scripts").mkdir(parents=True)
        shutil.copy2(SOURCE / "scripts" / "lockfile.py", self.root / "scripts" / "lockfile.py")
        shutil.copy2(SOURCE / "scripts" / "bootstrap.sh", self.root / "scripts" / "bootstrap.sh")
        shutil.copy2(SOURCE / "toolchain.lock", self.root / "toolchain.lock")
        core = self.root / "cores" / "fes-core"
        core.mkdir(parents=True)
        shutil.copy2(SOURCE / "toolchain.lock", core / "toolchain.lock")
        _set_pin(core / "toolchain.lock", "nextpnr", "commit", "c" * 40)
        git = ["git", "-C", str(self.repository), "-c", "user.name=t", "-c", "user.email=t@example.invalid"]
        subprocess.run([*git, "init", "-q"], check=True)
        subprocess.run([*git, "add", "-A"], check=True)
        subprocess.run([*git, "commit", "-q", "-m", "locks"], check=True)
        self.cache = self.base / "cache"
        self.cache.mkdir(mode=0o700)

    def tearDown(self):
        for root, directories, _files in os.walk(self.base):
            for name in directories:
                path = os.path.join(root, name)
                if not os.path.islink(path):
                    os.chmod(path, 0o700)
        self._directory.cleanup()

    def request(self, lock: Path | None = None, **fields):
        values = {
            "root": self.root,
            "lock_path": lock or self.root / "toolchain.lock",
            "cache_root": self.cache,
            "host_identity": HOST,
            "compiler_identity": COMPILER,
        }
        values.update(fields)
        return toolchain_cache.ToolchainRequest(**values)

    def prune(self, **options):
        output = io.StringIO()
        report = toolchain_cache.prune(
            self.cache, self.root, ref="HEAD", host=HOST, compiler=COMPILER, output=output,
            **{"keep_days": 14, **options},
        )
        return report, output.getvalue()

    def test_prune_keeps_pinned_recent_and_held_installs(self):
        pinned = self.request()
        for tool in toolchain_cache.TOOLS:
            _publish(pinned, tool)
        # The second committed lock pins another nextpnr: also referenced.
        core = self.request(self.root / "cores" / "fes-core" / "toolchain.lock")
        _publish(core, "nextpnr")
        # An unpinned Yosys, last used 30 days ago.
        stale_lock = self.base / "stale.lock"
        shutil.copy2(self.root / "toolchain.lock", stale_lock)
        _set_pin(stale_lock, "yosys", "commit", "d" * 40)
        stale = self.request(stale_lock)
        stale_install = _publish(stale, "yosys")
        old = time.time() - 30 * 86400
        os.utime(toolchain_cache.lock_path(stale, "yosys"), (old, old))
        # An unpinned Verilator used yesterday.
        recent_lock = self.base / "recent.lock"
        shutil.copy2(self.root / "toolchain.lock", recent_lock)
        _set_pin(recent_lock, "verilator", "commit", "e" * 40)
        recent = self.request(recent_lock)
        recent_install = _publish(recent, "verilator")
        # A pinned install built for another host is no longer referenced.
        other_host = self.request(host_identity={**HOST, "libc": "glibc-old"})
        other_install = _publish(other_host, "openfpgaloader")
        os.utime(toolchain_cache.lock_path(other_host, "openfpgaloader"), (old, old))
        # A failed build: no ready.json.
        partial = self.cache / "tools" / "mistral" / ("f" * 64)
        (partial / "build").mkdir(parents=True)

        report, output = self.prune()
        self.assertIn(f"tools/yosys/{stale_install.key}", report["removed"])
        self.assertIn(f"tools/openfpgaloader/{other_install.key}", report["removed"])
        self.assertIn(f"tools/mistral/{'f' * 64}", report["removed"])
        self.assertIn("dry run", output)
        self.assertTrue(stale_install.directory.exists(), "a dry run removes nothing")
        kept = set(report["kept"])
        self.assertIn(f"tools/verilator/{recent_install.key}", kept)
        for tool in toolchain_cache.TOOLS:
            self.assertIn(f"tools/{tool}/{toolchain_cache.tool_key(pinned, tool)}", kept)
        self.assertIn(f"tools/nextpnr/{toolchain_cache.tool_key(core, 'nextpnr')}", kept)

        # A process that resolved the stale install holds it.
        holder = os.open(toolchain_cache.lock_path(stale, "yosys"), os.O_RDONLY)
        try:
            fcntl.flock(holder, fcntl.LOCK_SH)
            report, _ = self.prune(apply=True)
            self.assertIn(f"tools/yosys/{stale_install.key}", report["busy"])
            self.assertTrue(stale_install.directory.exists())
        finally:
            os.close(holder)
        self.assertFalse(other_install.directory.exists())
        self.assertFalse(partial.exists())
        os.utime(toolchain_cache.lock_path(stale, "yosys"), (old, old))
        report, output = self.prune(apply=True)
        self.assertEqual(report["removed"], [f"tools/yosys/{stale_install.key}"])
        self.assertFalse(stale_install.directory.exists())
        self.assertIn("freed", output)
        toolchain_cache.verify_ready(pinned)
        self.assertEqual(toolchain_cache.verify_install(recent, "verilator").key, recent_install.key)
        self.assertEqual(list((self.cache / "tools" / "yosys").glob(".prune-*")), [])

    def test_prune_reads_locks_from_the_ref_not_the_worktree(self):
        pinned = self.request()
        install = _publish(pinned, "yosys")
        os.utime(toolchain_cache.lock_path(pinned, "yosys"), (1, 1))
        # Uncommitted lock edits do not count: the install stays referenced.
        _set_pin(self.root / "toolchain.lock", "yosys", "commit", "a" * 40)
        _set_pin(self.root / "cores" / "fes-core" / "toolchain.lock", "yosys", "commit", "a" * 40)
        report, _ = self.prune()
        self.assertIn(f"tools/yosys/{install.key}", report["kept"])
        with self.assertRaisesRegex(toolchain_cache.CacheError, "git"):
            toolchain_cache.prune(self.cache, self.root, keep_days=14, ref="no-such-ref",
                                  host=HOST, compiler=COMPILER, output=io.StringIO())

    def test_legacy_slots_are_pruned_only_on_request_and_by_last_use(self):
        slots = self.cache / "slots"
        old_slot = slots / ("1" * 64)
        new_slot = slots / ("2" * 64)
        for slot in (old_slot, new_slot):
            (slot / "install" / "bin").mkdir(parents=True)
            (slot / "install" / "bin" / "yosys").write_text("legacy\n", encoding="utf-8")
            (slot / "ready.json").write_text("{}\n", encoding="utf-8")
        old = time.time() - 30 * 86400
        for path in (old_slot / "install" / "bin" / "yosys", old_slot / "ready.json"):
            os.utime(path, (old, old))
        (old_slot / "install" / "bin" / "yosys").chmod(0o555)
        (old_slot / "install" / "bin").chmod(0o555)
        report, _ = self.prune(apply=True)
        self.assertTrue(old_slot.exists())
        self.assertEqual(report["removed"], [])
        with self.assertRaisesRegex(toolchain_cache.CacheError, "at least 2"):
            self.prune(apply=True, legacy_slots=True, keep_days=1)
        report, _ = self.prune(apply=True, legacy_slots=True)
        self.assertEqual(report["removed"], [f"slots/{'1' * 64}"])
        self.assertFalse(old_slot.exists())
        self.assertTrue(new_slot.exists())

    def test_prune_cli_is_a_dry_run_by_default(self):
        install = _publish(self.request(), "yosys")
        result = subprocess.run(
            ["python3", str(SOURCE / "scripts" / "toolchain_cache.py"), "prune", "--root", str(self.root),
             "--cache", str(self.cache), "--ref", "HEAD", "--keep-days", "0"],
            capture_output=True, text=True,
        )
        if result.returncode and "required host command is missing" in result.stderr:
            self.skipTest("the host compiler inventory is incomplete")
        # The real host does not match the fixture's, so the install is not
        # referenced and the zero-day window does not keep it.
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"would remove: tools/yosys/{install.key}", result.stdout)
        self.assertTrue(install.directory.exists())


if __name__ == "__main__":
    unittest.main()
