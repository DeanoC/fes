#!/usr/bin/env python3
"""Generate deterministic, synthetic format-2 core-bundle fixtures."""

import argparse
import hashlib
import json
from pathlib import Path
import struct
import sys


ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = ROOT / "testdata" / "core-bundle-v2"
PAYLOAD = b"fes-fixture\n"
PAYLOAD_SHA256 = hashlib.sha256(PAYLOAD).hexdigest()


def package_id(manifest: bytes, payload: bytes) -> str:
    digest = hashlib.sha256(b"FES-CORE-PACKAGE-2\n")
    for data in (manifest, payload):
        digest.update(struct.pack("<Q", len(data)))
        digest.update(data)
    return digest.hexdigest()


def canonical_manifest() -> bytes:
    return f'''format = 2

[core]
id = "fes.pong"
name = "FES Pong"
description = "Synthetic test-only core bundle fixture; never deploy."
version = "0.1.0"

[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"

[payload]
file = "core.rbf"
size = 12
sha256 = "{PAYLOAD_SHA256}"

[abi]
id = "fes.simple-game"
major = 1
minor = 0

[[interfaces]]
id = "fes.gamepad"
major = 1
minor = 0
required = true

[[interfaces]]
id = "fes.video.fixed-720p60"
major = 1
minor = 0
required = true

[build]
id = "0123456789abcdef0123456789abcdef"
repository = "https://example.invalid/fes-pong"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic fixture generator 1.0 (test-only)"
'''.encode("utf-8")


def literal_string_manifest() -> bytes:
    return f'''format = 2

[core]
id = 'fes.pong'
name = 'FES Pong'
description = 'Synthetic test-only core bundle fixture; never deploy.'
version = '0.1.0'

[target]
platform = 'de10_nano'
device = '5CSEBA6U23I7'
programming_profile = 'fes-gp-v1'

[payload]
file = 'core.rbf'
size = 12
sha256 = '{PAYLOAD_SHA256}'

[abi]
id = 'fes.simple-game'
major = 1
minor = 0

[[interfaces]]
id = 'fes.gamepad'
major = 1
minor = 0
required = true

[[interfaces]]
id = 'fes.video.fixed-720p60'
major = 1
minor = 0
required = true

[build]
id = '0123456789abcdef0123456789abcdef'
repository = 'https://example.invalid/fes-pong'
revision = '1111111111111111111111111111111111111111'
recipe_sha256 = '2222222222222222222222222222222222222222222222222222222222222222'
toolchain = 'synthetic fixture generator 1.0 (test-only)'
'''.encode("utf-8")


def dotted_key_manifest() -> bytes:
    return f'''format = 2
core.id = "fes.pong"
core.name = "FES Pong"
core.description = "Synthetic test-only core bundle fixture; never deploy."
core.version = "0.1.0"
target.platform = "de10_nano"
target.device = "5CSEBA6U23I7"
target.programming_profile = "fes-gp-v1"
payload.file = "core.rbf"
payload.size = 12
payload.sha256 = "{PAYLOAD_SHA256}"
abi.id = "fes.simple-game"
abi.major = 1
abi.minor = 0
interfaces = [
  {{ id = "fes.gamepad", major = 1, minor = 0, required = true }},
  {{ id = "fes.video.fixed-720p60", major = 1, minor = 0, required = true }},
]
build.id = "0123456789abcdef0123456789abcdef"
build.repository = "https://example.invalid/fes-pong"
build.revision = "1111111111111111111111111111111111111111"
build.recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
build.toolchain = "synthetic fixture generator 1.0 (test-only)"
'''.encode("utf-8")


def inline_table_manifest() -> bytes:
    return f'''format = 2
core = {{ id = "fes.pong", name = "FES Pong", description = "Synthetic test-only core bundle fixture; never deploy.", version = "0.1.0" }}
target = {{ platform = "de10_nano", device = "5CSEBA6U23I7", programming_profile = "fes-gp-v1" }}
payload = {{ file = "core.rbf", size = 12, sha256 = "{PAYLOAD_SHA256}" }}
abi = {{ id = "fes.simple-game", major = 1, minor = 0 }}
interfaces = [{{ id = "fes.gamepad", major = 1, minor = 0, required = true }}, {{ id = "fes.video.fixed-720p60", major = 1, minor = 0, required = true }}]
build = {{ id = "0123456789abcdef0123456789abcdef", repository = "https://example.invalid/fes-pong", revision = "1111111111111111111111111111111111111111", recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222", toolchain = "synthetic fixture generator 1.0 (test-only)" }}
'''.encode("utf-8")


