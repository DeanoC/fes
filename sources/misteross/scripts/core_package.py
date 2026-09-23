#!/usr/bin/env python3
"""Validate and inspect FES core packages."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import struct
import sys
import tarfile
import tomllib
from dataclasses import dataclass
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.rfc3986_validator import validate_rfc3986


MAX_MANIFEST_SIZE = 65_536
MAX_PAYLOAD_SIZE = 33_554_432
MAX_ARCHIVE_SIZE = 33 * 1024 * 1024
MAX_ARCHIVE_SIZE_V3 = 65 * 1024 * 1024
MAX_ROM_MAP_SIZE = 33_554_432
PACKAGE_DOMAIN = b"FES-CORE-PACKAGE-2\n"
ID_RE = re.compile(r"[a-z][a-z0-9_.-]{0,95}\Z")
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
HEX40_RE = re.compile(r"[0-9A-Fa-f]{40}\Z")
HEX64_RE = re.compile(r"[0-9a-f]{64}\Z")
SEMVER_RE = re.compile(
    r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?\Z"
)


class PackageError(ValueError):
    """Raised when a core package fails closed validation."""


@dataclass(frozen=True)
class CorePackage:
    manifest_bytes: bytes
    fields: dict
    payload_bytes: bytes
    package_id: str
    rom_map_bytes: bytes | None = None


def package_identity(manifest: bytes, payload: bytes, rom_map: bytes | None = None) -> str:
    """Hash exact members using the format-2 or format-3 domain."""

    if not isinstance(manifest, bytes) or not isinstance(payload, bytes):
        raise TypeError("manifest and payload must be bytes")
    digest = hashlib.sha256()
    if rom_map is not None and not isinstance(rom_map, bytes):
        raise TypeError("rom map must be bytes")
    digest.update(PACKAGE_DOMAIN if rom_map is None else b"FES-CORE-PACKAGE-3\n")
    digest.update(struct.pack("<Q", len(manifest)))
    digest.update(manifest)
    digest.update(struct.pack("<Q", len(payload)))
    digest.update(payload)
    if rom_map is not None:
        digest.update(struct.pack("<Q", len(rom_map)))
        digest.update(rom_map)
    return digest.hexdigest()


def _exact_dict(value: object, required: set[str], optional: set[str], field: str) -> dict:
    if not isinstance(value, dict):
        raise PackageError(f"{field} must be a TOML table")
    keys = set(value)
    missing = required - keys
    unknown = keys - required - optional
    if missing:
        raise PackageError(f"{field} is missing required fields: {', '.join(sorted(missing))}")
    if unknown:
        raise PackageError(f"{field} has unknown fields: {', '.join(sorted(unknown))}")
    return value


def _string(value: object, field: str, *, nonempty: bool = False, max_bytes: int | None = None) -> str:
    if not isinstance(value, str):
        raise PackageError(f"{field} must be a TOML string")
    if any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value):
        raise PackageError(f"{field} contains a control character")
    if nonempty and not value:
        raise PackageError(f"{field} must not be empty")
    if max_bytes is not None and len(value.encode("utf-8")) > max_bytes:
        raise PackageError(f"{field} exceeds {max_bytes} UTF-8 bytes")
    return value


def _identifier(value: object, field: str) -> str:
    value = _string(value, field)
    if ID_RE.fullmatch(value) is None:
        raise PackageError(f"{field} is not a valid identifier")
    return value


def _integer(value: object, field: str, minimum: int, maximum: int) -> int:
    if type(value) is not int or not minimum <= value <= maximum:
        raise PackageError(f"{field} must be a TOML integer from {minimum} through {maximum}")
    return value


def _versioned_contract(value: object, field: str) -> dict:
    table = _exact_dict(value, {"id", "major", "minor"}, set(), field)
    _identifier(table["id"], f"{field}.id")
    _integer(table["major"], f"{field}.major", 1, 65_535)
    _integer(table["minor"], f"{field}.minor", 0, 65_535)
    return table


def _repository(value: object) -> str:
    value = _string(value, "build.repository", nonempty=True)
    if validate_rfc3986(value, rule="URI") is None:
        raise PackageError("build.repository must be a valid RFC 3986 URI")
    if not value.startswith("https://"):
        raise PackageError("build.repository must be HTTPS without credentials")
    authority = re.split(r"[/?#]", value[len("https://") :], maxsplit=1)[0]
    if not authority or "@" in authority:
        raise PackageError("build.repository must be HTTPS without credentials")
    return value


def _validate_fields(fields: object, payload: bytes | None) -> dict:
    format_version = fields.get("format") if isinstance(fields, dict) else None
    if type(format_version) is not int or format_version not in (2, 3):
        raise PackageError("format must be the TOML integer 2 or 3")
    root = _exact_dict(
        fields,
        {"format", "core", "target", "payload", "abi", "interfaces", "build"} | ({"rom"} if format_version == 3 else set()),
        set(),
        "manifest",
    )

    core = _exact_dict(root["core"], {"id", "name", "description", "version"}, {"system"}, "core")
    _identifier(core["id"], "core.id")
    _string(core["name"], "core.name", nonempty=True, max_bytes=128)
    _string(core["description"], "core.description", max_bytes=2_048)
    version = _string(core["version"], "core.version", nonempty=True)
    if SEMVER_RE.fullmatch(version) is None:
        raise PackageError("core.version must be a SemVer release")
    if "system" in core:
        _identifier(core["system"], "core.system")

    target = _exact_dict(root["target"], {"platform", "device", "programming_profile"}, set(), "target")
    _identifier(target["platform"], "target.platform")
    _string(target["device"], "target.device", nonempty=True)
    _identifier(target["programming_profile"], "target.programming_profile")
    if target["platform"] != "de10_nano" or target["device"] != "5CSEBA6U23I7":
        raise PackageError("target must be de10_nano device 5CSEBA6U23I7")

    payload_fields = _exact_dict(root["payload"], {"file", "size", "sha256"}, set(), "payload")
    if payload_fields["file"] != "core.rbf":
        raise PackageError("payload.file must be core.rbf")
    declared_size = _integer(payload_fields["size"], "payload.size", 1, MAX_PAYLOAD_SIZE)
    declared_digest = _string(payload_fields["sha256"], "payload.sha256")
    if HEX64_RE.fullmatch(declared_digest) is None:
        raise PackageError("payload.sha256 must be 64 lowercase hexadecimal characters")
    if payload is not None:
        if len(payload) != declared_size:
            raise PackageError("payload size does not match manifest")
        if hashlib.sha256(payload).hexdigest() != declared_digest:
            raise PackageError("payload digest does not match manifest")

    if format_version == 3:
        rom = _exact_dict(root["rom"], {"id", "role", "source_size", "file", "size", "sha256"}, set(), "rom")
        _identifier(rom["id"], "rom.id")
        if rom["role"] not in ("firmware", "cartridge"):
            raise PackageError("rom.role must be firmware or cartridge")
        source_size = _integer(rom["source_size"], "rom.source_size", 1024, 262144)
        if source_size % 1024:
            raise PackageError("rom.source_size must be divisible by 1024")
        if rom["file"] != "rom-map.json":
            raise PackageError("rom.file must be rom-map.json")
        _integer(rom["size"], "rom.size", 1, MAX_ROM_MAP_SIZE)
        if HEX64_RE.fullmatch(_string(rom["sha256"], "rom.sha256")) is None:
            raise PackageError("rom.sha256 must be lowercase SHA256")

    _versioned_contract(root["abi"], "abi")
    interfaces = root["interfaces"]
    if not isinstance(interfaces, list):
        raise PackageError("interfaces must be an array of tables")
    seen: set[str] = set()
    for index, value in enumerate(interfaces):
        field = f"interfaces[{index}]"
        interface = _exact_dict(value, {"id", "major", "minor", "required"}, set(), field)
        identifier = _identifier(interface["id"], f"{field}.id")
        _integer(interface["major"], f"{field}.major", 1, 65_535)
        _integer(interface["minor"], f"{field}.minor", 0, 65_535)
        if type(interface["required"]) is not bool:
            raise PackageError(f"{field}.required must be a TOML Boolean")
        if identifier in seen:
            raise PackageError(f"duplicate interface id: {identifier}")
        seen.add(identifier)

    build = _exact_dict(root["build"], {"id", "repository", "revision", "recipe_sha256", "toolchain"}, set(), "build")
    build_id = _string(build["id"], "build.id")
    if HEX32_RE.fullmatch(build_id) is None:
        raise PackageError("build.id must be 32 lowercase hexadecimal characters")
    _repository(build["repository"])
    revision = _string(build["revision"], "build.revision")
    if HEX40_RE.fullmatch(revision) is None:
        raise PackageError("build.revision must be a full 40-hex Git commit")
    recipe_digest = _string(build["recipe_sha256"], "build.recipe_sha256")
    if HEX64_RE.fullmatch(recipe_digest) is None:
        raise PackageError("build.recipe_sha256 must be 64 lowercase hexadecimal characters")
    _string(build["toolchain"], "build.toolchain", nonempty=True, max_bytes=1_024)
    return root


def _decode_manifest(manifest: bytes, payload: bytes | None) -> dict:
    if not 1 <= len(manifest) <= MAX_MANIFEST_SIZE:
        raise PackageError(f"manifest size must be 1 through {MAX_MANIFEST_SIZE} bytes")
    try:
        text = manifest.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise PackageError("manifest must be valid UTF-8") from exc
    try:
        fields = tomllib.loads(text)
    except tomllib.TOMLDecodeError as exc:
        raise PackageError("manifest is not valid TOML") from exc
    return _validate_fields(fields, payload)


def validate_rom_map(data: bytes, fields: dict) -> dict:
    """Validate sealed map structure and bindings without decoding FPGA frames."""
    rom = fields["rom"]
    if len(data) != rom["size"] or hashlib.sha256(data).hexdigest() != rom["sha256"]:
        raise PackageError("ROM map size or digest does not match manifest")

    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise PackageError("duplicate ROM map key")
            result[key] = value
        return result

    def reject_number(value):
        raise PackageError("ROM map numbers must be integers")

    try:
        mapping = json.loads(data.decode("utf-8"), object_pairs_hook=pairs,
                             parse_float=reject_number, parse_constant=reject_number)
    except (UnicodeDecodeError, ValueError, RecursionError) as exc:
        raise PackageError("ROM map must be valid UTF-8 JSON with unique keys") from exc
    _exact_dict(mapping, {"format", "device", "encoding", "base_sha256", "source_size", "blocks"}, set(), "ROM map")
    if type(mapping["format"]) is not int or mapping["format"] != 1:
        raise PackageError("unsupported ROM map format")
    if mapping["device"] != fields["target"]["device"] or mapping["encoding"] != "m10k-1024x10-v1":
        raise PackageError("unsupported ROM map device or encoding")
    if mapping["base_sha256"] != fields["payload"]["sha256"]:
        raise PackageError("ROM map base digest mismatch")
    if type(mapping["source_size"]) is not int or mapping["source_size"] != rom["source_size"]:
        raise PackageError("ROM map source size mismatch")
    blocks = mapping["blocks"]
    if not isinstance(blocks, list) or len(blocks) != rom["source_size"] // 1024:
        raise PackageError("ROM map block count differs from source size")
    bels, sources = set(), set()
    destinations = bytearray((7605 * 7024 + 7) // 8)
    for block in blocks:
        _exact_dict(block, {"bel", "source_offset", "word_bits"}, set(), "ROM block")
        bel = _string(block["bel"], "ROM BEL", nonempty=True, max_bytes=64)
        if bel in bels:
            raise PackageError("duplicate ROM BEL")
        bels.add(bel)
        offset = _integer(block["source_offset"], "ROM source offset", 0, rom["source_size"] - 1024)
        if offset % 1024 or offset in sources:
            raise PackageError("invalid or overlapping ROM source range")
        sources.add(offset)
        words = block["word_bits"]
        if not isinstance(words, list) or len(words) != 256:
            raise PackageError("ROM block must have 256 words")
        for word in words:
            if not isinstance(word, list) or len(word) != 40:
                raise PackageError("ROM word must have 40 destinations")
            for bit in word:
                _integer(bit, "ROM destination", 32 * 7605, 7605 * 7024 - 1)
                index, mask = bit // 8, 1 << (bit % 8)
                if destinations[index] & mask:
                    raise PackageError("overlapping ROM destinations")
                destinations[index] |= mask
    return mapping


def _toml_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def encode_manifest(fields: dict) -> bytes:
    """Encode validated package fields as deterministic UTF-8 TOML."""

    root = _validate_fields(fields, None)
    core, target, payload, abi, build = (root[name] for name in ("core", "target", "payload", "abi", "build"))
    lines = [
        f"format = {root['format']}",
    ]
    if not root["interfaces"]:
        lines.append("interfaces = []")
    lines.extend([
        "",
        "[core]",
        f"id = {_toml_string(core['id'])}",
        f"name = {_toml_string(core['name'])}",
        f"description = {_toml_string(core['description'])}",
        f"version = {_toml_string(core['version'])}",
    ])
    if "system" in core:
        lines.append(f"system = {_toml_string(core['system'])}")
    lines.extend([
        "",
        "[target]",
        f"platform = {_toml_string(target['platform'])}",
        f"device = {_toml_string(target['device'])}",
        f"programming_profile = {_toml_string(target['programming_profile'])}",
        "",
        "[payload]",
        f"file = {_toml_string(payload['file'])}",
        f"size = {payload['size']}",
        f"sha256 = {_toml_string(payload['sha256'])}",
        "",
        "[abi]",
        f"id = {_toml_string(abi['id'])}",
        f"major = {abi['major']}",
        f"minor = {abi['minor']}",
    ])
    for interface in root["interfaces"]:
        lines.extend([
            "",
            "[[interfaces]]",
            f"id = {_toml_string(interface['id'])}",
            f"major = {interface['major']}",
            f"minor = {interface['minor']}",
            f"required = {'true' if interface['required'] else 'false'}",
        ])
    lines.extend([
        "",
        "[build]",
        f"id = {_toml_string(build['id'])}",
        f"repository = {_toml_string(build['repository'])}",
        f"revision = {_toml_string(build['revision'])}",
        f"recipe_sha256 = {_toml_string(build['recipe_sha256'])}",
        f"toolchain = {_toml_string(build['toolchain'])}",
    ])
    if root["format"] == 3:
        lines.extend(["", "[rom]"])
        for key in ("id", "role", "source_size", "file", "size", "sha256"):
            value = root["rom"][key]
            lines.append(f"{key} = {_toml_string(value) if isinstance(value, str) else value}")
    encoded = ("\n".join(lines) + "\n").encode("utf-8")
    if len(encoded) > MAX_MANIFEST_SIZE:
        raise PackageError(f"encoded manifest exceeds {MAX_MANIFEST_SIZE} bytes")
    return encoded


def _read_fd(fd: int, size: int, maximum: int, field: str) -> bytes:
    if not 1 <= size <= maximum:
        raise PackageError(f"{field} size must be 1 through {maximum} bytes")
    chunks: list[bytes] = []
    remaining = size
    while remaining:
        chunk = os.read(fd, min(remaining, 1024 * 1024))
        if not chunk:
            raise PackageError(f"{field} was truncated while reading")
        chunks.append(chunk)
        remaining -= len(chunk)
    if os.read(fd, 1):
        raise PackageError(f"{field} grew while reading")
    return b"".join(chunks)


def _read_directory(path: Path) -> tuple[bytes, bytes, bytes | None]:
    flags = os.O_RDONLY | os.O_DIRECTORY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        directory_fd = os.open(path, flags)
    except OSError as exc:
        raise PackageError(f"cannot open package directory: {path}") from exc
    try:
        names = set(os.listdir(directory_fd))
        if names not in ({"manifest.toml", "core.rbf"}, {"manifest.toml", "core.rbf", "rom-map.json"}):
            raise PackageError("package directory has unexpected members")
        values: list[bytes] = []
        members = [("manifest.toml", MAX_MANIFEST_SIZE), ("core.rbf", MAX_PAYLOAD_SIZE)]
        if "rom-map.json" in names:
            members.append(("rom-map.json", MAX_ROM_MAP_SIZE))
        for name, maximum in members:
            try:
                fd = os.open(
                    name,
                    os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0),
                    dir_fd=directory_fd,
                )
            except OSError as exc:
                raise PackageError(f"cannot open package member: {name}") from exc
            try:
                metadata = os.fstat(fd)
                if not stat.S_ISREG(metadata.st_mode):
                    raise PackageError(f"package member must be a regular file: {name}")
                values.append(_read_fd(fd, metadata.st_size, maximum, name))
            finally:
                os.close(fd)
        if set(os.listdir(directory_fd)) != names:
            raise PackageError("package directory changed while reading")
        return values[0], values[1], values[2] if len(values) == 3 else None
    finally:
        os.close(directory_fd)


def _parse_octal_size(header: bytes, field: str) -> int:
    raw = header[124:136]
    if re.fullmatch(b"[0-7]{11}\0", raw) is None:
        raise PackageError(f"{field} archive size is not canonical octal")
    return int(raw[:-1], 8)


def _ustar_header(name: str, size: int) -> bytes:
    info = tarfile.TarInfo(name)
    info.mode = 0o644
    info.uid = 0
    info.gid = 0
    info.size = size
    info.mtime = 0
    info.type = tarfile.REGTYPE
    info.linkname = ""
    info.uname = ""
    info.gname = ""
    return info.tobuf(format=tarfile.USTAR_FORMAT, encoding="ascii", errors="strict")


def _read_archive(path: Path) -> tuple[bytes, bytes, bytes | None]:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        fd = os.open(path, flags)
    except OSError as exc:
        raise PackageError(f"cannot open package archive: {path}") from exc
    try:
        metadata = os.fstat(fd)
        if not stat.S_ISREG(metadata.st_mode):
            raise PackageError("package archive must be a regular non-symlink file")
        data = _read_fd(fd, metadata.st_size, MAX_ARCHIVE_SIZE_V3, "archive")
    finally:
        os.close(fd)

    offset = 0
    values: list[bytes] = []
    members = [("manifest.toml", MAX_MANIFEST_SIZE), ("core.rbf", MAX_PAYLOAD_SIZE)]
    for name, maximum in members:
        if offset + 512 > len(data):
            raise PackageError(f"archive is missing the {name} header")
        header = data[offset : offset + 512]
        size = _parse_octal_size(header, name)
        if not 1 <= size <= maximum:
            raise PackageError(f"{name} size must be 1 through {maximum} bytes")
        if header != _ustar_header(name, size):
            raise PackageError(f"{name} header is not canonical restricted ustar")
        offset += 512
        padded_size = (size + 511) & ~511
        if offset + padded_size > len(data):
            raise PackageError(f"archive member {name} is truncated")
        values.append(data[offset : offset + size])
        if any(data[offset + size : offset + padded_size]):
            raise PackageError(f"archive member {name} has nonzero padding")
        offset += padded_size
        if name == "manifest.toml":
            fields = _decode_manifest(values[0], None)
            if fields["format"] == 3:
                members.append(("rom-map.json", MAX_ROM_MAP_SIZE))
            elif len(data) > MAX_ARCHIVE_SIZE:
                raise PackageError("format-2 archive exceeds 33 MiB")
    if data[offset:] != b"\0" * 1024:
        raise PackageError("archive must end with exactly two zero blocks")
    return values[0], values[1], values[2] if len(values) == 3 else None


def read_package(path: Path) -> CorePackage:
    """Read and validate an exact format-2/3 directory or restricted .fcore archive."""

    path = Path(path)
    try:
        metadata = path.lstat()
    except OSError as exc:
        raise PackageError(f"cannot inspect package path: {path}") from exc
    if stat.S_ISLNK(metadata.st_mode):
        raise PackageError("package path must not be a symlink")
    if stat.S_ISDIR(metadata.st_mode):
        manifest, payload, rom_map = _read_directory(path)
    elif stat.S_ISREG(metadata.st_mode):
        manifest, payload, rom_map = _read_archive(path)
    else:
        raise PackageError("package path must be a directory or regular archive")
    fields = _decode_manifest(manifest, payload)
    if (fields["format"] == 3) != (rom_map is not None):
        raise PackageError("package members do not match declared format")
    if rom_map is not None:
        validate_rom_map(rom_map, fields)
    return CorePackage(manifest, fields, payload, package_identity(manifest, payload, rom_map), rom_map)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    inspect_parser = subparsers.add_parser("inspect", help="inspect a package directory or .fcore")
    inspect_parser.add_argument("path", type=Path)
    arguments = parser.parse_args(argv)
    try:
        package = read_package(arguments.path)
        print(json.dumps({"manifest": package.fields, "package_id": package.package_id}, indent=2, sort_keys=True))
    except (OSError, PackageError) as exc:
        print(f"core-package: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
