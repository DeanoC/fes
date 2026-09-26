"""Parent format-2 recipe registry and producer dispatch."""
from dataclasses import replace
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
    def test_registry_preserves_existing_hip_descriptors(self):
        self.assert_existing_descriptors()

    def test_default_registry_uses_functional_identity(self):
        self.assertTrue(recipes.FORMAT2_RECIPES)
        self.assertEqual({r.identity_version for r in recipes.FORMAT2_RECIPES.values()}, {2})

    def test_original_game_uses_existing_recipe_contract(self):
        recipe = recipes.recipe_for("fes.catch")
        self.assertEqual(recipe.producer_module, "scripts.build_fes_catch")
        self.assertEqual(recipe.producer_script, "scripts/build_fes_catch.py")
        self.assertEqual(recipe.authenticate, "_authenticate_tools")
        self.assertEqual(recipe.identity_version, 2)

    def assert_existing_descriptors(self):
        self.assertTrue(
            {"fes.pong", "fes.zx81", "fes.coleco", "fes.sms", "fes.sg1000"}
            <= recipes.FORMAT2_RECIPES.keys())
        expected = {
            "fes.pong": {
                "producer_script": "scripts/build_fes_pong.py",
                "producer_module": "scripts.build_fes_pong",
                "lock_path": "toolchain.lock",
                "selection_filename": "fes-pong.package-selection.toml",
                "authenticate": "_authenticate_tools",
                "package_dir_env": "FES_PONG_PACKAGE_DIR",
                "package_selection_env": "FES_PONG_PACKAGE_SELECTION",
            },
            "fes.zx81": {
                "producer_script": "scripts/build_fes_zx81_oss.py",
                "producer_module": "scripts.build_fes_zx81_oss",
                "lock_path": "toolchains/zx81-expansion.lock",
                "selection_filename": "fes-zx81.package-selection.toml",
                "authenticate": "_authenticate_tools",
                "package_dir_env": "FES_ZX81_PACKAGE_DIR",
                "package_selection_env": "FES_ZX81_PACKAGE_SELECTION",
            },
            "fes.coleco": {
                "producer_script": "scripts/build_fes_coleco_socket_v2.py",
                "producer_module": "scripts.build_fes_coleco_socket_v2",
                "lock_path": "toolchains/coleco-sgm.lock",
                "selection_filename": "fes-coleco.package-selection.toml",
                "authenticate": "authenticate_tools",
                "package_dir_env": "FES_COLECO_PACKAGE_DIR",
                "package_selection_env": "FES_COLECO_PACKAGE_SELECTION",
            },
            "fes.sms": {
                "producer_script": "scripts/build_fes_sms_oss.py",
                "producer_module": "scripts.build_fes_sms_oss",
                "lock_path": "toolchains/fes-sms.lock",
                "selection_filename": "fes-sms.package-selection.toml",
                "authenticate": "_authenticate_sms_tools",
                "package_dir_env": "FES_SMS_PACKAGE_DIR",
                "package_selection_env": "FES_SMS_PACKAGE_SELECTION",
            },
            "fes.sg1000": {
                "producer_script": "scripts/build_fes_sg1000_oss.py",
                "producer_module": "scripts.build_fes_sg1000_oss",
                "lock_path": "toolchains/registered-memory.lock",
                "selection_filename": "fes-sg1000.package-selection.toml",
                "authenticate": "_authenticate_sg1000_tools",
                "package_dir_env": "FES_SG1000_PACKAGE_DIR",
                "package_selection_env": "FES_SG1000_PACKAGE_SELECTION",
            },
        }
        for core_id, fields in expected.items():
            recipe = recipes.recipe_for(core_id)
            self.assertEqual(recipe.core_id, core_id)
            self.assertEqual(recipe.producer_script, fields["producer_script"])
            self.assertEqual(recipe.producer_module, fields["producer_module"])
            self.assertEqual(recipe.lock_path, fields["lock_path"])
            self.assertEqual(recipe.authenticate, fields["authenticate"])
            self.assertEqual(recipe.package_dir_env, fields["package_dir_env"])
            self.assertEqual(recipe.package_selection_env, fields["package_selection_env"])
            self.assertEqual(recipe.gpu_router, "HIP")
            self.assertEqual(recipe.hip_architectures, "gfx1100;gfx1201")
            self.assertEqual(recipe.selection_filename, fields["selection_filename"])
            self.assertEqual(recipe.cache_root, recipes.TOOLCHAIN_CACHE_ROOT)
            self.assertEqual(recipe.quartus_role, "check only when a twin exists"
                             if core_id == "fes.pong" else "bring-up/check oracle")
        pong = recipes.recipe_for("fes.pong")
        zx81 = recipes.recipe_for("fes.zx81")
        coleco = recipes.recipe_for("fes.coleco")
        sms = recipes.recipe_for("fes.sms")
        self.assertNotEqual(pong.lock_path, zx81.lock_path)
        self.assertNotEqual(coleco.lock_path, pong.lock_path)
        self.assertNotEqual(sms.lock_path, pong.lock_path)
        self.assertNotEqual(sms.lock_path, coleco.lock_path)
        self.assertEqual(pong.cache_root, coleco.cache_root)
        self.assertEqual(pong.cache_root, sms.cache_root)
        with self.assertRaisesRegex(ValueError, "unknown format-2 recipe"):
            recipes.recipe_for("fes.unknown")

    def test_profile_rejects_unknown_duplicate_and_malformed_package_selection(self):
        pong = {"fpga_packages": [{"core_id": "fes.pong"}]}
        self.assertEqual(build.selected_packages(pong, "native-integration-dev"), ("fes.pong",))
        self.assertEqual(build.selected_packages(
            {"fpga_packages": [{"core_id": "fes.pong"}, {"core_id": "fes.zx81"}]},
            "native-integration-dev"), ("fes.pong", "fes.zx81"))
        self.assertEqual(build.selected_packages({}, "native-dev"), ())
        self.assertEqual(build.selected_packages(
            {"fpga_packages": [{"core_id": "fes.sms"}]},
            "native-integration-dev"), ("fes.sms",))
        cases = (
            ({"fpga_packages": [{"core_id": "fes.unknown"}]}, "unknown"),
            ({"fpga_packages": [{"core_id": "fes.pong"}, {"core_id": "fes.pong"}]}, "duplicate"),
            ({"fpga_packages": [{"core_id": "fes.pong"}, "fes.zx81"]}, "entries"),
        )
        for profile, needle in cases:
            with self.subTest(profile=profile), self.assertRaisesRegex(ValueError, needle):
                build.selected_packages(profile, "native-integration-dev")