def replace_once(manifest: bytes, old: bytes, new: bytes) -> bytes:
    if manifest.count(old) != 1:
        raise ValueError(f"expected exactly one occurrence of {old!r}")
    return manifest.replace(old, new, 1)


def generated_files():
    base = canonical_manifest()
    valid_manifests = {
        "valid-basic": base,
        "valid-semver-prerelease-build": replace_once(
            replace_once(base, b'version = "0.1.0"', b'version = "0.1.0-rc.1+fixture.5"'),
            b'revision = "1111111111111111111111111111111111111111"',
            b'revision = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"',
        ),
        "valid-unknown-abi": replace_once(
            base,
            b'id = "fes.simple-game"',
            b'id = "vendor.future-abi"',
        ),
        "valid-multibyte-bounds": replace_once(
            replace_once(
                base,
                b'name = "FES Pong"',
                ('name = "' + "€" * 42 + "é" + '"').encode("utf-8"),
            ),
            b'description = "Synthetic test-only core bundle fixture; never deploy."',
            ('description = "' + "€" * 682 + "é" + '"').encode("utf-8"),
        ),
        "valid-literal-strings": literal_string_manifest(),
        "valid-dotted-keys": dotted_key_manifest(),
        "valid-inline-tables": inline_table_manifest(),
    }

    invalid_manifests = {
        "invalid-missing-field": (
            replace_once(
                base,
                b'description = "Synthetic test-only core bundle fixture; never deploy."\n',
                b"",
            ),
            "required core.description is missing",
        ),
        "invalid-wrong-type": (
            replace_once(base, b'name = "FES Pong"', b"name = 7"),
            "core.name has the wrong TOML type",
        ),
        "invalid-unknown-field": (
            base + b"unknown = true\n",
            "unknown root fields are rejected",
        ),
        "invalid-duplicate-key": (
            replace_once(
                base,
                b'name = "FES Pong"\n',
                b'name = "FES Pong"\nname = "Other"\n',
            ),
            "duplicate TOML keys are rejected",
        ),
        "invalid-duplicate-interface": (
            replace_once(
                base,
                b'id = "fes.video.fixed-720p60"',
                b'id = "fes.gamepad"',
            ),
            "interface IDs must be unique",
        ),
        "invalid-utf8": (
            base + b"# invalid byte follows: \xff\n",
            "manifest bytes must be valid UTF-8",
        ),
        "invalid-empty-name": (
            replace_once(base, b'name = "FES Pong"', b'name = ""'),
            "core.name must not be empty",
        ),
        "invalid-oversized-name-utf8": (
            replace_once(
                base,
                b'name = "FES Pong"',
                ('name = "' + "€" * 43 + '"').encode("utf-8"),
            ),
            "core.name exceeds 128 UTF-8 bytes",
        ),
        "invalid-oversized-description-utf8": (
            replace_once(
                base,
                b'description = "Synthetic test-only core bundle fixture; never deploy."',
                ('description = "' + "€" * 683 + '"').encode("utf-8"),
            ),
            "core.description exceeds 2048 UTF-8 bytes",
        ),
        "invalid-boolean-size": (
            replace_once(base, b"size = 12", b"size = true"),
            "payload.size must be a TOML integer, not Boolean",
        ),
        "invalid-boolean-version": (
            replace_once(
                base,
                b'[abi]\nid = "fes.simple-game"\nmajor = 1',
                b'[abi]\nid = "fes.simple-game"\nmajor = true',
            ),
            "ABI major must be a TOML integer, not Boolean",
        ),
        "invalid-float-size": (
            replace_once(base, b"size = 12", b"size = 12.0"),
            "payload.size must be a TOML integer, not a float",
        ),
        "invalid-float-version": (
            replace_once(
                base,
                b'[abi]\nid = "fes.simple-game"\nmajor = 1',
                b'[abi]\nid = "fes.simple-game"\nmajor = 1.0',
            ),
            "ABI major must be a TOML integer, not a float",
        ),
        "invalid-payload-file": (
            replace_once(base, b'file = "core.rbf"', b'file = "other.rbf"'),
            "payload.file must be core.rbf",
        ),
        "invalid-payload-digest": (
            replace_once(
                base,
                f'sha256 = "{PAYLOAD_SHA256}"'.encode("ascii"),
                b'sha256 = "0000000000000000000000000000000000000000000000000000000000000000"',
            ),
            "payload digest does not match the fixture bytes",
        ),
        "invalid-payload-size": (
            replace_once(base, b"size = 12", b"size = 13"),
            "payload size does not match the fixture bytes",
        ),
        "invalid-control-character": (
            replace_once(base, b'name = "FES Pong"', b'name = "FES\\u0001Pong"'),
            "manifest strings exclude control characters",
        ),
        "invalid-malformed-repository": (
            replace_once(
                base,
                b'repository = "https://example.invalid/fes-pong"',
                b'repository = "https://user:pass@example.invalid/fes-pong"',
            ),
            "build.repository must be HTTPS without credentials",
        ),
        "invalid-malformed-repository-uri": (
            replace_once(
                base,
                b'repository = "https://example.invalid/fes-pong"',
                b'repository = "https://["',
            ),
            "build.repository must be a valid RFC 3986 URI",
        ),
        "invalid-malformed-revision": (
            replace_once(
                base,
                b'revision = "1111111111111111111111111111111111111111"',
                b'revision = "1111111"',
            ),
            "build.revision must be a full 40-hex commit",
        ),
        "invalid-oversized-manifest": (
            base + b"#" + b"x" * 65_536 + b"\n",
            "manifest exceeds 65,536 bytes",
        ),
    }

    files = {
        "payloads/fes-fixture.rbf": PAYLOAD,
        "payloads/empty.rbf": b"",
    }
    cases = []
    for name, manifest in valid_manifests.items():
        manifest_path = f"manifests/{name}.toml"
        files[manifest_path] = manifest
        cases.append(
            {
                "name": name,
                "manifest": manifest_path,
                "payload": "payloads/fes-fixture.rbf",
                "valid": True,
                "package_id": package_id(manifest, PAYLOAD),
            }
        )
    for name, (manifest, reason) in invalid_manifests.items():
        manifest_path = f"manifests/{name}.toml"
        files[manifest_path] = manifest
        cases.append(
            {
                "name": name,
                "manifest": manifest_path,
                "payload": "payloads/fes-fixture.rbf",
                "valid": False,
                "reason": reason,
            }
        )

    empty_payload_manifest = base
    empty_payload_name = "invalid-empty-payload"
    empty_payload_path = f"manifests/{empty_payload_name}.toml"
    files[empty_payload_path] = empty_payload_manifest
    cases.append(
        {
            "name": empty_payload_name,
            "manifest": empty_payload_path,
            "payload": "payloads/empty.rbf",
            "valid": False,
            "reason": "payload must contain at least one byte",
        }
    )

    files["cases.json"] = (json.dumps(cases, indent=2) + "\n").encode("utf-8")
    return files


def check(files):
    drift = []
    for relative_path, expected in files.items():
        path = FIXTURE_ROOT / relative_path
        if not path.exists():
            drift.append(f"missing: {relative_path}")
        elif path.read_bytes() != expected:
            drift.append(f"changed: {relative_path}")
    if FIXTURE_ROOT.exists():
        actual_paths = {
            path.relative_to(FIXTURE_ROOT).as_posix()
            for path in FIXTURE_ROOT.rglob("*")
            if path.is_file()
        }
        for relative_path in sorted(actual_paths - files.keys()):
            drift.append(f"unexpected: {relative_path}")
    if drift:
        print("fixture drift detected:", file=sys.stderr)
        for item in drift:
            print(f"  {item}", file=sys.stderr)
        return 1
    print(f"checked {len(files)} generated fixture files")
    return 0


def write(files):
    for relative_path, content in files.items():
        path = FIXTURE_ROOT / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
    print(f"wrote {len(files)} generated fixture files")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check", action="store_true", help="fail if checked-in fixtures differ"
    )
    args = parser.parse_args()
    files = generated_files()
    return check(files) if args.check else write(files)


if __name__ == "__main__":
    raise SystemExit(main())
