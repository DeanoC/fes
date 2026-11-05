import json
import os
import subprocess
import sys
from pathlib import Path
import tempfile
import tomllib
import unittest

from scripts.core_package import PackageError, encode_manifest, package_identity, read_package
from scripts.export_core_package import _archive_bytes
from tests.test_core_package import _rewrite_checksum

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests/fixtures/core-bundle-v5"


class StaticCorePackageTests(unittest.TestCase):
    def test_shared_conformance_corpus_in_directories_and_archives(self):
        for case in json.loads((FIXTURES / "cases.json").read_text()):
            with self.subTest(case=case["name"]), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                manifest = (FIXTURES / case["manifest"]).read_bytes()
                base = (FIXTURES / case["payload"]).read_bytes()
                persona = (FIXTURES / case["persona"]).read_bytes()
                (root / "manifest.toml").write_bytes(manifest)
                (root / "core.rbf").write_bytes(base)
                (root / "persona.rbf").write_bytes(persona)
                archive = root.parent / (root.name + ".fcore")
                try:
                    archive.write_bytes(_archive_bytes(manifest, base, persona=persona))
                    for path in (root, archive):
                        if not case["valid"]:
                            with self.assertRaises(PackageError):
                                read_package(path)
                            continue
                        package = read_package(path)
                        self.assertEqual(case["package_id"], package.package_id)
                        self.assertEqual(persona, package.persona_bytes)
                        self.assertIsNone(package.rom_map_bytes)
                        self.assertEqual(manifest, encode_manifest(tomllib.loads(manifest.decode())))
                        self.assertEqual(case["package_id"], package_identity(manifest, base, persona=persona))
                finally:
                    archive.unlink(missing_ok=True)

    def test_identity_refuses_mixed_or_missing_personas(self):
        manifest = (FIXTURES / "manifests/valid-basic.toml").read_bytes()
        base = (FIXTURES / "payloads/base.rbf").read_bytes()
        persona = (FIXTURES / "payloads/persona.rbf").read_bytes()
        for options in ({"format_version": 5}, {"format_version": 2, "persona": persona},
                        {"format_version": 3, "persona": persona},
                        {"format_version": 5, "persona": persona, "rom_map": b"map"}):
            with self.subTest(options=options), self.assertRaises(PackageError):
                package_identity(manifest, base, **options)
        with self.assertRaises(TypeError):
            package_identity(manifest, base, persona="not bytes")

    def test_wrong_extra_members_and_persona_bytes_fail(self):
        manifest = (FIXTURES / "manifests/valid-basic.toml").read_bytes()
        base = (FIXTURES / "payloads/base.rbf").read_bytes()
        persona = (FIXTURES / "payloads/persona.rbf").read_bytes()
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for name, data in (("manifest.toml", manifest), ("core.rbf", base), ("persona.rbf", persona)):
                (root / name).write_bytes(data)
            original = read_package(root)
            (root / "persona.rbf").write_bytes(persona + b"changed")
            with self.assertRaises(PackageError):
                read_package(root)
            (root / "persona.rbf").write_bytes(persona)
            (root / "rom-map.json").write_bytes(b"map")
            with self.assertRaises(PackageError):
                read_package(root)
            (root / "rom-map.json").unlink()
            (root / "persona.rbf").unlink()
            with self.assertRaises(PackageError):
                read_package(root)
            outside = root.parent / (root.name + ".persona")
            try:
                outside.write_bytes(persona)
                (root / "persona.rbf").symlink_to(outside)
                with self.assertRaises(PackageError):
                    read_package(root)
            finally:
                outside.unlink(missing_ok=True)
            fields = tomllib.loads(manifest.decode())
            fields["static_expansion"]["root_qdb_sha256"] = "a" * 64
            self.assertNotEqual(original.package_id,
                package_identity(encode_manifest(fields), base, persona=persona))

    def test_archive_persona_header_order_padding_and_extra_data_fail(self):
        manifest = (FIXTURES / "manifests/valid-basic.toml").read_bytes()
        base = (FIXTURES / "payloads/base.rbf").read_bytes()
        persona = (FIXTURES / "payloads/persona.rbf").read_bytes()
        raw = _archive_bytes(manifest, base, persona=persona)
        start = 512 + ((len(manifest) + 511) // 512) * 512 + 512 + ((len(base) + 511) // 512) * 512
        for fault in ("missing", "wrong-name", "padding", "trailing", "symlink"):
            changed = bytearray(raw)
            if fault == "missing": changed = changed[:start] + bytes(1024)
            if fault == "wrong-name": changed[start:start+11] = b"rom-map.txt"
            if fault == "padding": changed[start+512+len(persona)] = 1
            if fault == "trailing": changed += bytes(512)
            if fault == "symlink": changed[start+156] = ord("2")
            if fault in ("wrong-name", "symlink"):
                header = changed[start:start+512]
                _rewrite_checksum(header)
                changed[start:start+512] = header
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as temp:
                archive = Path(temp) / "package.fcore"
                archive.write_bytes(changed)
                with self.assertRaises(PackageError):
                    read_package(archive)

    def test_directory_fifo_is_refused_without_blocking(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "manifest.toml").write_bytes((FIXTURES / "manifests/valid-basic.toml").read_bytes())
            (root / "core.rbf").write_bytes((FIXTURES / "payloads/base.rbf").read_bytes())
            os.mkfifo(root / "persona.rbf")
            program = "import sys; from scripts.core_package import read_package,PackageError; from pathlib import Path\ntry: read_package(Path(sys.argv[1]))\nexcept PackageError: sys.exit(0)\nsys.exit(1)"
            subprocess.run([sys.executable, "-c", program, str(root)], cwd=ROOT, check=True, timeout=3)
