"""Parent format-2 recipe registry and producer dispatch."""
from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import recipes
import build
from tests.test_bundle import BundleTest


class RecipeRegistryTest(unittest.TestCase):
    def test_registry_contains_exactly_three_hip_descriptors(self):
        self.assertEqual(tuple(recipes.FORMAT2_RECIPES), ("fes.pong", "fes.zx81", "fes.coleco"))
        expected = {
            "fes.pong": {
                "producer_script": "scripts/build_fes_pong.py",
                "producer_module": "scripts.build_fes_pong",
                "lock_path": "toolchain.lock",
                "selection_filename": "fes-pong.package-selection.toml",
            },
            "fes.zx81": {
                "producer_script": "scripts/build_fes_zx81_oss.py",
                "producer_module": "scripts.build_fes_zx81_oss",
                "lock_path": "toolchain.lock",
                "selection_filename": "fes-zx81.package-selection.toml",
            },
            "fes.coleco": {
                "producer_script": "scripts/build_fes_coleco_oss.py",
                "producer_module": "scripts.build_fes_coleco_oss",
                "lock_path": "cores/fes-coleco/toolchain.lock",
                "selection_filename": "fes-coleco.package-selection.toml",
            },
        }
        for core_id, fields in expected.items():
            recipe = recipes.recipe_for(core_id)
            self.assertEqual(recipe.core_id, core_id)
            self.assertEqual(recipe.producer_script, fields["producer_script"])
            self.assertEqual(recipe.producer_module, fields["producer_module"])
            self.assertEqual(recipe.lock_path, fields["lock_path"])
            self.assertEqual(recipe.gpu_router, "HIP")
            self.assertEqual(recipe.hip_architectures, "gfx1100;gfx1201")
            self.assertEqual(recipe.selection_filename, fields["selection_filename"])
            self.assertEqual(recipe.cache_root, recipes.TOOLCHAIN_CACHE_ROOT)
        pong = recipes.recipe_for("fes.pong")
        zx81 = recipes.recipe_for("fes.zx81")
        coleco = recipes.recipe_for("fes.coleco")
        self.assertEqual(pong.lock_path, zx81.lock_path)
        self.assertNotEqual(coleco.lock_path, pong.lock_path)
        self.assertEqual(pong.cache_root, coleco.cache_root)
        with self.assertRaisesRegex(ValueError, "unknown format-2 recipe"):
            recipes.recipe_for("fes.unknown")

    def test_profile_rejects_unknown_duplicate_and_multi_package_image_selection(self):
        pong = {"fpga_packages": [{"core_id": "fes.pong"}]}
        self.assertEqual(build.selected_packages(pong, "native-integration-dev"), ("fes.pong",))
        self.assertEqual(build.selected_packages({}, "native-dev"), ())
        cases = (
            ({"fpga_packages": [{"core_id": "fes.unknown"}]}, "unknown"),
            ({"fpga_packages": [{"core_id": "fes.pong"}, {"core_id": "fes.pong"}]}, "duplicate"),
            ({"fpga_packages": [{"core_id": "fes.pong"}, {"core_id": "fes.zx81"}]}, "multiple"),
            ({"fpga_packages": [{"core_id": "fes.zx81"}]}, "fes.zx81"),
            ({"fpga_packages": [{"core_id": "fes.coleco"}]}, "fes.coleco"),
        )
        for profile, needle in cases:
            with self.subTest(profile=profile), self.assertRaisesRegex(ValueError, needle):
                build.selected_packages(profile, "native-integration-dev")


