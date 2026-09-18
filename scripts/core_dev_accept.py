#!/usr/bin/env python3
"""Run existing isolated acceptance using a frozen core-dev candidate.

All other options are those of package_acceptance_isolated.py. Explicit platform
identities, exclusive kit use and --execute remain required.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import tomllib

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
                   {"library_media"})
    if type(data["format"]) is not int or data["format"] != 1:
        raise ValueError("unsupported prepared receipt format")
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
            or selected.get("misteross_revision") != sources["misteross"]
            or selected.get("mister_packages_revision") != sources["mister-packages"]
            or selected.get("payload_sha256") != selection["payload_sha256"]):
        raise ValueError("prepared selection differs from receipt")
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
