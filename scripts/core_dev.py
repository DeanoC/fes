#!/usr/bin/env python3
"""Prepare one authenticated core package; never launch, deploy or build an image."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

import build
import bundle
import inputs
import recipes

MAX_MEDIA_BYTES = 32 << 20
MAX_ARCHIVE_BYTES = 33 << 20
CHUNK_BYTES = 64 << 10


def snapshot(source, destination=None, *, limit=None, expected=None, sealed=False):
    """Hash/copy an exact regular file without following its final symlink."""
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size < 1:
            raise ValueError(f"not a nonempty regular file: {source}")
        if sealed and before.st_mode & 0o222:
            raise ValueError(f"archive is not sealed: {source}")
        if limit is not None and before.st_size > limit:
            raise ValueError(f"file exceeds {limit} bytes: {source}")
        digest = hashlib.sha256()
        size = 0
        target = open(destination, "xb") if destination is not None else None
        try:
            while chunk := stream.read(min(CHUNK_BYTES, before.st_size - size + 1)):
                size += len(chunk)
                if size > before.st_size:
                    raise ValueError(f"file grew while reading: {source}")
                digest.update(chunk)
                if target is not None:
                    target.write(chunk)
            after = os.fstat(stream.fileno())
            if (size != before.st_size or before.st_mtime_ns != after.st_mtime_ns
                    or before.st_ctime_ns != after.st_ctime_ns):
                raise ValueError(f"file changed while reading: {source}")
            value = digest.hexdigest()
            if expected is not None and value != expected:
                raise ValueError(f"SHA-256 mismatch: {source}")
            if target is not None:
                target.flush()
                os.fsync(target.fileno())
        finally:
            if target is not None:
                target.close()
    return {"sha256": value, "size": size}


def inspect_archive(source, archive, directory, recipe):
    """Use the selected producer's parser, not a second package implementation."""
    program = '''
import hashlib, json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from scripts.core_package import read_package
def identity(path):
    package = read_package(Path(path))
    return dict(package_id=package.package_id, core_id=package.fields['core']['id'],
                manifest_sha256=hashlib.sha256(package.manifest_bytes).hexdigest(),
                payload_sha256=hashlib.sha256(package.payload_bytes).hexdigest())
archive, directory = identity(sys.argv[2]), identity(sys.argv[3])
if archive != directory:
    raise ValueError('snapshot archive differs from resolved package directory')
print(json.dumps(archive))
'''
    return json.loads(subprocess.check_output(
        [sys.executable, "-I", "-c", program, str(source), str(archive), str(directory)],
        cwd=source, env=recipes.producer_environment(recipe=recipe), text=True))


def reconstruct_archive(source, directory, destination, recipe, record):
    """Repackage original sealed members with the selected canonical exporter."""
    directory, destination = Path(directory), Path(destination)
    metadata = directory.lstat()
    if not stat.S_ISDIR(metadata.st_mode) or metadata.st_mode & 0o222:
        raise ValueError("cached package directory must be sealed and not linked")
    snapshot(directory / "manifest.toml", limit=65536, sealed=True,
             expected=record["manifest_sha256"])
    snapshot(directory / "core.rbf", limit=32 << 20, sealed=True,
             expected=record["core_rbf_sha256"])
    temporary = destination.with_name("." + destination.name + ".tmp")
    program = '''
import os, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from scripts.core_package import read_package
from scripts.export_core_package import _archive_bytes
package = read_package(Path(sys.argv[2]))
data = _archive_bytes(package.manifest_bytes, package.payload_bytes)
if not 1 <= len(data) <= int(sys.argv[4]):
    raise ValueError('reconstructed archive exceeds size bound')
with open(sys.argv[3], 'xb') as stream:
    stream.write(data)
    stream.flush()
    os.fsync(stream.fileno())
'''
    try:
        subprocess.run([sys.executable, "-I", "-c", program, str(source), str(directory),
                        str(temporary), str(MAX_ARCHIVE_BYTES)], cwd=source,
                       env=recipes.producer_environment(recipe=recipe), check=True)
        archive = snapshot(temporary, limit=MAX_ARCHIVE_BYTES)
        temporary.chmod(0o444)
        temporary.replace(destination)
        return archive
    finally:
        temporary.unlink(missing_ok=True)