class RecipeDataTest(unittest.TestCase):
    def load_document(self, document):
        # Fixture serialization only: the production loader always parses TOML.
        import json
        text = "version = " + json.dumps(document["version"]) + "\n"
        for key, value in document.items():
            if key not in ("version", "recipes"):
                text += key + " = " + json.dumps(value) + "\n"
        for entry in document["recipes"]:
            text += "\n[[recipes]]\n"
            for key, value in entry.items():
                text += key + " = " + json.dumps(value) + "\n"
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "recipes.toml"
            path.write_text(text)
            return recipes.load_recipes(path)

    def document(self):
        import tomllib
        return tomllib.loads(recipes.REGISTRY_PATH.read_text())

    def test_fifth_recipe_is_only_data(self):
        document = self.document()
        fifth = dict(document["recipes"][0])
        fifth.update(
            core_id="fes.fixture",
            producer_script="scripts/build_fes_fixture.py",
            producer_module="scripts.build_fes_fixture",
            selection_filename="fes-fixture.package-selection.toml",
            package_dir_env="FES_FIXTURE_PACKAGE_DIR",
            package_selection_env="FES_FIXTURE_PACKAGE_SELECTION",
        )
        document["recipes"].append(fifth)
        loaded = self.load_document(document)
        self.assertEqual(tuple(loaded), (*recipes.FORMAT2_RECIPES, "fes.fixture"))
        self.assertEqual(loaded["fes.fixture"], recipes.Format2Recipe(**fifth))
        with patch.object(recipes, "FORMAT2_RECIPES", loaded):
            # The same invariants used for the real registry must accept a
            # data-only extension, without weakening existing descriptor checks.
            RecipeRegistryTest().assert_existing_descriptors()
            self.assertIs(recipes.recipe_for("fes.fixture"), loaded["fes.fixture"])
            env = recipes.producer_environment(
                {"CC": "bad", "MAKEFLAGS": "bad", "KEEP": "yes"},
                recipes.recipe_for("fes.fixture"))
            self.assertNotIn("CC", env)
            self.assertNotIn("MAKEFLAGS", env)
            self.assertEqual(env["FES_TOOLCHAIN_GPU_ROUTER"], "HIP")
            self.assertEqual(env["KEEP"], "yes")

    def test_unknown_and_each_missing_recipe_field_rejected(self):
        for field in self.document()["recipes"][0].keys() - {"identity_version"}:
            document = self.document()
            del document["recipes"][0][field]
            with self.subTest(missing=field), self.assertRaises(ValueError):
                self.load_document(document)
        for field in ("command", "shell", "cache_root"):
            document = self.document()
            document["recipes"][0][field] = "arbitrary"
            with self.subTest(unknown=field), self.assertRaises(ValueError):
                self.load_document(document)

    def test_identity_version_must_be_explicitly_two(self):
        for value in (None, 1):
            document = self.document()
            if value is None:
                del document["recipes"][0]["identity_version"]
            else:
                document["recipes"][0]["identity_version"] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.load_document(document)

    def test_invalid_field_values_rejected(self):
        cases = {
            "core_id": ["", "pong", "fes.Pong", 1],
            "producer_script": ["/scripts/x.py", "../x.py", "scripts/../x.py",
                                "scripts//x.py", "./scripts/x.py", "scripts/x.py;true",
                                "scripts\\x.py", "scripts/other.py"],
            "lock_path": ["/tmp/lock", "../lock", "cores/./lock", "cores//lock",
                          "cores/../lock", "C:\\lock", "lock\n"],
            "producer_module": ["scripts.x;exec", "scripts..x", ".scripts.x",
                                "scripts.class", "scripts.1x"],
            "authenticate": ["x()", "a.b", "class", "x;true", "1x"],
            "gpu_router": ["OFF", "CUDA", "hip"],
            "hip_architectures": ["", "gfx1100;$(true)", "gfx1100;;gfx1201"],
            "selection_filename": ["../x.package-selection.toml",
                                   "/x.package-selection.toml", "x.toml"],
            "package_dir_env": ["PATH", "FES_X_PACKAGE_SELECTION", "FES_X;Y_PACKAGE_DIR"],
            "package_selection_env": ["HOME", "FES_X_PACKAGE_DIR"],
            "quartus_role": ["", False, "oracle\ncommand"],
        }
        for field, values in cases.items():
            for value in values:
                document = self.document()
                document["recipes"][0][field] = value
                with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                    self.load_document(document)

    def test_duplicate_identifiers_rejected(self):
        for field in ("core_id", "selection_filename", "package_dir_env",
                      "package_selection_env"):
            document = self.document()
            document["recipes"][1][field] = document["recipes"][0][field]
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "duplicate"):
                self.load_document(document)

    def test_closed_top_level_schema_and_version(self):
        for text in ('version = 2\nrecipes = []', 'version = true\nrecipes = []',
                     'version = "1"\nrecipes = []', 'version = 1',
                     'recipes = []', 'version = 1\nrecipes = {}',
                     'version = 1\nrecipes = ["bad"]',
                     'version = 1\nrecipes = []',
                     'version = 1\nrecipes = []\ncommand = "true"',
                     'version = 1\nversion = 1\nrecipes = []'):
            with tempfile.TemporaryDirectory() as temporary:
                path = Path(temporary) / "bad.toml"
                path.write_text(text)
                with self.subTest(text=text), self.assertRaises(ValueError):
                    recipes.load_recipes(path)

    def test_import_uses_checkout_registry_not_cwd(self):
        with tempfile.TemporaryDirectory() as temporary:
            result = subprocess.run(
                [sys.executable, "-c",
                 "import sys; sys.path.insert(0, sys.argv[1]); import recipes; "
                 "assert recipes.FORMAT2_RECIPES == recipes.load_recipes(recipes.REGISTRY_PATH)",
                 str(recipes.REGISTRY_PATH.parent.parent / "scripts")],
                cwd=temporary, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)


