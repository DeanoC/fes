import hashlib
import json
import os
import stat
import tempfile
import unittest
from pathlib import Path

from scripts import toolchain_cache


class ToolchainCacheContractTests(unittest.TestCase):
    def _request(self, root: Path, cache: Path, recipe: Path, *, lane: str = "OFF"):
        root.mkdir(parents=True, exist_ok=True)
        cache.mkdir(parents=True, exist_ok=True)
        cache.chmod(0o700)
        lock = root / "toolchain.lock"
        lock.write_text("lock-content\n", encoding="utf-8")
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

            manifest = toolchain_cache.publish_ready(
                request,
                tools={
                    "yosys": {
                        "binary": "bin/yosys",
                        "commit": "pin",
                        "identity": "fake",
                    }
                },
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


if __name__ == "__main__":
    unittest.main()
