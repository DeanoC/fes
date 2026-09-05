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

            result = self.module().prepare(fogcast, bundle)

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


if __name__ == "__main__":
    unittest.main()
