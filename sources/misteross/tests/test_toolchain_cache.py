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


def _publish(request: toolchain_cache.ToolchainRequest, tool: str, *, content: str = "fake",
             max_gb: str | None = None) -> toolchain_cache.InstallManifest:
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
        return toolchain_cache.publish_install(request, tool, identity=f"{content} {tool}", max_gb=max_gb)


class ToolchainCacheContractTests(unittest.TestCase):
    def test_missing_nextpnr_reports_selected_lane_without_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root, cache = base / "checkout", base / "cache"
            off = self._request(root, cache)
            hip = self._request(root, cache, lane="HIP")
            cuda = self._request(root, cache, lane="CUDA")
            # A valid install in another lane must not be silently selected.
            other = _publish(hip, "nextpnr")
            for request in (off, cuda):
                with self.subTest(lane=request.gpu_router):
                    before = toolchain_cache.tool_key(request, "nextpnr")
                    for operation in (toolchain_cache.verify_install, toolchain_cache.resolve_install):
                        with self.assertRaises(toolchain_cache.CacheError) as raised:
                            operation(request, "nextpnr")
                        self.assertIn(f"gpu-router='{request.gpu_router}'", str(raised.exception))
                        self.assertIn(str(before), str(raised.exception))
                        self.assertIsInstance(raised.exception.__cause__, toolchain_cache.CacheError)
                    self.assertEqual(before, toolchain_cache.tool_key(request, "nextpnr"))
            self.assertEqual(toolchain_cache.verify_install(hip, "nextpnr").key, other.key)
            other.binary.chmod(0o644)
            other.binary.write_text("tampered\n", encoding="utf-8")
            with self.assertRaises(toolchain_cache.CacheError) as raised:
                toolchain_cache.verify_install(hip, "nextpnr")
            self.assertIn("gpu-router='HIP'", str(raised.exception))
            self.assertIn("closure differs", str(raised.exception))

    def test_lane_context_on_all_tool_and_cli_verification(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache", lane="CUDA")
            for tool in ("yosys", "mistral"):
                _publish(request, tool)
            for operation in (toolchain_cache.resolve_ready, toolchain_cache.verify_ready):
                with self.assertRaises(toolchain_cache.CacheError) as raised:
                    operation(request)
                self.assertIn("gpu-router='CUDA'", str(raised.exception))
            with mock.patch.object(toolchain_cache, "_load_request_arguments") as load:
                load.return_value = (request, mock.Mock(command="verify", tool="nextpnr", expect_key=None))
                with mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
                    self.assertEqual(toolchain_cache._main([]), 2)
                self.assertIn("gpu-router='CUDA'", stderr.getvalue())
                self.assertIn("missing path component", stderr.getvalue())

    def test_other_tool_failures_are_not_attributed_to_gpu_lane(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "checkout", base / "cache", lane="CUDA")
            with self.assertRaises(toolchain_cache.CacheError) as raised:
                toolchain_cache.resolve_install(request, "yosys")
            self.assertIn("yosys install is not ready", str(raised.exception))
            self.assertNotIn("gpu-router", str(raised.exception))

    def test_request_precedence_is_reflected_in_missing_lane_error(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root, cache = base / "checkout", base / "cache"
            self._request(root, cache)  # Create a lock and bootstrap fixture.
            cases = (({}, None, "OFF"), ({"FES_TOOLCHAIN_GPU_ROUTER": "CUDA"}, None, "CUDA"),
                     ({"FES_TOOLCHAIN_GPU_ROUTER": "CUDA"}, "HIP", "HIP"))
            for environment, explicit, expected in cases:
                with self.subTest(environment=environment, explicit=explicit), \
                     mock.patch.dict(os.environ, environment, clear=True), \
                     mock.patch.object(toolchain_cache, "host_identity", return_value=HOST), \
                     mock.patch.object(toolchain_cache, "compiler_inventory", return_value=COMPILER):
                    request = toolchain_cache.request_from_environment(
                        root, cache_root=cache, gpu_router=explicit)
                    self.assertEqual(request.gpu_router, expected)
                    with self.assertRaises(toolchain_cache.CacheError) as raised:
                        toolchain_cache.verify_install(request, "nextpnr")
                    self.assertIn(f"gpu-router='{expected}'", str(raised.exception))

    def test_binary_pin_selects_original_verified_install_after_recipe_drift(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "source", base / "cache")
            original = _publish(request, "yosys", content="approved")
            recipe = request.root / "scripts/bootstrap.sh"
            recipe.write_text(recipe.read_text() + "\n# newer provisioning recipe\n")
            current = _publish(request, "yosys", content="new build")
            self.assertNotEqual(original.key, current.key)
            self.assertEqual(toolchain_cache.resolve_install(request, "yosys").key, current.key)
            selected = toolchain_cache.resolve_install(
                request, "yosys", binary_sha256=original.record["sha256"])
            self.assertEqual(selected.key, original.key)
            self.assertIn(str(base / "cache/locks" / (original.key + ".lock")),
                          toolchain_cache._READER_LOCKS)
            with self.assertRaisesRegex(toolchain_cache.CacheError, "no ready"):
                toolchain_cache.resolve_install(request, "yosys", binary_sha256="0" * 64)

    def test_binary_pin_rejects_changed_source_pin_and_published_key(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "source", base / "cache")
            original = _publish(request, "yosys")
            digest = original.record["sha256"]
            _set_pin(request.lock_path, "yosys", "commit", "e" * 40)
            with self.assertRaisesRegex(toolchain_cache.CacheError, "source differs"):
                toolchain_cache.resolve_install(request, "yosys", binary_sha256=digest)
            _set_pin(request.lock_path, "yosys", "commit", original.record["commit"])
            ready = original.directory / "ready.json"
            data = json.loads(ready.read_text())
            data["build"]["recipe"] = "0" * 64
            ready.chmod(0o600)
            ready.write_text(json.dumps(data))
            with self.assertRaisesRegex(toolchain_cache.CacheError, "published key"):
                toolchain_cache.resolve_install(request, "yosys", binary_sha256=digest)

    def test_binary_pin_ignores_unrelated_install_pruned_during_discovery(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "source", base / "cache")
            first = _publish(request, "yosys", content="first")
            recipe = request.root / "scripts/bootstrap.sh"
            recipe.write_text(recipe.read_text() + "\n# another install\n")
            second = _publish(request, "yosys", content="second")
            unrelated, wanted = sorted((first, second), key=lambda item: item.key)
            vanished = unrelated.directory / "ready.json"
            read_manifest = toolchain_cache.read_manifest
            def prune_before_read(path):
                if path == vanished:
                    path.unlink()
                return read_manifest(path)
            with mock.patch.object(toolchain_cache, "read_manifest", side_effect=prune_before_read):
                selected = toolchain_cache.resolve_install(
                    request, "yosys", binary_sha256=wanted.record["sha256"])
            self.assertEqual(selected.key, wanted.key)
            self.assertFalse(vanished.exists())
            self.assertTrue(wanted.binary.is_file())

    def test_binary_pin_verifies_full_install_closure_and_host(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            request = self._request(base / "source", base / "cache")
            original = _publish(request, "yosys")
            digest = original.record["sha256"]
            wrong_host = toolchain_cache.dataclasses.replace(
                request, host_identity={**HOST, "machine": "other"})
            with self.assertRaisesRegex(toolchain_cache.CacheError, "host architecture"):
                toolchain_cache.resolve_install(wrong_host, "yosys", binary_sha256=digest)
            evidence = original.evidence / "record"
            evidence.chmod(0o600)
            evidence.write_text("tampered\n")
            with self.assertRaisesRegex(toolchain_cache.CacheError, "closure differs"):
                toolchain_cache.resolve_install(request, "yosys", binary_sha256=digest)

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
        defaults = {
            "keep_days": 14,
            "host": HOST,
            "compiler": COMPILER,
            "environment": {},
        }
        defaults.update(options)
        report = toolchain_cache.prune(
            self.cache, self.root, ref="HEAD", output=output,
            **defaults,
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
        # A pinned install built by an older bootstrap.sh is no longer referenced.
        older_bootstrap = self.base / "older-bootstrap.sh"
        older_bootstrap.write_text("# older\n" + (self.root / "scripts" / "bootstrap.sh").read_text(), encoding="utf-8")
        older = self.request(bootstrap_path=older_bootstrap)
        older_install = _publish(older, "mistral")
        self.assertNotEqual(older_install.key, toolchain_cache.tool_key(pinned, "mistral"))
        os.utime(toolchain_cache.lock_path(older, "mistral"), (old, old))
        # A failed build: no ready.json.
        partial = self.cache / "tools" / "mistral" / ("f" * 64)
        (partial / "build").mkdir(parents=True)

        report, output = self.prune()
        self.assertIn(f"tools/yosys/{stale_install.key}", report["removed"])
        self.assertIn(f"tools/openfpgaloader/{other_install.key}", report["removed"])
        self.assertIn(f"tools/mistral/{older_install.key}", report["removed"])
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

    def test_prune_reclaims_tombstones_only_from_dead_prunes(self):
        """An interrupted prune leaves a complete install nobody revisits.

        ``remove()`` renames the directory aside to ``.prune-<name>-<pid>``
        before deleting it, and both enumeration passes skip dot-prefixed
        names, so a delete that dies after the rename strands the whole tree.
        A live pid means a concurrent prune is still working on it.
        """
        pinned = self.request()
        for tool in toolchain_cache.TOOLS:
            _publish(pinned, tool)
        parent = self.cache / "tools" / "yosys"
        dead_pid = int(Path("/proc/sys/kernel/pid_max").read_text()) + 1
        abandoned = parent / f".prune-{'a' * 64}-{dead_pid}"
        (abandoned / "install" / "bin").mkdir(parents=True)
        (abandoned / "install" / "bin" / "yosys").write_text("stranded", encoding="utf-8")
        live = parent / f".prune-{'b' * 64}-{os.getpid()}"
        (live / "install").mkdir(parents=True)

        report, output = self.prune()
        self.assertIn(f"yosys/{abandoned.name}", report["removed"])
        self.assertIn("would reclaim", output)
        self.assertIn("in progress, skipped", output)
        self.assertTrue(abandoned.exists(), "a dry run removes nothing")

        report, output = self.prune(apply=True)
        self.assertIn(f"yosys/{abandoned.name}", report["removed"])
        self.assertFalse(abandoned.exists())
        self.assertTrue(live.exists(), "a live prune's tombstone is left alone")
        self.assertIn("reclaimed", output)
        # The pinned installs are untouched by the sweep.
        toolchain_cache.verify_ready(pinned)

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
                                  host=HOST, compiler=COMPILER, environment={}, output=io.StringIO())

    def test_prune_preserves_committed_binary_pin_across_recipe_drift(self):
        older_recipe = self.base / "older-bootstrap.sh"
        older_recipe.write_text("# older\n" + (self.root / "scripts/bootstrap.sh").read_text())
        older = self.request(bootstrap_path=older_recipe)
        install = _publish(older, "yosys", content="approved binary")
        _publish(self.request(), "yosys", content="new provisioning binary")
        old = time.time() - 30 * 86400
        os.utime(toolchain_cache.lock_path(older, "yosys"), (old, old))
        lock = self.root / "cores/fes-core/quartus.lock"
        lock.write_text(
            '[quartus]\nedition = "lite"\n'
            'version = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"\n'
            'device_family = "cyclonev"\nquartus_sh_sha256 = "' + 'a' * 64 + '"\n'
            '[yosys]\ncommit = "' + install.record['commit'] + '"\n'
            'sha256 = "' + install.record['sha256'] + '"\n')
        git = ["git", "-C", str(self.repository), "-c", "user.name=t", "-c", "user.email=t@example.invalid"]
        subprocess.run([*git, "add", "-A"], check=True)
        subprocess.run([*git, "commit", "-q", "-m", "binary pin"], check=True)
        # Uncommitted edits must not erase a committed binary reference.
        lock.write_text(lock.read_text().replace(install.record['sha256'], '0' * 64))
        report, output = self.prune(apply=True)
        self.assertIn(f"tools/yosys/{install.key}", report['kept'])
        self.assertIn('binary-pinned at HEAD', output)
        selected = toolchain_cache.resolve_install(
            self.request(), 'yosys', binary_sha256=install.record['sha256'], hold=False)
        self.assertEqual(selected.key, install.key)
        # Once the committed pin moves, the aged old recipe is collectible.
        subprocess.run([*git, "add", "-A"], check=True)
        subprocess.run([*git, "commit", "-q", "-m", "move binary pin"], check=True)
        report, _ = self.prune(apply=True)
        self.assertIn(f"tools/yosys/{install.key}", report['removed'])
        self.assertFalse(install.directory.exists())

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

    def test_prune_ages_out_obsolete_nextpnr_gpu_lane_variants(self):
        """Pinned nextpnr retention discriminates GPU lane; non-GPU tools do not."""
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        nvcc = {"path": "/usr/local/cuda/bin/nvcc", "sha256": "cuda-1", "version": "cuda 12.6", "returncode": 0}
        hip_compiler = {"commands": {**COMPILER["commands"], "hipcc": hipcc}}
        cuda_compiler = {"commands": {**COMPILER["commands"], "nvcc": nvcc}}

        off_req = self.request(gpu_router="OFF")
        hip_req = self.request(gpu_router="HIP", compiler_identity=hip_compiler)
        cuda_req = self.request(gpu_router="CUDA", compiler_identity=cuda_compiler)
        yosys_install = _publish(off_req, "yosys")
        off_install = _publish(off_req, "nextpnr", content="off")
        hip_install = _publish(hip_req, "nextpnr", content="hip")
        cuda_install = _publish(cuda_req, "nextpnr", content="cuda")

        old = time.time() - 30 * 86400
        for req, tool in (
            (off_req, "yosys"),
            (off_req, "nextpnr"),
            (hip_req, "nextpnr"),
            (cuda_req, "nextpnr"),
        ):
            os.utime(toolchain_cache.lock_path(req, tool), (old, old))

        # COMPILER has no accelerator compiler, so OFF is the only lane this
        # host can build: OFF stays pinned, HIP and CUDA age out.
        report, output = self.prune()
        self.assertIn(f"tools/yosys/{yosys_install.key}", report["kept"])
        self.assertIn(f"tools/nextpnr/{off_install.key}", report["kept"])
        self.assertIn(f"kept: tools/nextpnr/{off_install.key} (pinned at HEAD)", output)
        self.assertIn(f"tools/nextpnr/{hip_install.key}", report["removed"])
        self.assertIn(f"tools/nextpnr/{cuda_install.key}", report["removed"])

        # A recently used obsolete lane still survives the keep_days window.
        now = time.time()
        os.utime(toolchain_cache.lock_path(hip_req, "nextpnr"), (now, now))
        report, output = self.prune(now=now)
        self.assertIn(f"tools/nextpnr/{hip_install.key}", report["kept"])
        self.assertNotIn(f"kept: tools/nextpnr/{hip_install.key} (pinned at HEAD)", output)
        self.assertIn(f"tools/nextpnr/{cuda_install.key}", report["removed"])

        # A held obsolete lane is never removed even when older than keep_days.
        os.utime(toolchain_cache.lock_path(hip_req, "nextpnr"), (old, old))
        holder = os.open(toolchain_cache.lock_path(hip_req, "nextpnr"), os.O_RDONLY)
        try:
            fcntl.flock(holder, fcntl.LOCK_SH)
            report, _ = self.prune(apply=True)
            self.assertIn(f"tools/nextpnr/{hip_install.key}", report["busy"])
            self.assertTrue(hip_install.directory.exists())
        finally:
            os.close(holder)
        self.assertFalse(cuda_install.directory.exists())
        self.assertTrue(off_install.directory.exists())
        self.assertTrue(yosys_install.directory.exists())

    def test_prune_ages_out_nextpnr_on_accelerator_architecture_or_env_drift(self):
        """A ROCm/CUDA upgrade, architecture change, or backend root move is not pinned forever."""
        hipcc_old = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-old", "version": "rocm 6.2", "returncode": 0}
        hipcc_new = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-new", "version": "rocm 6.3", "returncode": 0}
        nvcc_old = {"path": "/usr/local/cuda/bin/nvcc", "sha256": "nvcc-old", "version": "cuda 12.6", "returncode": 0}
        nvcc_new = {"path": "/usr/local/cuda/bin/nvcc", "sha256": "nvcc-new", "version": "cuda 12.8", "returncode": 0}

        hip_current = self.request(
            gpu_router="HIP",
            hip_architectures="gfx1100;gfx1201",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc_new}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        hip_old_accel = self.request(
            gpu_router="HIP",
            hip_architectures="gfx1100;gfx1201",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc_old}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        hip_old_arch = self.request(
            gpu_router="HIP",
            hip_architectures="gfx1100",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc_new}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        hip_old_env = self.request(
            gpu_router="HIP",
            hip_architectures="gfx1100;gfx1201",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc_new}},
            environment={"ROCM_PATH": "/opt/rocm-6.2"},
        )
        cuda_current = self.request(
            gpu_router="CUDA",
            cuda_architectures="86",
            compiler_identity={"commands": {**COMPILER["commands"], "nvcc": nvcc_new}},
            environment={"CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        cuda_old_accel = self.request(
            gpu_router="CUDA",
            cuda_architectures="86",
            compiler_identity={"commands": {**COMPILER["commands"], "nvcc": nvcc_old}},
            environment={"CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        cuda_old_arch = self.request(
            gpu_router="CUDA",
            cuda_architectures="75;86",
            compiler_identity={"commands": {**COMPILER["commands"], "nvcc": nvcc_new}},
            environment={"CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        cuda_old_env = self.request(
            gpu_router="CUDA",
            cuda_architectures="86",
            compiler_identity={"commands": {**COMPILER["commands"], "nvcc": nvcc_new}},
            environment={"CUDA_HOME": "/usr/local/cuda-12.6"},
        )

        old = time.time() - 30 * 86400
        published = {}
        for name, req in (
            ("hip_current", hip_current),
            ("hip_old_accel", hip_old_accel),
            ("hip_old_arch", hip_old_arch),
            ("hip_old_env", hip_old_env),
            ("cuda_current", cuda_current),
            ("cuda_old_accel", cuda_old_accel),
            ("cuda_old_arch", cuda_old_arch),
            ("cuda_old_env", cuda_old_env),
        ):
            published[name] = _publish(req, "nextpnr", content=name)
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        report, _ = self.prune(
            gpu_routers=("HIP",),
            hip_architectures="gfx1100;gfx1201",
            compiler={"commands": {**COMPILER["commands"], "hipcc": hipcc_new}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        self.assertEqual(report["kept"], [f"tools/nextpnr/{published['hip_current'].key}"])
        for obsolete in (
            "hip_old_accel", "hip_old_arch", "hip_old_env",
            "cuda_current", "cuda_old_accel", "cuda_old_arch", "cuda_old_env",
        ):
            self.assertIn(f"tools/nextpnr/{published[obsolete].key}", report["removed"])

        report, _ = self.prune(
            gpu_routers=("CUDA",),
            cuda_architectures="86",
            compiler={"commands": {**COMPILER["commands"], "nvcc": nvcc_new}},
            environment={"CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        self.assertEqual(report["kept"], [f"tools/nextpnr/{published['cuda_current'].key}"])
        for obsolete in (
            "cuda_old_accel", "cuda_old_arch", "cuda_old_env",
            "hip_current", "hip_old_accel", "hip_old_arch", "hip_old_env",
        ):
            self.assertIn(f"tools/nextpnr/{published[obsolete].key}", report["removed"])

    def test_prune_keeps_multiple_requested_gpu_lanes_with_scoped_environment(self):
        """Multi-lane prune scopes backend environment and accelerator per lane."""
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        nvcc = {"path": "/usr/local/cuda/bin/nvcc", "sha256": "cuda-1", "version": "cuda 12.8", "returncode": 0}
        both_compilers = {"commands": {**COMPILER["commands"], "hipcc": hipcc, "nvcc": nvcc}}

        off_req = self.request(gpu_router="OFF")
        hip_req = self.request(
            gpu_router="HIP",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        cuda_req = self.request(
            gpu_router="CUDA",
            compiler_identity={"commands": {**COMPILER["commands"], "nvcc": nvcc}},
            environment={"CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        stale_hip_req = self.request(
            gpu_router="HIP",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": {**hipcc, "sha256": "hip-old"}}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )

        old = time.time() - 30 * 86400
        off_install = _publish(off_req, "nextpnr", content="off")
        hip_install = _publish(hip_req, "nextpnr", content="hip")
        cuda_install = _publish(cuda_req, "nextpnr", content="cuda")
        stale_hip = _publish(stale_hip_req, "nextpnr", content="stale-hip")
        for req in (off_req, hip_req, cuda_req, stale_hip_req):
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        # Keeping HIP + OFF with ROCM_PATH and CUDA_HOME present scopes ROCM_PATH
        # to HIP and keeps OFF's environment empty; CUDA and stale HIP age out.
        report, _ = self.prune(
            gpu_routers=("hip", "OFF", "HIP"),
            compiler=both_compilers,
            environment={"ROCM_PATH": "/opt/rocm-6.3", "CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        self.assertEqual(
            set(report["kept"]),
            {f"tools/nextpnr/{off_install.key}", f"tools/nextpnr/{hip_install.key}"},
        )
        self.assertEqual(
            set(report["removed"]),
            {f"tools/nextpnr/{cuda_install.key}", f"tools/nextpnr/{stale_hip.key}"},
        )

        # Keeping all three lanes pins OFF, HIP and CUDA while still pruning stale HIP.
        report, _ = self.prune(
            gpu_routers=("OFF", "HIP", "CUDA"),
            compiler=both_compilers,
            environment={"ROCM_PATH": "/opt/rocm-6.3", "CUDA_HOME": "/usr/local/cuda-12.8"},
        )
        self.assertEqual(
            set(report["kept"]),
            {
                f"tools/nextpnr/{off_install.key}",
                f"tools/nextpnr/{hip_install.key}",
                f"tools/nextpnr/{cuda_install.key}",
            },
        )
        self.assertEqual(report["removed"], [f"tools/nextpnr/{stale_hip.key}"])

    def test_prune_preserves_explicitly_empty_backend_variables_like_request_environment(self):
        """An install built with an empty backend var (e.g. CUDA_HOME='') stays pinned."""
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        hip_compiler = {"commands": {**COMPILER["commands"], "hipcc": hipcc}}
        env = {"ROCM_PATH": "/opt/rocm-6.3", "CUDA_HOME": ""}
        toolchain_cache.validate_shared_environment(env, gpu_router="HIP")
        request_env = toolchain_cache._request_environment(env, tool="nextpnr")
        self.assertEqual(request_env, {"CUDA_HOME": "", "ROCM_PATH": "/opt/rocm-6.3"})

        hip_req = self.request(
            gpu_router="HIP",
            compiler_identity=hip_compiler,
            environment=request_env,
        )
        off_req = self.request(
            gpu_router="OFF",
            environment={"CUDA_HOME": ""},
        )
        hip_install = _publish(hip_req, "nextpnr", content="hip-empty-cuda")
        off_install = _publish(off_req, "nextpnr", content="off-empty-cuda")
        old = time.time() - 30 * 86400
        for req in (hip_req, off_req):
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        report, output = self.prune(
            gpu_routers=("HIP", "OFF"),
            compiler=hip_compiler,
            environment=env,
        )
        self.assertEqual(
            set(report["kept"]),
            {f"tools/nextpnr/{hip_install.key}", f"tools/nextpnr/{off_install.key}"},
        )
        self.assertIn(f"kept: tools/nextpnr/{hip_install.key} (pinned at HEAD)", output)
        self.assertIn(f"kept: tools/nextpnr/{off_install.key} (pinned at HEAD)", output)
        self.assertEqual(report["removed"], [])

    def test_default_prune_keeps_pinned_hip_and_off_nextpnr_on_a_hip_host(self):
        """With no lane named, a pinned HIP nextpnr is as safe as the OFF one.

        Core producers and the toolchain-fes* targets build HIP, and
        toolchain-fes-i386 builds OFF, each naming its lane only for its own
        bootstrap run, so `make toolchain-prune` sees no lane in its
        environment. Both installs main pins must survive an aged prune.
        """
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        hip_compiler = {"commands": {**COMPILER["commands"], "hipcc": hipcc}}
        hip_req = self.request(gpu_router="HIP", compiler_identity=hip_compiler)
        off_req = self.request(gpu_router="OFF")
        # The install an older ROCm built is not current and still ages out.
        stale_hip_req = self.request(
            gpu_router="HIP",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": {**hipcc, "sha256": "hip-0"}}},
        )
        hip_install = _publish(hip_req, "nextpnr", content="hip")
        off_install = _publish(off_req, "nextpnr", content="off")
        stale_hip = _publish(stale_hip_req, "nextpnr", content="stale-hip")
        old = time.time() - 30 * 86400
        for req in (hip_req, off_req, stale_hip_req):
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        report, output = self.prune(compiler=hip_compiler, environment={}, apply=True)
        self.assertEqual(
            set(report["kept"]),
            {f"tools/nextpnr/{hip_install.key}", f"tools/nextpnr/{off_install.key}"},
        )
        self.assertIn(f"kept: tools/nextpnr/{hip_install.key} (pinned at HEAD)", output)
        self.assertIn(f"kept: tools/nextpnr/{off_install.key} (pinned at HEAD)", output)
        self.assertEqual(report["removed"], [f"tools/nextpnr/{stale_hip.key}"])
        self.assertTrue(hip_install.directory.exists())
        self.assertTrue(off_install.directory.exists())
        self.assertFalse(stale_hip.directory.exists())

    def test_default_prune_lanes_follow_the_accelerator_compilers_on_path(self):
        """Without an inventory, the default lanes are OFF plus each accelerator found on PATH."""
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        nvcc = {"path": "/usr/local/cuda/bin/nvcc", "sha256": "cuda-1", "version": "cuda 12.8", "returncode": 0}
        off_req = self.request(gpu_router="OFF")
        hip_req = self.request(
            gpu_router="HIP", compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc}}
        )
        cuda_req = self.request(
            gpu_router="CUDA", compiler_identity={"commands": {**COMPILER["commands"], "nvcc": nvcc}}
        )
        installs = {
            "OFF": _publish(off_req, "nextpnr", content="off"),
            "HIP": _publish(hip_req, "nextpnr", content="hip"),
            "CUDA": _publish(cuda_req, "nextpnr", content="cuda"),
        }
        old = time.time() - 30 * 86400
        for req in (off_req, hip_req, cuda_req):
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        bin_dir = self.base / "bin"
        bin_dir.mkdir()
        identities = {"hipcc": hipcc, "nvcc": nvcc}
        probed: list[str] = []

        def put_on_path(command):
            path = bin_dir / command
            path.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            path.chmod(0o755)

        def fake_command_identity(command, args=("--version",), *, environment=None):
            probed.append(command)
            self.assertEqual(environment["PATH"], str(bin_dir))
            return identities[command]

        def default_prune():
            probed.clear()
            with (
                mock.patch.object(toolchain_cache, "compiler_inventory", return_value=COMPILER),
                mock.patch.object(toolchain_cache, "_command_identity", side_effect=fake_command_identity),
            ):
                report, output = self.prune(compiler=None, environment={"PATH": str(bin_dir)})
            pinned = {
                lane
                for lane, install in installs.items()
                if f"kept: tools/nextpnr/{install.key} (pinned at HEAD)" in output
            }
            self.assertEqual(len(report["kept"]) + len(report["removed"]), len(installs))
            return pinned, sorted(probed)

        # No accelerator compiler on PATH: OFF is the only lane this host builds.
        self.assertEqual(default_prune(), ({"OFF"}, []))
        put_on_path("hipcc")
        self.assertEqual(default_prune(), ({"OFF", "HIP"}, ["hipcc"]))
        put_on_path("nvcc")
        self.assertEqual(default_prune(), ({"OFF", "HIP", "CUDA"}, ["hipcc", "nvcc"]))

    def test_prune_default_lanes_ignore_the_build_lane_variable_and_reject_invalid_lanes(self):
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        hip_compiler = {"commands": {**COMPILER["commands"], "hipcc": hipcc}}
        hip_req = self.request(
            gpu_router="HIP",
            hip_architectures="gfx1201",
            compiler_identity=hip_compiler,
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        off_req = self.request(gpu_router="OFF")
        hip_install = _publish(hip_req, "nextpnr", content="hip")
        off_install = _publish(off_req, "nextpnr", content="off")
        old = time.time() - 30 * 86400
        for req in (hip_req, off_req):
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        # No explicit gpu_routers: the HIP architecture list comes from the
        # environment, but FES_TOOLCHAIN_GPU_ROUTER (one build's lane) neither
        # narrows the lanes this host can build nor adds one it cannot.
        for exported in ("HIP", "OFF", "CUDA"):
            report, _ = self.prune(
                compiler=hip_compiler,
                environment={
                    "FES_TOOLCHAIN_GPU_ROUTER": exported,
                    "FES_TOOLCHAIN_HIP_ARCHITECTURES": "gfx1201",
                    "ROCM_PATH": "/opt/rocm-6.3",
                },
            )
            self.assertEqual(
                set(report["kept"]),
                {f"tools/nextpnr/{hip_install.key}", f"tools/nextpnr/{off_install.key}"},
                exported,
            )
            self.assertEqual(report["removed"], [], exported)

        with self.assertRaisesRegex(toolchain_cache.CacheError, "sequence of lane names"):
            self.prune(gpu_routers="HIP")
        with self.assertRaisesRegex(toolchain_cache.CacheError, "at least one lane"):
            self.prune(gpu_routers=())
        with self.assertRaisesRegex(toolchain_cache.CacheError, "unsupported GPU router"):
            self.prune(gpu_routers=("VULKAN",))
        with self.assertRaisesRegex(toolchain_cache.CacheError, "unsupported GPU router"):
            self.prune(gpu_routers=("",))
        with self.assertRaisesRegex(toolchain_cache.CacheError, "CUDA architectures"):
            self.prune(gpu_routers=("CUDA",), cuda_architectures="native")
        with self.assertRaisesRegex(toolchain_cache.CacheError, "missing required accelerator command: hipcc"):
            self.prune(gpu_routers=("HIP",), compiler=COMPILER)
        with self.assertRaisesRegex(toolchain_cache.CacheError, "missing required accelerator command: nvcc"):
            self.prune(gpu_routers=("CUDA",), compiler=COMPILER)

    def test_prune_probes_only_requested_accelerators_and_cli_forwards_lane_flags(self):
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        hip_req = self.request(
            gpu_router="HIP",
            compiler_identity={"commands": {**COMPILER["commands"], "hipcc": hipcc}},
            environment={"ROCM_PATH": "/opt/rocm-6.3"},
        )
        off_req = self.request(gpu_router="OFF")
        hip_install = _publish(hip_req, "nextpnr", content="hip")
        off_install = _publish(off_req, "nextpnr", content="off")
        old = time.time() - 30 * 86400
        for req in (hip_req, off_req):
            os.utime(toolchain_cache.lock_path(req, "nextpnr"), (old, old))

        probed: list[tuple[str, tuple[str, ...], dict[str, str] | None]] = []

        def fake_command_identity(command, args=("--version",), *, environment=None):
            probed.append((command, args, None if environment is None else dict(environment)))
            self.assertEqual(command, "hipcc")
            return hipcc

        with (
            mock.patch.object(toolchain_cache, "compiler_inventory", return_value=COMPILER) as inv_mock,
            mock.patch.object(toolchain_cache, "_command_identity", side_effect=fake_command_identity),
        ):
            report, _ = self.prune(
                compiler=None,
                gpu_routers=("HIP", "OFF"),
                environment={"PATH": "/usr/bin", "ROCM_PATH": "/opt/rocm-6.3"},
            )
        inv_mock.assert_called_once_with("OFF", environment={"PATH": "/usr/bin", "ROCM_PATH": "/opt/rocm-6.3"})
        self.assertEqual(len(probed), 1)
        self.assertEqual(probed[0][0], "hipcc")
        self.assertEqual(probed[0][2]["ROCM_PATH"], "/opt/rocm-6.3")
        self.assertEqual(
            set(report["kept"]),
            {f"tools/nextpnr/{hip_install.key}", f"tools/nextpnr/{off_install.key}"},
        )

        # Verify CLI parsing forwards repeatable --gpu-router, --max-gb, and architecture flags.
        with mock.patch.object(toolchain_cache, "prune") as prune_mock:
            rc = toolchain_cache._prune_main([
                "--root", str(self.root),
                "--cache", str(self.cache),
                "--ref", "HEAD",
                "--keep-days", "7",
                "--max-gb", "64.5",
                "--gpu-router", "HIP",
                "--gpu-router", "OFF",
                "--hip-architectures", "gfx1201",
                "--cuda-architectures", "75;86",
                "--apply",
            ])
        self.assertEqual(rc, 0)
        prune_mock.assert_called_once_with(
            self.cache,
            self.root,
            keep_days=7.0,
            ref="HEAD",
            apply=True,
            legacy_slots=False,
            max_gb=64.5,
            gpu_routers=["HIP", "OFF"],
            hip_architectures="gfx1201",
            cuda_architectures="75;86",
        )

        # Without lane flags (make toolchain-prune with no GPU_ROUTERS or
        # GPU_ROUTER) the CLI names no lane, so prune keeps the host's lanes.
        with mock.patch.object(toolchain_cache, "prune") as prune_mock:
            rc = toolchain_cache._prune_main([
                "--root", str(self.root),
                "--cache", str(self.cache),
            ])
        self.assertEqual(rc, 0)
        prune_mock.assert_called_once_with(
            self.cache,
            self.root,
            keep_days=14.0,
            ref="origin/main",
            apply=False,
            legacy_slots=False,
            max_gb=None,
            gpu_routers=None,
            hip_architectures=None,
            cuda_architectures=None,
        )

    def _publish_sized(self, request, tool: str, *, payload_bytes: int, tag: str):
        return _publish(request, tool, content=f"{tag}-" + ("x" * payload_bytes))

    def test_prune_max_gb_evicts_lru_unpinned_installs_preserving_pinned_and_held(self):
        """LRU size cap evicts oldest unpinned installs first and never evicts pinned or held ones."""
        payload = 128 * 1024
        pinned_req = self.request()
        pinned = self._publish_sized(pinned_req, "yosys", payload_bytes=payload, tag="pinned")

        unpinned = []
        for idx, commit_char in enumerate(("1", "2", "3")):
            lock = self.base / f"unpinned-{idx}.lock"
            shutil.copy2(self.root / "toolchain.lock", lock)
            _set_pin(lock, "yosys", "commit", commit_char * 40)
            req = self.request(lock)
            inst = self._publish_sized(req, "yosys", payload_bytes=payload, tag=f"u{idx}")
            unpinned.append((req, inst))

        now = time.time()
        # All three are well within keep_days=14, ordered oldest (0) -> middle (1) -> newest (2).
        for idx, (req, _inst) in enumerate(unpinned):
            ts = now - (5 - idx) * 86400
            os.utime(toolchain_cache.lock_path(req, "yosys"), (ts, ts))

        pinned_bytes = toolchain_cache._tree_bytes(pinned.directory)
        newest_bytes = toolchain_cache._tree_bytes(unpinned[2][1].directory)
        self.assertGreater(pinned_bytes, 0)
        self.assertGreater(newest_bytes, 0)
        # Cap allows pinned + newest, forcing eviction of oldest (0) and middle (1).
        cap_gb = (pinned_bytes + newest_bytes + 4096) / 2**30

        # Dry run previews LRU eviction order without deleting anything.
        report, output = self.prune(max_gb=cap_gb, now=now)
        self.assertEqual(
            report["removed"],
            [f"tools/yosys/{unpinned[0][1].key}", f"tools/yosys/{unpinned[1][1].key}"],
        )
        self.assertEqual(
            set(report["kept"]),
            {f"tools/yosys/{pinned.key}", f"tools/yosys/{unpinned[2][1].key}"},
        )
        self.assertIn("dry run", output)
        self.assertTrue(unpinned[0][1].directory.exists())

        # Holding the oldest install skips it as busy and evicts middle + newest to meet the cap.
        holder = os.open(toolchain_cache.lock_path(unpinned[0][0], "yosys"), os.O_RDONLY)
        try:
            fcntl.flock(holder, fcntl.LOCK_SH)
            report, output = self.prune(max_gb=cap_gb, now=now)
            self.assertEqual(report["busy"], [f"tools/yosys/{unpinned[0][1].key}"])
            self.assertEqual(
                report["removed"],
                [f"tools/yosys/{unpinned[1][1].key}", f"tools/yosys/{unpinned[2][1].key}"],
            )
            self.assertEqual(report["kept"], [f"tools/yosys/{pinned.key}"])
            # Cap below pinned alone warns cleanly and never evicts pinned or held installs.
            tiny_report, tiny_out = self.prune(max_gb=0.0, now=now)
            self.assertEqual(tiny_report["kept"], [f"tools/yosys/{pinned.key}"])
            self.assertEqual(tiny_report["busy"], [f"tools/yosys/{unpinned[0][1].key}"])
            self.assertIn("pinned or in-use installs are never evicted", tiny_out)
        finally:
            os.close(holder)

        # Apply via FES_TOOLCHAIN_CACHE_MAX_GB env removes oldest and middle, keeping pinned and newest.
        report, output = self.prune(
            apply=True,
            now=now,
            environment={"FES_TOOLCHAIN_CACHE_MAX_GB": repr(cap_gb)},
        )
        self.assertEqual(
            report["removed"],
            [f"tools/yosys/{unpinned[0][1].key}", f"tools/yosys/{unpinned[1][1].key}"],
        )
        self.assertFalse(unpinned[0][1].directory.exists())
        self.assertFalse(unpinned[1][1].directory.exists())
        self.assertTrue(pinned.directory.exists())
        self.assertTrue(unpinned[2][1].directory.exists())
        self.assertIn("freed", output)

        # Invalid cap values are rejected in both prune() and validate_shared_environment().
        for bad in ("-1", "nan", "inf", "abc"):
            with self.assertRaisesRegex(toolchain_cache.CacheError, "max-gb|FES_TOOLCHAIN_CACHE_MAX_GB"):
                self.prune(environment={"FES_TOOLCHAIN_CACHE_MAX_GB": bad})
            with self.assertRaisesRegex(toolchain_cache.CacheError, "max-gb|FES_TOOLCHAIN_CACHE_MAX_GB"):
                toolchain_cache.validate_shared_environment({"FES_TOOLCHAIN_CACHE_MAX_GB": bad})
        with self.assertRaisesRegex(toolchain_cache.CacheError, "max-gb"):
            self.prune(max_gb=-0.5)
        with self.assertRaisesRegex(toolchain_cache.CacheError, "max-gb"):
            self.prune(max_gb=True)
        # Valid FES_TOOLCHAIN_CACHE_MAX_GB passes environment validation and stays out of build identity.
        toolchain_cache.validate_shared_environment({"FES_TOOLCHAIN_CACHE_MAX_GB": "80"})
        self.assertEqual(
            toolchain_cache._request_environment({"FES_TOOLCHAIN_CACHE_MAX_GB": "80"}, tool="nextpnr"),
            {},
        )

    def test_publish_install_stamps_lock_mtime_and_enforces_max_gb_env(self):
        """Publishing updates lock mtime and enforces FES_TOOLCHAIN_CACHE_MAX_GB without breaking publish."""
        payload = 128 * 1024
        pinned_req = self.request()
        pinned = self._publish_sized(pinned_req, "yosys", payload_bytes=payload, tag="pinned")

        old_lock = self.base / "old.lock"
        shutil.copy2(self.root / "toolchain.lock", old_lock)
        _set_pin(old_lock, "yosys", "commit", "1" * 40)
        old_req = self.request(old_lock)
        old_install = self._publish_sized(old_req, "yosys", payload_bytes=payload, tag="old")
        old_ts = time.time() - 3 * 86400
        os.utime(toolchain_cache.lock_path(old_req, "yosys"), (old_ts, old_ts))

        new_lock = self.base / "new.lock"
        shutil.copy2(self.root / "toolchain.lock", new_lock)
        _set_pin(new_lock, "yosys", "commit", "2" * 40)
        new_req = self.request(new_lock)

        # Pre-create stale lock mtime and huge transient src/ + build/ trees like bootstrap.sh does.
        new_lock_file = toolchain_cache.ensure_lock_file(new_req, "yosys")
        os.utime(new_lock_file, (old_ts, old_ts))
        new_dir = toolchain_cache.install_directory(new_req, "yosys")
        (new_dir / "install" / "bin").mkdir(parents=True)
        (new_dir / "evidence").mkdir()
        (new_dir / "src").mkdir()
        (new_dir / "build").mkdir()
        (new_dir / "install" / "bin" / "yosys").write_text("new-" + ("y" * payload), encoding="utf-8")
        (new_dir / "evidence" / "record").write_text("evidence\n", encoding="utf-8")
        # Huge transient build tree that bootstrap.sh deletes right after publish.
        (new_dir / "build" / "huge.o").write_bytes(b"z" * (4 * payload))

        pinned_bytes = toolchain_cache._tree_bytes(pinned.directory)
        new_install_bytes = toolchain_cache._tree_bytes(new_dir, skip_top_dirs=frozenset({"src", "build"}))
        # Cap fits pinned + new install (excluding transient src/build), so old_install is evicted
        # while pinned and the just-published unpinned install both survive.
        cap_gb = (pinned_bytes + new_install_bytes + 4096) / 2**30

        # Invalid FES_TOOLCHAIN_CACHE_MAX_GB fails before writing ready.json.
        with (
            mock.patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_MAX_GB": "bogus"}),
            toolchain_cache.acquire_build_lock(new_req, "yosys"),
            self.assertRaisesRegex(toolchain_cache.CacheError, "FES_TOOLCHAIN_CACHE_MAX_GB"),
        ):
            toolchain_cache.publish_install(new_req, "yosys", identity="new yosys")
        self.assertFalse((new_dir / "ready.json").exists())

        before_publish = time.time() - 1.0
        stderr = io.StringIO()
        with (
            mock.patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_MAX_GB": repr(cap_gb)}),
            mock.patch.object(toolchain_cache.sys, "stderr", stderr),
            toolchain_cache.acquire_build_lock(new_req, "yosys"),
        ):
            published = toolchain_cache.publish_install(new_req, "yosys", identity="new yosys")
        self.assertGreaterEqual(new_lock_file.stat().st_mtime, before_publish)
        self.assertTrue(published.directory.exists())
        self.assertTrue(pinned.directory.exists())
        self.assertFalse(old_install.directory.exists())
        self.assertIn(f"removed: tools/yosys/{old_install.key}", stderr.getvalue())

        # A non-git checkout logs a warning to stderr and still completes publication cleanly.
        non_git_root = self.base / "non-git"
        (non_git_root / "scripts").mkdir(parents=True)
        shutil.copy2(SOURCE / "scripts" / "lockfile.py", non_git_root / "scripts" / "lockfile.py")
        shutil.copy2(SOURCE / "scripts" / "bootstrap.sh", non_git_root / "scripts" / "bootstrap.sh")
        shutil.copy2(SOURCE / "toolchain.lock", non_git_root / "toolchain.lock")
        non_git_req = self.request(non_git_root / "toolchain.lock", root=non_git_root)
        warn_stderr = io.StringIO()
        with (
            mock.patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_MAX_GB": "1"}),
            mock.patch.object(toolchain_cache.sys, "stderr", warn_stderr),
        ):
            published_ng = _publish(non_git_req, "verilator", content="non-git")
        self.assertTrue((published_ng.directory / "ready.json").is_file())
        self.assertIn("skipping post-publish LRU eviction", warn_stderr.getvalue())

        # Unexpected OSError in best-effort post-publish eviction never fails a completed publish.
        err_lock = self.base / "err.lock"
        shutil.copy2(self.root / "toolchain.lock", err_lock)
        _set_pin(err_lock, "verilator", "commit", "3" * 40)
        err_req = self.request(err_lock)
        err_stderr = io.StringIO()
        with (
            mock.patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_MAX_GB": "1"}),
            mock.patch.object(toolchain_cache.sys, "stderr", err_stderr),
            mock.patch.object(toolchain_cache, "prune", side_effect=OSError("transient io error")),
        ):
            published_err = _publish(err_req, "verilator", content="oserr")
        self.assertTrue((published_err.directory / "ready.json").is_file())
        self.assertIn("skipping post-publish LRU eviction: transient io error", err_stderr.getvalue())

    def test_prune_handles_concurrent_eviction_race_without_failing(self):
        """An install removed by a concurrent prune before rename is treated as already freed."""
        payload = 128 * 1024
        pinned = self._publish_sized(self.request(), "yosys", payload_bytes=payload, tag="pinned")
        unpinned = []
        for idx, commit_char in enumerate(("4", "5")):
            lock = self.base / f"race-{idx}.lock"
            shutil.copy2(self.root / "toolchain.lock", lock)
            _set_pin(lock, "yosys", "commit", commit_char * 40)
            req = self.request(lock)
            inst = self._publish_sized(req, "yosys", payload_bytes=payload, tag=f"r{idx}")
            unpinned.append((req, inst))
        now = time.time()
        os.utime(toolchain_cache.lock_path(unpinned[0][0], "yosys"), (now - 4 * 86400, now - 4 * 86400))
        os.utime(toolchain_cache.lock_path(unpinned[1][0], "yosys"), (now - 1 * 86400, now - 1 * 86400))

        pinned_bytes = toolchain_cache._tree_bytes(pinned.directory)
        newer_bytes = toolchain_cache._tree_bytes(unpinned[1][1].directory)
        cap_gb = (pinned_bytes + newer_bytes + 4096) / 2**30

        real_rename = os.rename
        raced_dir = unpinned[0][1].directory

        def racing_rename(src, dst):
            if Path(src) == raced_dir:
                toolchain_cache._remove_tree(raced_dir)
                raise FileNotFoundError(src)
            return real_rename(src, dst)

        with mock.patch.object(toolchain_cache.os, "rename", side_effect=racing_rename):
            report, _ = self.prune(apply=True, max_gb=cap_gb, now=now)
        self.assertFalse(raced_dir.exists())
        self.assertEqual(report["removed"], [])
        self.assertIn(f"tools/yosys/{unpinned[1][1].key}", report["kept"])
        self.assertTrue(unpinned[1][1].directory.exists())

    def test_post_publish_eviction_keeps_this_builds_and_heads_installs(self):
        """A tight cap never evicts what this build's lock or the locks at HEAD select.

        Bootstrap releases each install's lock once it is published or hit and
        the producer resolves them only after the last tool, so on a branch
        whose locks pin installs origin/main does not, a lock held by nobody
        cannot be what keeps them.
        """
        git = ["git", "-C", str(self.repository), "-c", "user.name=t", "-c", "user.email=t@example.invalid"]
        subprocess.run([*git, "update-ref", "refs/remotes/origin/main", "HEAD"], check=True)
        # Built for origin/main; the branch moves every lock off this Verilator.
        main_only = _publish(self.request(), "verilator")
        core_lock = self.root / "cores" / "fes-core" / "toolchain.lock"
        for lock in (self.root / "toolchain.lock", core_lock):
            _set_pin(lock, "verilator", "commit", "e" * 40)
        _set_pin(core_lock, "openfpgaloader", "commit", "1" * 40)
        subprocess.run([*git, "commit", "-q", "-a", "-m", "branch"], check=True)
        # Another lock at HEAD, built earlier: nobody holds it now.
        head_only = _publish(self.request(core_lock), "openfpgaloader")
        # An install no lock selects, used long ago: what the cap does evict.
        stale_lock = self.base / "stale.lock"
        shutil.copy2(self.root / "toolchain.lock", stale_lock)
        _set_pin(stale_lock, "yosys", "commit", "d" * 40)
        stale = self.request(stale_lock)
        stale_install = _publish(stale, "yosys")
        old = time.time() - 30 * 86400
        os.utime(toolchain_cache.lock_path(stale, "yosys"), (old, old))
        # This build's own lock is an uncommitted edit: no ref pins its Yosys.
        _set_pin(self.root / "toolchain.lock", "yosys", "commit", "9" * 40)
        build = self.request()

        stderr = io.StringIO()
        with (
            mock.patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_MAX_GB": "0"}),
            mock.patch.object(toolchain_cache.sys, "stderr", stderr),
        ):
            # Bootstrap's order: each publish releases its lock before the next.
            installs = {tool: _publish(build, tool) for tool in toolchain_cache.TOOLS}
        log = stderr.getvalue()
        self.assertFalse(stale_install.directory.exists(), log)
        self.assertIn(f"removed: tools/yosys/{stale_install.key}", log)
        for install in installs.values():
            self.assertTrue((install.directory / "ready.json").is_file(), log)
        toolchain_cache.verify_ready(build)
        self.assertEqual({tool: install.key for tool, install in installs.items()}, toolchain_cache.tool_keys(build))
        self.assertIn(f"kept: tools/yosys/{installs['yosys'].key} (selected by this build's lock)", log)
        self.assertTrue(head_only.directory.exists(), log)
        self.assertIn(f"kept: tools/openfpgaloader/{head_only.key} (pinned at HEAD)", log)
        self.assertTrue(main_only.directory.exists(), log)
        self.assertIn(f"kept: tools/verilator/{main_only.key} (pinned at origin/main)", log)

    def test_publish_install_takes_the_cap_bootstrap_forwards(self):
        """Bootstrap scrubs FES_TOOLCHAIN_CACHE_MAX_GB and forwards its text to publish."""
        stale_lock = self.base / "stale.lock"
        shutil.copy2(self.root / "toolchain.lock", stale_lock)
        _set_pin(stale_lock, "yosys", "commit", "d" * 40)
        stale = self.request(stale_lock)
        stale_install = _publish(stale, "yosys")
        old = time.time() - 30 * 86400
        os.utime(toolchain_cache.lock_path(stale, "yosys"), (old, old))
        build = self.request()

        # Blank text means no cap, as a blank variable does, and wins over the variable.
        stderr = io.StringIO()
        with (
            mock.patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_MAX_GB": "0"}),
            mock.patch.object(toolchain_cache.sys, "stderr", stderr),
        ):
            _publish(build, "yosys", max_gb=" ")
        self.assertEqual(stderr.getvalue(), "")
        self.assertTrue(stale_install.directory.exists())
        # The scrubbed build environment has no cap of its own.
        stderr = io.StringIO()
        with mock.patch.dict(os.environ), mock.patch.object(toolchain_cache.sys, "stderr", stderr):
            os.environ.pop("FES_TOOLCHAIN_CACHE_MAX_GB", None)
            _publish(build, "verilator", max_gb=" 0 ")
        self.assertIn(f"removed: tools/yosys/{stale_install.key}", stderr.getvalue())
        self.assertFalse(stale_install.directory.exists())
        toolchain_cache.verify_install(build, "yosys")
        with self.assertRaisesRegex(toolchain_cache.CacheError, "invalid max-gb value: 'many'"):
            toolchain_cache._parse_max_gb("many", {})

    def test_compiler_for_post_publish_eviction_preserves_other_host_gpu_lanes(self):
        """An OFF-lane publish on a GPU host still keeps pinned HIP nextpnr protected."""
        hipcc = {"path": "/opt/rocm/bin/hipcc", "sha256": "hip-1", "version": "rocm 6.3", "returncode": 0}
        real_off_compiler = {"commands": dict(COMPILER["commands"]), "dependencies": {}}

        def fake_command_path(command, environment=None):
            return "/opt/rocm/bin/hipcc" if command == "hipcc" else None

        def fake_command_identity(command, args=("--version",), *, environment=None):
            self.assertEqual(command, "hipcc")
            return hipcc

        with (
            mock.patch.object(toolchain_cache, "_command_path", side_effect=fake_command_path),
            mock.patch.object(toolchain_cache, "_command_identity", side_effect=fake_command_identity),
        ):
            enriched = toolchain_cache._compiler_for_post_publish_eviction(
                real_off_compiler, {"PATH": "/opt/rocm/bin:/usr/bin"}
            )
        self.assertEqual(enriched["commands"]["hipcc"], hipcc)
        self.assertEqual(
            toolchain_cache._host_gpu_routers(environment={}, compiler=enriched),
            ("OFF", "HIP"),
        )

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
