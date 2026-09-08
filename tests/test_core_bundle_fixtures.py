import hashlib
import json
from pathlib import Path, PurePosixPath
import struct
import subprocess
import sys
import tomllib
import unittest

from jsonschema import Draft202012Validator, FormatChecker


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata" / "core-bundle-v2"
SCHEMA_PATH = ROOT / "schema" / "core-bundle-v2.json"
MANIFEST_MINIMUM = 1
MANIFEST_MAXIMUM = 65_536
PAYLOAD_MINIMUM = 1
PAYLOAD_MAXIMUM = 33_554_432


def package_id(manifest: bytes, payload: bytes) -> str:
    digest = hashlib.sha256(b"FES-CORE-PACKAGE-2\n")
    for data in (manifest, payload):
        digest.update(struct.pack("<Q", len(data)))
        digest.update(data)
    return digest.hexdigest()


def utf8_bound_errors(instance, schema, path="manifest"):
    errors = []
    maximum = schema.get("x-fes-maxUtf8Bytes")
    if maximum is not None and isinstance(instance, str):
        actual = len(instance.encode("utf-8"))
        if actual > maximum:
            errors.append(f"{path} is {actual} UTF-8 bytes, maximum is {maximum}")

    if isinstance(instance, dict):
        properties = schema.get("properties", {})
        for key, value in instance.items():
            child_schema = properties.get(key)
            if child_schema is not None:
                errors.extend(utf8_bound_errors(value, child_schema, f"{path}.{key}"))
    elif isinstance(instance, list) and "items" in schema:
        for index, value in enumerate(instance):
            errors.extend(
                utf8_bound_errors(value, schema["items"], f"{path}[{index}]")
            )
    return errors


def fixture_errors(case, schema):
    errors = []
    manifest_path = FIXTURES / case["manifest"]
    payload_path = FIXTURES / case["payload"]
    manifest = manifest_path.read_bytes()
    payload = payload_path.read_bytes()

    if not MANIFEST_MINIMUM <= len(manifest) <= MANIFEST_MAXIMUM:
        errors.append("manifest byte length is outside the admission bounds")
    if not PAYLOAD_MINIMUM <= len(payload) <= PAYLOAD_MAXIMUM:
        errors.append("payload byte length is outside the admission bounds")

    try:
        decoded = manifest.decode("utf-8")
    except UnicodeDecodeError as error:
        errors.append(f"manifest is not UTF-8: {error}")
        return errors, manifest, payload

    try:
        parsed = tomllib.loads(decoded)
    except tomllib.TOMLDecodeError as error:
        errors.append(f"manifest is not valid TOML: {error}")
        return errors, manifest, payload

    validator = Draft202012Validator(schema, format_checker=FormatChecker())
    errors.extend(
        error.message
        for error in sorted(validator.iter_errors(parsed), key=lambda item: list(item.path))
    )
    errors.extend(utf8_bound_errors(parsed, schema))

    integer_fields = [("format", parsed.get("format"))]
    abi = parsed.get("abi")
    if isinstance(abi, dict):
        integer_fields.extend(
            [("abi.major", abi.get("major")), ("abi.minor", abi.get("minor"))]
        )

    interfaces = parsed.get("interfaces")
    if isinstance(interfaces, list):
        for index, entry in enumerate(interfaces):
            if isinstance(entry, dict):
                integer_fields.extend(
                    [
                        (f"interfaces[{index}].major", entry.get("major")),
                        (f"interfaces[{index}].minor", entry.get("minor")),
                    ]
                )

    payload_description = parsed.get("payload")
    if isinstance(payload_description, dict):
        integer_fields.append(("payload.size", payload_description.get("size")))
    for field, value in integer_fields:
        if value is not None and type(value) is not int:
            errors.append(f"{field} must be a TOML integer")

    if isinstance(interfaces, list):
        interface_ids = [
            entry.get("id") for entry in interfaces if isinstance(entry, dict)
        ]
        if len(interface_ids) != len(set(interface_ids)):
            errors.append("interface IDs are duplicated")

    if isinstance(payload_description, dict):
        declared_size = payload_description.get("size")
        if type(declared_size) is int and declared_size != len(payload):
            errors.append("payload size does not match core.rbf")
        declared_digest = payload_description.get("sha256")
        if isinstance(declared_digest, str):
            actual_digest = hashlib.sha256(payload).hexdigest()
            if declared_digest != actual_digest:
                errors.append("payload sha256 does not match core.rbf")

    return errors, manifest, payload


class PackageIdentityTest(unittest.TestCase):
    def test_metadata_changes_identity(self):
        self.assertNotEqual(
            package_id(b"name='Pong'\n", b"same"),
            package_id(b"name='Other'\n", b"same"),
        )


class CoreBundleFixtureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
        Draft202012Validator.check_schema(cls.schema)
        cls.cases = json.loads(
            (FIXTURES / "cases.json").read_text(encoding="utf-8")
        )

    def test_fixture_set_covers_the_contract(self):
        names = {case["name"] for case in self.cases}
        required = {
            "valid-basic",
            "valid-semver-prerelease-build",
            "valid-unknown-abi",
            "valid-multibyte-bounds",
            "valid-literal-strings",
            "valid-dotted-keys",
            "valid-inline-tables",
            "invalid-missing-field",
            "invalid-wrong-type",
            "invalid-unknown-field",
            "invalid-duplicate-key",
            "invalid-duplicate-interface",
            "invalid-utf8",
            "invalid-empty-name",
            "invalid-empty-payload",
            "invalid-oversized-name-utf8",
            "invalid-oversized-description-utf8",
            "invalid-boolean-size",
            "invalid-boolean-version",
            "invalid-float-size",
            "invalid-float-version",
            "invalid-payload-file",
            "invalid-payload-digest",
            "invalid-payload-size",
            "invalid-control-character",
            "invalid-malformed-repository",
            "invalid-malformed-repository-uri",
            "invalid-malformed-revision",
            "invalid-oversized-manifest",
        }
        self.assertTrue(required <= names, sorted(required - names))
        self.assertEqual(len(self.cases), len(names), "fixture names must be unique")

        for case in self.cases:
            with self.subTest(case=case["name"]):
                expected_keys = {"name", "manifest", "payload", "valid"}
                expected_keys.add("package_id" if case["valid"] else "reason")
                self.assertEqual(expected_keys, set(case))
                for key in ("manifest", "payload"):
                    relative_path = PurePosixPath(case[key])
                    self.assertFalse(relative_path.is_absolute())
                    self.assertNotIn("..", relative_path.parts)
                    self.assertTrue((FIXTURES / relative_path).is_file())
                if case["valid"]:
                    self.assertRegex(case["package_id"], r"^[0-9a-f]{64}$")
                else:
                    self.assertTrue(case["reason"])

    def test_fixture_results_match_index(self):
        expected_failures = {
            "invalid-missing-field": "required property",
            "invalid-wrong-type": "not of type 'string'",
            "invalid-unknown-field": "Additional properties",
            "invalid-duplicate-key": "Cannot overwrite a value",
            "invalid-duplicate-interface": "interface IDs are duplicated",
            "invalid-utf8": "manifest is not UTF-8",
            "invalid-empty-name": "should be non-empty",
            "invalid-oversized-name-utf8": "129 UTF-8 bytes",
            "invalid-oversized-description-utf8": "2049 UTF-8 bytes",
            "invalid-boolean-size": "payload.size must be a TOML integer",
            "invalid-boolean-version": "abi.major must be a TOML integer",
            "invalid-float-size": "payload.size must be a TOML integer",
            "invalid-float-version": "abi.major must be a TOML integer",
            "invalid-payload-file": "'core.rbf' was expected",
            "invalid-payload-digest": "payload sha256 does not match core.rbf",
            "invalid-payload-size": "payload size does not match core.rbf",
            "invalid-control-character": "does not match",
            "invalid-malformed-repository": "does not match",
            "invalid-malformed-repository-uri": "is not a 'uri'",
            "invalid-malformed-revision": "does not match",
            "invalid-oversized-manifest": "manifest byte length",
            "invalid-empty-payload": "payload byte length",
        }
        for case in self.cases:
            with self.subTest(case=case["name"]):
                required_keys = {"name", "manifest", "payload", "valid"}
                self.assertTrue(required_keys <= case.keys())
                errors, manifest, payload = fixture_errors(case, self.schema)
                if case["valid"]:
                    self.assertIn("package_id", case)
                    self.assertNotIn("reason", case)
                    self.assertEqual([], errors)
                    self.assertEqual(case["package_id"], package_id(manifest, payload))
                else:
                    self.assertIn("reason", case)
                    self.assertNotIn("package_id", case)
                    self.assertTrue(errors, case["name"])
                    self.assertIn(
                        expected_failures[case["name"]],
                        "\n".join(errors),
                    )

    def test_uri_format_checker_is_registered(self):
        checker = FormatChecker()
        self.assertIn("uri", checker.checkers)
        self.assertFalse(checker.conforms("https://[", "uri"))

    def test_primary_fixture_has_approved_pong_contract(self):
        case = next(case for case in self.cases if case["name"] == "valid-basic")
        manifest = tomllib.loads(
            (FIXTURES / case["manifest"]).read_text(encoding="utf-8")
        )
        self.assertEqual("fes.pong", manifest["core"]["id"])
        self.assertEqual("FES Pong", manifest["core"]["name"])
        self.assertIn("test-only", manifest["core"]["description"])
        self.assertEqual("0.1.0", manifest["core"]["version"])
        self.assertEqual("fes-gp-v1", manifest["target"]["programming_profile"])
        self.assertEqual(
            [
                {"id": "fes.gamepad", "major": 1, "minor": 0, "required": True},
                {
                    "id": "fes.video.fixed-720p60",
                    "major": 1,
                    "minor": 0,
                    "required": True,
                },
            ],
            manifest["interfaces"],
        )
        self.assertEqual(b"fes-fixture\n", (FIXTURES / case["payload"]).read_bytes())

    def test_generator_reports_no_drift(self):
        result = subprocess.run(
            [sys.executable, ROOT / "scripts" / "core_bundle_fixtures.py", "--check"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
