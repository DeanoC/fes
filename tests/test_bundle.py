import importlib.util
import hashlib
from pathlib import Path
import tempfile
import tomllib
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/bundle.py"


class BundleTest(unittest.TestCase):
    def module(self):
        self.assertTrue(SCRIPT.exists(), "bundle integration is not implemented")
        spec = importlib.util.spec_from_file_location("bundle", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_prepare_replaces_only_megadrive_input_and_lock_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fogcast = root / "FogCast"
            cache = fogcast / "build/cache/target-image/native"
            cache.mkdir(parents=True)
            (cache / "idle.rbf").write_bytes(b"idle")
            (cache / "megadrive.rbf").write_bytes(b"upstream")
            lock = fogcast / "build/native-runtime.inputs.lock.toml"
            lock.write_text("""format = 1
[mister_runtime]
commit = '1111111111111111111111111111111111111111'
mount_path = '/runtime-source'
[idle_rbf]
sha256 = 'idle'
[megadrive_rbf]
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
commit = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
path = 'releases/MegaDrive_20260603.rbf'
sha256 = 'upstream'
size = 8
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
""")
            bundle = root / "bundle"
            bundle.mkdir()
            payload = b"source-built-rbf"
            (bundle / "megadrive.rbf").write_bytes(payload)
            digest = hashlib.sha256(payload).hexdigest()
            (bundle / "megadrive-rbf.toml").write_text(f"""format = 1
abi = "mister"
system = "megadrive"
artifact = "megadrive.rbf"
sha256 = "{digest}"
size = {len(payload)}
repository = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
revision = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
recipe = "scripts/rebuild_core.py"
recipe_sha256 = "{'2' * 64}"
toolchain = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
""")

            module = self.module()
            original_lock = lock.read_text()
            with self.assertRaisesRegex(ValueError, "recipe digest differs"):
                module.load(bundle, expected_recipe_sha256="3" * 64)
            with self.assertRaisesRegex(ValueError, "recipe digest differs"):
                module.prepare(fogcast, bundle, expected_recipe_sha256="3" * 64)
            self.assertEqual(lock.read_text(), original_lock)
            self.assertEqual((cache / "megadrive.rbf").read_bytes(), b"upstream")
            self.assertEqual(module.load(bundle)["recipe_sha256"], "2" * 64)
            self.assertEqual(module.load(bundle, "2" * 64)["recipe_sha256"], "2" * 64)

            result = module.prepare(fogcast, bundle, expected_recipe_sha256="2" * 64)

            self.assertEqual((cache / "megadrive.rbf").read_bytes(), payload)
            self.assertEqual((cache / "idle.rbf").read_bytes(), b"idle")
            parsed = tomllib.loads(lock.read_text())
            self.assertEqual(parsed["megadrive_rbf"]["sha256"], digest)
            self.assertEqual(parsed["megadrive_rbf"]["size"], len(payload))
            self.assertEqual(parsed["megadrive_rbf"]["artifact_kind"], "source_rebuild_bundle")
            self.assertEqual(result["sha256"], digest)

    def test_rejects_manifest_or_payload_mismatch(self):
        with tempfile.TemporaryDirectory() as temporary:
            bundle = Path(temporary)
            (bundle / "megadrive.rbf").write_bytes(b"wrong")
            (bundle / "megadrive-rbf.toml").write_text("""format = 1
abi = "mister"
system = "megadrive"
artifact = "megadrive.rbf"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
size = 5
repository = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
revision = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
recipe = "scripts/rebuild_core.py"
recipe_sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
toolchain = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
""")
            with self.assertRaisesRegex(ValueError, "digest"):
                self.module().load(bundle)

    def test_all_systems_and_identity_drift(self):
        module = self.module()
        for system, repository, revision, recipe in (
            ("megadrive", module.REPOSITORY, module.REVISION, "scripts/rebuild_core.py"),
            ("snes", "https://github.com/MiSTer-devel/SNES_MiSTer",
             "93d359e6f23c734ae3928984e88bed1d9b53cbac", "scripts/rebuild_core.py"),
            ("nes", "https://github.com/MiSTer-devel/NES_MiSTer",
             "9a63821173b6da4d6e95dcbe2e2a322ec8171144", "scripts/rebuild_core.py"),
            ("pong", "https://github.com/DeanoC/misteross", "1" * 40, "scripts/build_pong.py"),
        ):
            with self.subTest(system=system), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                artifact = directory / f"{system}.rbf"
                artifact.write_bytes(b"rbf")
                manifest_path = directory / f"{system}-rbf.toml"
                manifest = dict(format=1, abi="mister", system=system,
                                artifact=artifact.name, repository=repository,
                                revision=revision, recipe=recipe, toolchain=module.TOOLCHAIN,
                                sha256=hashlib.sha256(b"rbf").hexdigest(), size=3,
                                recipe_sha256="2" * 64)
                def write(values):
                    manifest_path.write_text("".join(f"{key} = {value!r}\n" for key, value in values.items()))
                def load():
                    return module.load(directory, "2" * 64, system=system, expected_revision=revision)
                write(manifest)
                self.assertEqual(load(), manifest)
                for field, value in (("system", "other"), ("repository", "https://example.org/wrong"),
                                     ("revision", "9" * 40), ("recipe", "scripts/wrong.py"),
                                     ("recipe_sha256", "3" * 64), ("toolchain", "wrong"),
                                     ("artifact", "../other.rbf"), ("sha256", "4" * 64),
                                     ("size", 4), ("extra", "unexpected")):
                    with self.subTest(field=field):
                        write(dict(manifest, **{field: value}))
                        with self.assertRaises(ValueError):
                            load()
                write(manifest)
                artifact.write_bytes(b"bad")
                with self.assertRaisesRegex(ValueError, "digest"):
                    load()
                artifact.write_bytes(b"")
                with self.assertRaisesRegex(ValueError, "empty"):
                    load()
                artifact.unlink()
                artifact.mkdir()
                with self.assertRaisesRegex(ValueError, "regular"):
                    load()
                artifact.rmdir()
                target = directory / "payload"
                target.write_bytes(b"rbf")
                artifact.symlink_to(target)
                with self.assertRaisesRegex(ValueError, "symlink"):
                    load()
                artifact.unlink()
                artifact.write_bytes(b"rbf")
                if system == "pong":
                    with self.assertRaisesRegex(ValueError, "requires.*revision"):
                        module.load(directory, system="pong")
                    with self.assertRaisesRegex(ValueError, "revision"):
                        module.load(directory, system="pong", expected_revision="9" * 40)
                else:
                    with self.assertRaisesRegex(ValueError, "revision"):
                        module.load(directory, system=system, expected_revision="9" * 40)
                manifest_path.unlink()
                target.write_text("irrelevant")
                manifest_path.symlink_to(target)
                with self.assertRaisesRegex(ValueError, "symlink"):
                    load()


if __name__ == "__main__":
    unittest.main()