def prepare(core_id, output, library_media=None, expected_media_sha256=None):
    recipe = recipes.recipe_for(core_id)
    output = Path(output).absolute()
    if output.exists() or output.is_symlink():
        raise ValueError("output must be a new directory")
    if any(parent.is_symlink() for parent in output.parents):
        raise ValueError("output ancestors must not be symlinks")
    if not output.parent.is_dir():
        raise ValueError("output parent directory must already exist")
    if (library_media is None) != (expected_media_sha256 is None):
        raise ValueError("library media and expected SHA-256 must be supplied together")
    if library_media is not None:
        if re.fullmatch(r"[0-9a-f]{64}", expected_media_sha256) is None:
            raise ValueError("expected media SHA-256 must be lowercase hex")
        snapshot(library_media, limit=MAX_MEDIA_BYTES, expected=expected_media_sha256)

    # Same lock as all parent builds; no image/container orchestration is needed.
    lock_path = build.ROOT / "out/build.lock"
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError("another parent build is running in this workspace") from None
        revisions = inputs.validate(build.ROOT)
        output.mkdir(mode=0o700)
        try:
            media = None
            if library_media is not None:
                media = {"path": "media.bin", **snapshot(
                    library_media, output / "media.bin", limit=MAX_MEDIA_BYTES,
                    expected=expected_media_sha256)}
            source = build.source_checkout("misteross", revisions["misteross"])
            selection_path = output / recipe.selection_filename
            resolved = bundle.resolve_core_package(
                source, revisions["mister-packages"], selection_path,
                recipe=recipe)
            record = resolved["inputs"]
            package_id = record["selection"]["package_id"]
            if re.fullmatch(r"[0-9a-f]{64}", package_id) is None:
                raise ValueError("resolved package ID is invalid")
            if recipe.identity_version == 2:
                archive = reconstruct_archive(source, resolved["directory"], output / "core.fcore", recipe, record)
            else:
                archive = snapshot(source / "build/packages" / (package_id + ".fcore"),
                                   output / "core.fcore", limit=MAX_ARCHIVE_BYTES, sealed=True)
            inspected = inspect_archive(source, output / "core.fcore",
                                        resolved["directory"], recipe)
            expected = dict(package_id=package_id, core_id=core_id,
                            manifest_sha256=record["manifest_sha256"],
                            payload_sha256=record["core_rbf_sha256"])
            if inspected != expected:
                raise ValueError("archive identity differs from resolved selection")
            snapshot(selection_path, expected=record["selection_sha256"])
            receipt = dict(format=1, core_id=core_id, package_id=package_id,
                           archive={"path": "core.fcore", **archive},
                           sources=revisions, selection={
                               "path": recipe.selection_filename, "sha256": record["selection_sha256"],
                               "manifest_sha256": record["manifest_sha256"],
                               "payload_sha256": record["core_rbf_sha256"]})
            if recipe.identity_version == 2:
                provenance = record.get("source_selection")
                if not isinstance(provenance, dict):
                    raise ValueError("v2 package selection lacks source provenance")
                encoded = json.dumps(provenance, sort_keys=True, indent=2).encode() + b"\n"
                sidecar = selection_path.with_suffix(".provenance.json")
                observed = snapshot(sidecar, limit=65536, sealed=True,
                                    expected=hashlib.sha256(encoded).hexdigest())
                receipt["format"] = 2
                receipt["source_selection"] = {"path": sidecar.name, "sha256": observed["sha256"]}
            if media is not None:
                receipt["library_media"] = media
            for name in ("core.fcore", recipe.selection_filename, "media.bin"):
                if (output / name).exists():
                    (output / name).chmod(0o444)
            temporary = output / ".prepared.json.tmp"
            temporary.write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
            temporary.chmod(0o444)
            temporary.replace(output / "prepared.json")
            return receipt
        except Exception as error:
            if recipe.identity_version == 2:
                for name in ("core.fcore", ".core.fcore.tmp", ".prepared.json.tmp",
                             Path(recipe.selection_filename).with_suffix(".provenance.json").name):
                    (output / name).unlink(missing_ok=True)
            (output / "failure.json").write_text(json.dumps({
                "format": 1, "status": "failed", "error": str(error)}, indent=2) + "\n")
            raise


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    command = commands.add_parser("prepare")
    command.add_argument("--core", required=True)
    command.add_argument("--output", required=True)
    command.add_argument("--library-media")
    command.add_argument("--expected-media-sha256")
    args = parser.parse_args(argv)
    try:
        prepare(args.core, args.output, args.library_media, args.expected_media_sha256)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"core prepare: {error}", file=sys.stderr)
        return 1
    print(str(Path(args.output).absolute() / "prepared.json"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