class RecipeResolverTest(unittest.TestCase):
    def module(self):
        return BundleTest.module(self)


    def test_each_descriptor_dispatches_its_producer_with_explicit_cache_root(self):
        module = self.module()
        for core_id in ("fes.pong", "fes.zx81", "fes.coleco", "fes.sms"):
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
            payload = b'{"format":2,"repository":"https://github.com/DeanoC/fes.git","revision":"cccccccccccccccccccccccccccccccccccccccc","source_inputs":{"x":"y"},"source_path":"sources/misteross"}\n'
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
            record = b'{"format":2,"repository":"https://github.com/DeanoC/fes.git","revision":"cccccccccccccccccccccccccccccccccccccccc","source_inputs":{"x":"y"},"source_path":"sources/misteross"}\n'
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
            record = b'{"format":2,"repository":"https://github.com/DeanoC/fes.git","revision":"cccccccccccccccccccccccccccccccccccccccc","source_inputs":{"x":"y"},"source_path":"sources/misteross"}\n'
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
            record = b'{"format":2,"repository":"https://github.com/DeanoC/fes.git","revision":"cccccccccccccccccccccccccccccccccccccccc","source_inputs":{"x":"y"},"source_path":"sources/misteross"}\n'
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


class CacheLocationTest(unittest.TestCase):
    def test_image_verifier_without_git_can_import_recipes(self):
        import recipes
        from unittest.mock import patch
        with patch.dict(os.environ, {}, clear=True), patch.object(recipes.subprocess, 'run', side_effect=FileNotFoundError):
            self.assertEqual(recipes.shared_cache_root(), Path(recipes.__file__).resolve().parents[1] / 'out/cache')

    def test_explicit_cache_location_does_not_require_git(self):
        import recipes
        from unittest.mock import patch
        with patch.dict(os.environ, {'FES_CACHE_ROOT': '/shared/fes-cache'}), patch.object(recipes.subprocess, 'run', side_effect=AssertionError):
            self.assertEqual(recipes.shared_cache_root(), Path('/shared/fes-cache'))