class RecipeResolverTest(unittest.TestCase):
    def module(self):
        return BundleTest.module(self)


    def test_each_descriptor_dispatches_its_producer_with_explicit_cache_root(self):
        module = self.module()
        for core_id in ("fes.pong", "fes.zx81", "fes.coleco"):
            with self.subTest(core_id=core_id), tempfile.TemporaryDirectory() as temporary:
                source = Path(temporary)
                recipe = recipes.recipe_for(core_id)
                completed = subprocess.CompletedProcess(["python"], 0)
                caller = {
                    "KEEP": "1",
                    "MAKEFLAGS": "s",
                    "CC": "clang",
                    "TOOLCHAIN_ROOT": "/local-tools",
                    "FES_TOOLCHAIN_CACHE_ROOT": "/ambient-cache",
                    "FES_TOOLCHAIN_ROOT": "/ambient-root",
                    "ROCM_PATH": "/opt/rocm/core-7.14",
                    "HIPCC": "/opt/rocm/core-7.14/lib/llvm/bin/clang++",
                    "FES_TOOLCHAIN_GPU_ROUTER": "OFF",
                }
                with patch.object(module.subprocess, "run", return_value=completed) as run:
                    module._build_package(source, recipe=recipe, env=caller)
                args = [str(part) for part in run.call_args.args[0]]
                env = run.call_args.kwargs["env"]
                self.assertIn(recipe.producer_script, args)
                self.assertIn("--cache-root", args)
                self.assertEqual(args[args.index("--cache-root") + 1], str(module.TOOLCHAIN_CACHE_ROOT))
                self.assertNotIn("FES_TOOLCHAIN_CACHE_ROOT", env)
                self.assertNotIn("FES_TOOLCHAIN_ROOT", env)
                self.assertNotIn("MAKEFLAGS", env)
                self.assertNotIn("CC", env)
                self.assertNotIn("TOOLCHAIN_ROOT", env)
                self.assertEqual(env["FES_TOOLCHAIN_GPU_ROUTER"], "HIP")
                self.assertEqual(env["FES_TOOLCHAIN_HIP_ARCHITECTURES"], "gfx1100;gfx1201")
                self.assertEqual(env["ROCM_PATH"], "/opt/rocm/core-7.14")
                self.assertEqual(env["HIPCC"], "/opt/rocm/core-7.14/lib/llvm/bin/clang++")
                self.assertEqual(env["KEEP"], "1")
                self.assertEqual(caller["FES_TOOLCHAIN_CACHE_ROOT"], "/ambient-cache")
                self.assertNotIn("FES_TOOLCHAIN_CACHE_ROOT", os.environ)

    def test_canonical_record_passes_cache_root_and_keeps_hip_identity(self):
        module = self.module()
        recipe = recipes.recipe_for("fes.zx81")
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            payload = b'{"canonical":true}\n'
            caller = {"ROCM_PATH": "/opt/rocm", "HIPCC": "/opt/rocm/bin/hipcc", "PYTHON": "python3"}
            with patch.object(module, "authenticate_misteross_origin"), \
                 patch.object(module.subprocess, "check_output", return_value=payload) as check:
                module.canonical_package_record(source, env=caller, recipe=recipe)
            args = [str(part) for part in check.call_args.args[0]]
            env = check.call_args.kwargs["env"]
            self.assertIn("--cache-root", args)
            self.assertEqual(args[args.index("--cache-root") + 1], str(module.TOOLCHAIN_CACHE_ROOT))
            self.assertIn(recipe.producer_module, check.call_args.args[0])
            self.assertNotIn("FES_TOOLCHAIN_CACHE_ROOT", env)
            self.assertNotIn("PYTHON", env)
            self.assertEqual(env["FES_TOOLCHAIN_GPU_ROUTER"], "HIP")
            self.assertEqual(env["ROCM_PATH"], "/opt/rocm")
            self.assertEqual(env["HIPCC"], "/opt/rocm/bin/hipcc")

    def test_resolver_uses_descriptor_core_id_and_selection_filename(self):
        module = self.module()
        recipe = recipes.recipe_for("fes.coleco")
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / "build/packages"
            store.mkdir(parents=True)
            identity = "a" * 64
            record = b'{"canonical":true}\n'
            sidecar = store / f"{identity}.build-inputs.json"
            sidecar.write_bytes(record)
            sidecar.chmod(0o444)
            package = store / identity
            package.mkdir()
            (package / "manifest.toml").write_bytes(b"manifest")
            (package / "core.rbf").write_bytes(b"payload")
            payload_sha = __import__("hashlib").sha256(b"payload").hexdigest()
            inspected = {
                "package_id": identity,
                "manifest": {"core": {"id": "fes.coleco"},
                             "payload": {"sha256": payload_sha},
                             "build": {"revision": "c" * 40}},
                "manifest_sha256": __import__("hashlib").sha256(b"manifest").hexdigest(),
                "core_rbf_sha256": payload_sha,
            }
            selection_path = source / recipe.selection_filename
            with patch.object(module, "canonical_package_record", return_value=record), \
                 patch.object(module, "_inspect_package_candidate", return_value=inspected), \
                 patch.object(module, "_build_package", side_effect=AssertionError("unexpected build")):
                resolved = module.resolve_core_package(
                    source, "d" * 40, selection_path, recipe=recipe)
            self.assertEqual(resolved["inputs"]["selection"]["core_id"], "fes.coleco")
            self.assertEqual(selection_path.name, "fes-coleco.package-selection.toml")
            self.assertTrue(selection_path.is_file())

    def test_package_miss_builds_selected_producer_once_and_reuse_builds_none(self):
        module = self.module()
        recipe = recipes.recipe_for("fes.zx81")
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / "build/packages"
            identity = "a" * 64
            record = b'{"canonical":true}\n'
            payload_sha = __import__("hashlib").sha256(b"payload").hexdigest()
            inspected = {
                "package_id": identity,
                "manifest": {"core": {"id": "fes.zx81"},
                             "payload": {"sha256": payload_sha},
                             "build": {"revision": "c" * 40}},
                "manifest_sha256": __import__("hashlib").sha256(b"manifest").hexdigest(),
                "core_rbf_sha256": payload_sha,
            }

            def build_once(*args, **kwargs):
                store.mkdir(parents=True)
                (store / f"{identity}.build-inputs.json").write_bytes(record)
                (store / f"{identity}.build-inputs.json").chmod(0o444)
                (store / identity).mkdir()
                (store / identity / "manifest.toml").write_bytes(b"manifest")
                (store / identity / "core.rbf").write_bytes(b"payload")

            with patch.object(module, "canonical_package_record", return_value=record), \
                 patch.object(module, "_inspect_package_candidate", return_value=inspected), \
                 patch.object(module, "_build_package", side_effect=build_once) as built:
                module.resolve_core_package(
                    source, "d" * 40, source / recipe.selection_filename, recipe=recipe)
            built.assert_called_once()
            self.assertIs(built.call_args.kwargs["recipe"], recipe)
            with patch.object(module, "canonical_package_record", return_value=record), \
                 patch.object(module, "_inspect_package_candidate", return_value=inspected), \
                 patch.object(module, "_build_package",
                              side_effect=AssertionError("reuse must not build")):
                module.resolve_core_package(
                    source, "d" * 40, source / recipe.selection_filename, recipe=recipe)

    def test_mismatched_package_core_id_is_rejected(self):
        module = self.module()
        recipe = recipes.recipe_for("fes.zx81")
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / "build/packages"
            store.mkdir(parents=True)
            identity = "a" * 64
            record = b'{"canonical":true}\n'
            sidecar = store / f"{identity}.build-inputs.json"
            sidecar.write_bytes(record)
            sidecar.chmod(0o444)
            package = store / identity
            package.mkdir()
            (package / "manifest.toml").write_bytes(b"manifest")
            (package / "core.rbf").write_bytes(b"payload")
            payload_sha = __import__("hashlib").sha256(b"payload").hexdigest()
            inspected = {
                "package_id": identity,
                "manifest": {"core": {"id": "fes.pong"},
                             "payload": {"sha256": payload_sha},
                             "build": {"revision": "c" * 40}},
                "manifest_sha256": __import__("hashlib").sha256(b"manifest").hexdigest(),
                "core_rbf_sha256": payload_sha,
            }
            with patch.object(module, "canonical_package_record", return_value=record), \
                 patch.object(module, "_inspect_package_candidate", return_value=inspected), \
                 patch.object(module, "_build_package", side_effect=AssertionError("unexpected build")):
                with self.assertRaisesRegex(ValueError, "fes.zx81"):
                    module.resolve_core_package(
                        source, "d" * 40, source / recipe.selection_filename, recipe=recipe)


if __name__ == "__main__":
    unittest.main()
