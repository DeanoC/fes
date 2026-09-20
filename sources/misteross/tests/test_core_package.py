import json
import subprocess
import sys
import tarfile
import tempfile
import tomllib
import unittest
from pathlib import Path

from scripts.core_package import (
    MAX_MANIFEST_SIZE,
    MAX_PAYLOAD_SIZE,
    PackageError,
    encode_manifest,
    package_identity,
    read_package,
)


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests" / "fixtures" / "core-bundle-v2"
REPOSITORY_URI_CASES = (
    ("https://example.invalid/repo@rev", True),
    ("https://example.invalid/a?x=@ok#f/@", True),
    ("https://[::1]/repo", True),
    ("https://[v1.alpha]/repo", True),
    ("https://:80/path", True),
    ("https://example.invalid:/path", True),
    ("https://", False),
    ("https:///path", False),
    ("https://?q", False),
    ("https://#fragment", False),
    ("https://example.invalid/path[bad]", False),
    ("https://example.invalid/a#b#c", False),
    ("https://example.invalid:port/path", False),
    ("https://example.invalid/%zz", False),
    ("https://[", False),
    ("https://user@example.invalid/repo", False),
)


def _header(name: str, size: int, type_: bytes = tarfile.REGTYPE, linkname: str = "") -> bytes:
    info = tarfile.TarInfo(name)
    info.mode = 0o644
    info.uid = 0
    info.gid = 0
    info.size = size
    info.mtime = 0
    info.type = type_
    info.linkname = linkname
    info.uname = ""
    info.gname = ""
    return info.tobuf(format=tarfile.USTAR_FORMAT, encoding="ascii", errors="strict")


def _member(name: str, data: bytes, type_: bytes = tarfile.REGTYPE, linkname: str = "") -> bytes:
    padding = b"\0" * ((-len(data)) % 512)
    return _header(name, len(data), type_, linkname) + data + padding


def _archive(manifest: bytes, payload: bytes) -> bytes:
    return _member("manifest.toml", manifest) + _member("core.rbf", payload) + b"\0" * 1024


def _rewrite_checksum(header: bytearray) -> None:
    header[148:156] = b"        "
    checksum = sum(header)
    header[148:156] = f"{checksum:06o}\0 ".encode("ascii")


class CorePackageTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.work = Path(self.tempdir.name)
        self.cases = json.loads((FIXTURES / "cases.json").read_text(encoding="utf-8"))
        self.basic_manifest = (FIXTURES / "manifests" / "valid-basic.toml").read_bytes()
        self.payload = (FIXTURES / "payloads" / "fes-fixture.rbf").read_bytes()

    def tearDown(self):
        self.tempdir.cleanup()

    def _directory(self, manifest: bytes | None = None, payload: bytes | None = None) -> Path:
        package = self.work / "package"
        package.mkdir()
        (package / "manifest.toml").write_bytes(self.basic_manifest if manifest is None else manifest)
        (package / "core.rbf").write_bytes(self.payload if payload is None else payload)
        return package

    def _archive_path(self, data: bytes) -> Path:
        path = self.work / "package.fcore"
        path.write_bytes(data)
        return path

    def test_reads_every_shared_manifest_fixture(self):
        for case in self.cases:
            with self.subTest(case=case["name"]):
                manifest = (FIXTURES / case["manifest"]).read_bytes()
                payload = (FIXTURES / case["payload"]).read_bytes()
                directory = self.work / case["name"]
                directory.mkdir()
                (directory / "manifest.toml").write_bytes(manifest)
                (directory / "core.rbf").write_bytes(payload)
                if case["valid"]:
                    package = read_package(directory)
                    self.assertEqual(package.manifest_bytes, manifest)
                    self.assertEqual(package.payload_bytes, payload)
                    self.assertEqual(package.fields, tomllib.loads(manifest.decode("utf-8")))
                    self.assertEqual(package.package_id, case["package_id"])
                else:
                    with self.assertRaises(PackageError):
                        read_package(directory)

    def test_basic_fields_and_identity_have_independent_literal_expectations(self):
        package = read_package(self._directory())

        self.assertEqual(package.fields["format"], 2)
        self.assertEqual(package.fields["core"]["id"], "fes.pong")
        self.assertEqual(package.fields["abi"], {"id": "fes.simple-game", "major": 1, "minor": 0})
        self.assertEqual(package.package_id, "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0")

        changed = self.basic_manifest.replace(b'FES Pong', b'FES pong')
        self.assertNotEqual(package_identity(changed, self.payload), package.package_id)

    def test_repository_matches_shared_rfc3986_cases(self):
        original = b"https://example.invalid/fes-pong"
        for index, (repository, valid) in enumerate(REPOSITORY_URI_CASES):
            with self.subTest(repository=repository):
                manifest = self.basic_manifest.replace(original, repository.encode("ascii"))
                package = self.work / f"repository-{index}"
                package.mkdir()
                (package / "manifest.toml").write_bytes(manifest)
                (package / "core.rbf").write_bytes(self.payload)
                if valid:
                    self.assertEqual(read_package(package).fields["build"]["repository"], repository)
                else:
                    with self.assertRaises(PackageError):
                        read_package(package)

    def test_encode_manifest_is_deterministic_and_round_trips(self):
        fields = tomllib.loads(self.basic_manifest.decode("utf-8"))
        first = encode_manifest(fields)
        second = encode_manifest(json.loads(json.dumps(fields)))
        self.assertEqual(first, second)

        package = read_package(self._directory(first, self.payload))
        self.assertEqual(package.fields, fields)

    def test_encode_manifest_preserves_an_empty_interface_array(self):
        fields = tomllib.loads(self.basic_manifest.decode("utf-8"))
        fields["interfaces"] = []
        encoded = encode_manifest(fields)

        package = read_package(self._directory(encoded, self.payload))
        self.assertEqual(package.fields["interfaces"], [])

    def test_reads_only_the_canonical_ustar_representation(self):
        data = _archive(self.basic_manifest, self.payload)
        package = read_package(self._archive_path(data))

        self.assertEqual(package.manifest_bytes, self.basic_manifest)
        self.assertEqual(package.payload_bytes, self.payload)
        self.assertEqual(package.package_id, "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0")

    def test_rejects_noncanonical_archive_structure_and_headers(self):
        valid_manifest = _member("manifest.toml", self.basic_manifest)
        valid_payload = _member("core.rbf", self.payload)
        terminator = b"\0" * 1024

        bad_checksum = bytearray(_header("manifest.toml", len(self.basic_manifest)))
        bad_checksum[0] ^= 1
        bad_padding = bytearray(valid_manifest + valid_payload + terminator)
        bad_padding[512 + len(self.basic_manifest)] = 1
        overflow = bytearray(_header("manifest.toml", len(self.basic_manifest)))
        overflow[124:136] = b"\x80" + b"\0" * 11
        _rewrite_checksum(overflow)
        cases = {
            "third entry": valid_manifest + valid_payload + _member("third", b"x") + terminator,
            "reversed order": valid_payload + valid_manifest + terminator,
            "symlink": _member("manifest.toml", b"", tarfile.SYMTYPE, "elsewhere") + valid_payload + terminator,
            "hardlink": _member("manifest.toml", b"", tarfile.LNKTYPE, "elsewhere") + valid_payload + terminator,
            "path traversal": _member("../manifest.toml", self.basic_manifest) + valid_payload + terminator,
            "malformed checksum": bytes(bad_checksum) + self.basic_manifest + b"\0" * ((-len(self.basic_manifest)) % 512) + valid_payload + terminator,
            "truncated body": (valid_manifest + valid_payload + terminator)[:-1025],
            "nonzero padding": bytes(bad_padding),
            "numeric overflow": bytes(overflow) + self.basic_manifest + b"\0" * ((-len(self.basic_manifest)) % 512) + valid_payload + terminator,
            "extra terminator": valid_manifest + valid_payload + terminator + b"\0" * 512,
        }
        for label, data in cases.items():
            with self.subTest(case=label):
                with self.assertRaises(PackageError):
                    read_package(self._archive_path(data))

    def test_rejects_archive_declared_sizes_outside_bounds_before_allocation(self):
        too_large_manifest = _header("manifest.toml", MAX_MANIFEST_SIZE + 1) + b"\0" * 1024
        too_large_payload = _member("manifest.toml", self.basic_manifest) + _header(
            "core.rbf", MAX_PAYLOAD_SIZE + 1
        ) + b"\0" * 1024
        for data in (too_large_manifest, too_large_payload):
            with self.subTest(length=len(data)):
                with self.assertRaises(PackageError):
                    read_package(self._archive_path(data))

    def test_directory_rejects_extra_symlink_and_nonregular_entries(self):
        for label in ("extra", "symlink", "directory"):
            with self.subTest(case=label):
                package = self._directory()
                if label == "extra":
                    (package / "extra").write_bytes(b"x")
                elif label == "symlink":
                    (package / "core.rbf").unlink()
                    (package / "core.rbf").symlink_to(self.work / "payload.rbf")
                else:
                    (package / "core.rbf").unlink()
                    (package / "core.rbf").mkdir()
                with self.assertRaises(PackageError):
                    read_package(package)
                if package.exists():
                    for child in package.iterdir():
                        if child.is_dir() and not child.is_symlink():
                            child.rmdir()
                        else:
                            child.unlink()
                    package.rmdir()

    def test_inspect_cli_prints_stable_json(self):
        package = self._directory()
        result = subprocess.run(
            [sys.executable, str(ROOT / "scripts" / "core_package.py"), "inspect", str(package)],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(output["package_id"], "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0")
        self.assertEqual(output["manifest"]["core"]["id"], "fes.pong")


if __name__ == "__main__":
    unittest.main()
