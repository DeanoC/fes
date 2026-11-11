"""DE25 static export coverage uses the board-owned package codec."""
import json
import os
import subprocess
import sys
import tomllib
import unittest
from pathlib import Path

from boards.de25nano.package.core_package import encode_manifest, read_package
from boards.de25nano.package.export_core_package import (
    PackageExportError, build_identity, encode_build_record, export_package,
)
from tests import test_export_core_package as shared

ROOT = shared.ROOT


class DE25StaticExportTests(unittest.TestCase):
    setUp = shared.ExportCorePackageTests.setUp
    tearDown = shared.ExportCorePackageTests.tearDown
    functional_fields = shared.ExportCorePackageTests.functional_fields

    def static_export_inputs(self):
        fixture = ROOT / "tests/fixtures/core-bundle-v5"
        fields = tomllib.loads((fixture / "manifests/valid-basic.toml").read_text())
        self.record_fields = self.functional_fields()
        self.record_fields["toolchain"] = "quartus"
        self.record_fields["tools"] = {"quartus": "26.3", "qsys": "26.3", "ipgenerate": "26.3", "pfg": "26.3"}
        self.record_fields["parameters"] = dict(device=fields["target"]["device"],
            programming_profile=fields["target"]["programming_profile"], hps_initialization="HPS FIRST",
            static_control_build_id=fields["static_expansion"]["control_build_id"],
            static_default_persona_id=fields["static_expansion"]["hardware_id"])
        fields["build"].update(revision=self.revision, recipe_sha256=self.record_fields["recipe_sha256"])
        persona = self.build / "persona.rbf"
        persona.write_bytes((fixture / "payloads/persona.rbf").read_bytes())
        return fields, persona


    def static_manifest(self, fields):
        record = encode_build_record(self.record_fields)
        (self.build / "build-inputs.json").write_bytes(record)
        fields["build"]["id"] = build_identity(record)
        return encode_manifest(fields)


    def test_static_export_is_separate_sealed_reusable_and_deterministic(self):
        fields, persona = self.static_export_inputs()
        manifest = self.static_manifest(fields)
        directory = export_package(manifest, self.rbf, self.store, persona=persona)
        package = read_package(directory)
        self.assertEqual(persona.read_bytes(), package.persona_bytes)
        self.assertIsNone(package.rom_map_bytes)
        self.assertEqual({"manifest.toml", "core.rbf", "persona.rbf"}, {p.name for p in directory.iterdir()})
        self.assertEqual(package, read_package(directory.with_suffix(".fcore")))
        self.assertEqual(0, (directory / "persona.rbf").stat().st_mode & 0o222)
        self.assertEqual(directory, export_package(manifest, self.rbf, self.store, persona=persona))
        second = export_package(manifest, self.rbf, self.work / "another-store", persona=persona)
        self.assertEqual(directory.with_suffix(".fcore").read_bytes(), second.with_suffix(".fcore").read_bytes())
        (directory / "persona.rbf").chmod(0o644)
        with self.assertRaises(PackageExportError):
            export_package(manifest, self.rbf, self.store, persona=persona)


    def test_static_export_refuses_wrong_record_inputs(self):
        fields, persona = self.static_export_inputs()
        parameters = dict(self.record_fields["parameters"])
        for key, bad in [("device", "different"), ("programming_profile", "de25-linux-computer-v1"),
                         ("hps_initialization", "FPGA FIRST"), ("static_control_build_id", "a" * 32),
                         ("static_default_persona_id", True)]:
            self.record_fields["parameters"] = dict(parameters, **{key: bad})
            manifest = self.static_manifest(fields)
            with self.subTest(key=key), self.assertRaisesRegex(PackageExportError, "source-closed Quartus"):
                export_package(manifest, self.rbf, self.store, persona=persona)
            self.assertFalse(self.store.exists())
        self.record_fields["parameters"] = parameters
        tools = dict(self.record_fields["tools"])
        self.record_fields["tools"] = {"yosys": "synthetic non-Quartus tool"}
        with self.assertRaisesRegex(PackageExportError, "source-closed Quartus"):
            export_package(self.static_manifest(fields), self.rbf, self.store, persona=persona)
        self.record_fields["tools"] = tools
        del self.record_fields["toolchain"]
        with self.assertRaisesRegex(PackageExportError, "source-closed Quartus"):
            export_package(self.static_manifest(fields), self.rbf, self.store, persona=persona)
        self.assertFalse(self.store.exists())


    def test_static_export_refuses_missing_changed_or_unrelated_persona(self):
        fields, persona = self.static_export_inputs()
        manifest = self.static_manifest(fields)
        with self.assertRaises(PackageExportError):
            export_package(manifest, self.rbf, self.store)
        persona.write_bytes(b"different persona")
        with self.assertRaises(PackageExportError):
            export_package(manifest, self.rbf, self.store, persona=persona)
        outside = self.work / "persona.rbf"
        outside.write_bytes(persona.read_bytes())
        with self.assertRaises(PackageExportError):
            export_package(manifest, self.rbf, self.store, persona=outside)
        with self.assertRaises(PackageExportError):
            export_package(self.manifest, self.rbf, self.store, persona=persona)
        self.assertFalse(self.store.exists())


    def test_static_export_refuses_fifo_without_blocking(self):
        fields, persona = self.static_export_inputs()
        manifest = self.static_manifest(fields)
        manifest_path = self.build / "manifest.toml"
        manifest_path.write_bytes(manifest)
        persona.unlink()
        os.mkfifo(persona)
        program = "import sys; from pathlib import Path; from boards.de25nano.package.export_core_package import export_package,PackageExportError\ntry: export_package(Path(sys.argv[1]).read_bytes(),Path(sys.argv[2]),Path(sys.argv[3]),persona=Path(sys.argv[4]))\nexcept PackageExportError: sys.exit(0)\nsys.exit(1)"
        subprocess.run([sys.executable, "-c", program, str(manifest_path), str(self.rbf), str(self.store), str(persona)], cwd=ROOT, check=True, timeout=3)
        self.assertFalse(self.store.exists())


    def test_static_export_cli_and_corrupt_readonly_reuse(self):
        fields, persona = self.static_export_inputs()
        manifest_path = self.build / "manifest.toml"
        manifest_path.write_bytes(self.static_manifest(fields))
        completed = subprocess.run([sys.executable, str(ROOT / "boards/de25nano/package/export_core_package.py"),
            "--manifest", str(manifest_path), "--rbf", str(self.rbf), "--persona", str(persona),
            "--output", str(self.store)], cwd=ROOT, capture_output=True, text=True, check=True, timeout=20)
        directory = Path(completed.stdout.strip())
        self.assertEqual(persona.read_bytes(), read_package(directory).persona_bytes)
        installed = directory / "persona.rbf"
        installed.chmod(0o644)
        installed.write_bytes(b"damaged readonly persona")
        installed.chmod(0o444)
        archive = directory.with_suffix(".fcore").read_bytes()
        with self.assertRaises(PackageExportError):
            export_package(manifest_path.read_bytes(), self.rbf, self.store, persona=persona)
        self.assertEqual(b"damaged readonly persona", installed.read_bytes())
        self.assertEqual(archive, directory.with_suffix(".fcore").read_bytes())


