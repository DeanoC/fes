#!/usr/bin/env python3
"""Run existing isolated acceptance using a frozen core-dev candidate.

All other options are those of package_acceptance_isolated.py. Explicit platform
identities, exclusive kit use and --execute remain required.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys
import tomllib
from urllib.parse import urlsplit

from core_dev import snapshot
import package_acceptance_isolated as isolated

OWNED = frozenset({"--archive", "--expected-archive-sha256", "--expected-package-id",
                   "--expected-core-id", "--library-media", "--expected-media-sha256"})


def _object(value, required, optional=()):
    if not isinstance(value, dict) or not required <= value.keys() or value.keys() - required - set(optional):
        raise ValueError("invalid prepared receipt fields")
    return value


def _file(root, record, name, maximum, *, fixed=None):
    if not isinstance(record, dict):
        raise ValueError(f"invalid {name} record")
    path = record.get("path")
    if (not isinstance(path, str) or not re.fullmatch(r"[a-zA-Z0-9_.-]+", path)
            or path in (".", "..") or (fixed is not None and path != fixed)):
        raise ValueError(f"invalid {name} filename")
    digest = record.get("sha256")
    if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError(f"invalid {name} digest")
    observed = snapshot(root / path, limit=maximum, expected=digest)
    if "size" in record and (type(record["size"]) is not int or record["size"] != observed["size"]):
        raise ValueError(f"invalid {name} size")
    return str(root / path)


def source_selection(root, record, sources, selected, package, payload):
    record = _object(record, {"path", "sha256"})
    expected_name = Path(selected["path"]).with_suffix(".provenance.json").name
    filename = _file(root, record, "source selection", 65536, fixed=expected_name)
    fd = os.open(filename, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError("source selection must be regular")
        raw = stream.read(65537)
    if not raw or len(raw) > 65536 or hashlib.sha256(raw).hexdigest() != record["sha256"]:
        raise ValueError("source selection changed while reading")
    value = _object(json.loads(raw), {
        "format", "selected_repository", "selected_revision", "selected_source_path",
        "original_repository", "original_revision", "original_source_path",
        "functional_inputs_sha256", "original_record_sha256", "selected_record_sha256",
        "package_id", "core_rbf_sha256"})
    if type(value["format"]) is not int or value["format"] != 1:
        raise ValueError("unsupported source selection format")
    for prefix in ("selected", "original"):
        revision = value[prefix + "_revision"]
        if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-f]{40}", revision):
            raise ValueError("invalid source selection revision")
        repository = value[prefix + "_repository"]
        if not isinstance(repository, str) or not repository or any(ord(c) <= 32 or ord(c) >= 127 for c in repository):
            raise ValueError("invalid source selection repository")
        if any(char in repository for char in '\\<>"{}|^`'):
            raise ValueError("invalid source selection repository")
        url = urlsplit(repository)
        _ = url.port  # Reject malformed or out-of-range port authorities.
        if (url.scheme != "https" or not url.hostname or url.username is not None or
                url.password is not None or re.search(r"%(?![0-9a-fA-F]{2})", repository)):
            raise ValueError("invalid source selection repository")
        path = value[prefix + "_source_path"]
        if (not isinstance(path, str) or not path or "\\" in path or
                any(ord(c) < 32 or ord(c) == 127 for c in path) or
                (path != "." and (PurePosixPath(path).is_absolute() or
                 PurePosixPath(path).as_posix() != path or
                 any(part in ("", ".", "..") for part in path.split("/"))))):
            raise ValueError("invalid source selection path")
    for key in ("functional_inputs_sha256", "original_record_sha256", "selected_record_sha256", "package_id", "core_rbf_sha256"):
        if not isinstance(value[key], str) or not re.fullmatch(r"[0-9a-f]{64}", value[key]):
            raise ValueError("invalid source selection digest")
    if (value["selected_revision"] != sources["misteross"] or
            value["original_revision"] != selected["misteross_revision"] or
            value["package_id"] != package or value["core_rbf_sha256"] != payload):
        raise ValueError("source selection differs from prepared identity")
    return value


def candidate_arguments(receipt_path, provenance=None):
    path = Path(receipt_path).absolute()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError("prepared receipt must be a regular file")
        raw = stream.read(65537)
    if not raw or len(raw) > 65536:
        raise ValueError("prepared receipt exceeds its size bound")
    data = _object(json.loads(raw),
                   {"format", "core_id", "package_id", "archive", "sources", "selection"},
                   {"library_media", "source_selection"})
    if type(data["format"]) is not int or data["format"] not in (1, 2):
        raise ValueError("unsupported prepared receipt format")
    if (data["format"] == 2) != ("source_selection" in data):
        raise ValueError("prepared format and source selection disagree")
    core = isolated._require_core_id(data["core_id"])
    package = isolated._require_package_id(data["package_id"], "prepared package ID")
    sources = _object(data["sources"], {"FogCast", "libmister-runtime", "misteross", "mister-packages"})
    if any(not isinstance(v, str) or not re.fullmatch(r"[0-9a-f]{40}", v) for v in sources.values()):
        raise ValueError("invalid prepared source revisions")
    archive = _object(data["archive"], {"path", "sha256", "size"})
    archive_path = _file(path.parent, archive, "archive", 33 * 1024 * 1024, fixed="core.fcore")
    selection = _object(data["selection"], {"path", "sha256", "manifest_sha256", "payload_sha256"})
    for key in ("manifest_sha256", "payload_sha256"):
        if not isinstance(selection[key], str) or not re.fullmatch(r"[0-9a-f]{64}", selection[key]):
            raise ValueError(f"invalid prepared {key}")
    selection_path = _file(path.parent, selection, "selection", 65536)
    selected = tomllib.loads(Path(selection_path).read_text())
    if (selected.get("format") != 2 or selected.get("kind") != "core-package"
            or selected.get("core_id") != core or selected.get("package_id") != package
            or (data["format"] == 1 and selected.get("misteross_revision") != sources["misteross"])
            or selected.get("mister_packages_revision") != sources["mister-packages"]
            or selected.get("payload_sha256") != selection["payload_sha256"]):
        raise ValueError("prepared selection differs from receipt")
    if data["format"] == 2:
        source_selection(path.parent, data["source_selection"], sources,
                         {**selected, "path": selection["path"]}, package, selection["payload_sha256"])
    result = ["--archive", archive_path, "--expected-archive-sha256", archive["sha256"],
              "--expected-package-id", package, "--expected-core-id", core]
    if "library_media" in data:
        media = _object(data["library_media"], {"path", "sha256", "size"})
        media_path = _file(path.parent, media, "media", 32 * 1024 * 1024, fixed="media.bin")
        result += ["--library-media", media_path, "--expected-media-sha256", media["sha256"]]
    if provenance is not None:
        provenance.update(format=1, prepared_sha256=hashlib.sha256(raw).hexdigest(),
                          core_id=core, package_id=package,
                          archive_sha256=archive["sha256"])
        if data["format"] == 2:
            provenance["source_selection_sha256"] = data["source_selection"]["sha256"]
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False,
                                     epilog="Use package_acceptance_isolated.py --help for platform options.")
    parser.add_argument("--prepared", required=True, help="exact prepared.json from core-dev")
    args, remainder = parser.parse_known_args(argv)
    try:
        if any(arg.split("=", 1)[0] in OWNED for arg in remainder):
            raise ValueError("candidate identity options cannot be overridden")
        # Parse all downstream options before any host/container operation. Disable
        # abbreviations so --arch cannot bypass the candidate identity prohibition.
        provenance = {}
        forwarded = candidate_arguments(args.prepared, provenance) + remainder
        downstream = isolated.parser()
        downstream.allow_abbrev = False
        options = downstream.parse_args(forwarded)
        result = isolated.main(forwarded)
        if result == 0:
            # Add a link to the exact preparation document without rewriting the
            # existing runner's lifecycle receipt or claiming source re-attestation.
            with (Path(options.evidence_dir) / "preparation.json").open("x") as record:
                json.dump(provenance, record, sort_keys=True, indent=2)
                record.write("\n")
        return result
    except (ValueError, OSError, isolated.AcceptanceError) as error:
        print(f"core-dev acceptance: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
