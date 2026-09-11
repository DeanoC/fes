"""Validate a misteross bundle and install it as FogCast's native core input."""
from pathlib import Path
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tomllib

REPOSITORY = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
REVISION = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
TOOLCHAIN = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
MISTEROSS_REPOSITORY = "https://github.com/DeanoC/misteross.git"
HEX40 = re.compile(r"[0-9a-f]{40}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def load(directory, expected_recipe_sha256=None, *, system="megadrive", expected_revision=None):
    policies = {
        "megadrive": (REPOSITORY, REVISION, "scripts/rebuild_core.py"),
        "snes": ("https://github.com/MiSTer-devel/SNES_MiSTer",
                 "93d359e6f23c734ae3928984e88bed1d9b53cbac", "scripts/rebuild_core.py"),
        "nes": ("https://github.com/MiSTer-devel/NES_MiSTer",
                "9a63821173b6da4d6e95dcbe2e2a322ec8171144", "scripts/rebuild_core.py"),
        "pong": ("https://github.com/DeanoC/misteross", expected_revision, "scripts/build_pong.py"),
    }
    if system not in policies:
        raise ValueError("unsupported bundle system")
    repository, revision, recipe = policies[system]
    if system == "pong" and (not isinstance(expected_revision, str) or
                             not re.fullmatch(r"[0-9a-f]{40}", expected_revision)):
        raise ValueError("Pong bundle requires the selected source revision")
    if expected_revision is not None and expected_revision != revision:
        raise ValueError("bundle expected revision differs from policy")
    directory = Path(directory)
    artifact = directory / f"{system}.rbf"
    manifest_path = directory / f"{system}-rbf.toml"
    if artifact.is_symlink() or manifest_path.is_symlink():
        raise ValueError("bundle files must not be symlinks")
    if not artifact.is_file() or not manifest_path.is_file():
        raise ValueError("bundle files must be regular files")
    if artifact.stat().st_size == 0:
        raise ValueError("bundle artifact must not be empty")
    manifest = tomllib.loads(manifest_path.read_text())
    fields = {"format", "abi", "system", "artifact", "repository", "revision",
              "recipe", "toolchain", "sha256", "size", "recipe_sha256"}
    if set(manifest) != fields:
        raise ValueError("bundle manifest fields differ from schema")
    if type(manifest["format"]) is not int or type(manifest["size"]) is not int:
        raise ValueError("bundle manifest format and size must be integers")
    fixed = {
        "format": 1, "abi": "mister", "system": system,
        "artifact": f"{system}.rbf", "repository": repository,
        "revision": revision, "recipe": recipe,
        "toolchain": TOOLCHAIN,
    }
    for key, value in fixed.items():
        if manifest.get(key) != value:
            raise ValueError(f"bundle manifest {key} differs from policy")
    if manifest.get("sha256") != digest(artifact):
        raise ValueError("bundle artifact digest differs from manifest")
    if manifest.get("size") != artifact.stat().st_size:
        raise ValueError("bundle artifact size differs from manifest")
    if not re.fullmatch(r"[0-9a-f]{64}", str(manifest.get("recipe_sha256", ""))):
        raise ValueError("bundle recipe digest is invalid")
    if expected_recipe_sha256 is not None and manifest["recipe_sha256"] != expected_recipe_sha256:
        raise ValueError("bundle recipe digest differs from pinned recipe")
    return manifest


def _replace(text, key, value):
    pattern = rf"(?m)^(\s*{re.escape(key)}\s*=\s*).*$"
    replacement = rf"\g<1>{value}"
    changed, count = re.subn(pattern, replacement, text)
    if count != 1:
        raise ValueError(f"FogCast Mega Drive lock has {count} {key} fields")
    return changed


def prepare(fogcast, directory, expected_recipe_sha256=None, cache_root=None):
    fogcast = Path(fogcast)
    directory = Path(directory)
    manifest = load(directory, expected_recipe_sha256)
    cache = Path(cache_root or fogcast) / "build/cache/target-image/native"
    lock_path = fogcast / "build/native-runtime.inputs.lock.toml"
    lock = lock_path.read_text()
    section = lock.index("[megadrive_rbf]")
    prefix, megadrive = lock[:section], lock[section:]
    megadrive = _replace(megadrive, "sha256", repr(manifest["sha256"]))
    megadrive = _replace(megadrive, "size", str(manifest["size"]))
    megadrive += (
        "artifact_kind = 'source_rebuild_bundle'\n"
        f"bundle_recipe_sha256 = '{manifest['recipe_sha256']}'\n"
    )
    lock_path.write_text(prefix + megadrive)
    cache.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(directory / "megadrive.rbf", cache / "megadrive.rbf")
    return manifest


def authenticate_misteross_origin(source):
    """Replace a local clone URL with the selected canonical producer origin."""
    source = Path(source).resolve()
    try:
        subprocess.run(["git", "-C", str(source), "remote", "set-url", "origin",
                       MISTEROSS_REPOSITORY], check=True, stdout=subprocess.PIPE,
                       stderr=subprocess.PIPE, text=True)
        urls = subprocess.check_output(
            ["git", "-C", str(source), "remote", "get-url", "--all", "origin"],
            text=True).splitlines()
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError("cannot authenticate selected misteross repository origin") from error
    if urls != [MISTEROSS_REPOSITORY]:
        raise ValueError("selected misteross checkout has an ambiguous repository origin")


def canonical_package_record(source):
    """Derive the producer's canonical pre-synthesis record in isolation."""
    source = Path(source).resolve()
    authenticate_misteross_origin(source)
    program = r'''
import sys
from pathlib import Path
from scripts import build_fes_pong as producer
root = Path.cwd().resolve()
repository, revision = producer._require_clean_source(root)
tools = producer._authenticate_tools(root)
identities = {name: tool.identity for name, tool in tools.items()}
sys.stdout.buffer.write(producer.create_build_record(root, repository, revision, identities))
'''
    try:
        record = subprocess.check_output([sys.executable, "-c", program], cwd=source)
        parsed = json.loads(record)
    except (OSError, subprocess.CalledProcessError, UnicodeDecodeError,
            json.JSONDecodeError) as error:
        raise ValueError(f"cannot derive authenticated FES Pong build inputs in {source}; "
                         f"inspect the producer error and, if tools are missing, run "
                         f"make -C {source} toolchain then doctor-strict") from error
    canonical = json.dumps(parsed, ensure_ascii=False, separators=(",", ":"),
                           sort_keys=True).encode("utf-8") + b"\n"
    if record != canonical:
        raise ValueError("producer returned a noncanonical FES Pong build-input record")
    return record


def _sealed(path, *, directory=False):
    path = Path(path)
    metadata = path.lstat()
    valid_type = stat.S_ISDIR(metadata.st_mode) if directory else stat.S_ISREG(metadata.st_mode)
    if stat.S_ISLNK(metadata.st_mode) or not valid_type or stat.S_IMODE(metadata.st_mode) & 0o222:
        raise ValueError(f"package input is not sealed: {path}")


def _plain_directory(path, field):
    path = Path(path)
    try:
        metadata = path.lstat()
    except OSError as error:
        raise ValueError(f"selected package {field} is missing") from error
    if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
        raise ValueError(f"selected package {field} must be a non-symlink directory")


def _inspect_package_candidate(source, package, record_path):
    """Use the selected producer/reader to validate package and build evidence."""
    source = Path(source).resolve()
    package = Path(package).absolute()
    record_path = Path(record_path).absolute()
    _sealed(package, directory=True)
    _sealed(record_path)
    for name in ("manifest.toml", "core.rbf"):
        _sealed(package / name)
    program = r'''
import hashlib
import json
import sys
from pathlib import Path
from scripts import build_fes_pong as producer
from scripts.core_package import read_package
from scripts.export_core_package import build_identity, _decode_build_record, _verify_build_evidence
root = Path.cwd().resolve()
package_path = Path(sys.argv[1]).resolve()
record_path = Path(sys.argv[2]).resolve()
record = record_path.read_bytes()
record_fields = _decode_build_record(record)
package = read_package(package_path)
manifest = package.fields
_verify_build_evidence(package_path / "core.rbf", record, record_fields, manifest)
build = manifest["build"]
toolchain = "; ".join(f"{name} {record_fields['tools'][name]}" for name in sorted(record_fields["tools"]))
if package_path.name != package.package_id:
    raise ValueError("package directory name differs from package identity")
if (build["id"] != build_identity(record) or
    build["repository"] != record_fields["repository"] or
    build["revision"] != record_fields["revision"] or
    build["recipe_sha256"] != record_fields["recipe_sha256"] or
    build["toolchain"] != toolchain):
    raise ValueError("package descriptor differs from canonical build inputs")
print(json.dumps({"manifest": manifest, "package_id": package.package_id,
                  "manifest_sha256": hashlib.sha256(package.manifest_bytes).hexdigest(),
                  "core_rbf_sha256": hashlib.sha256(package.payload_bytes).hexdigest()}, sort_keys=True))
'''
    try:
        result = subprocess.run([sys.executable, "-c", program, str(package), str(record_path)],
                                cwd=source, text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, check=True)
        inspected = json.loads(result.stdout)
    except (OSError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) else ""
        raise ValueError("cached FES Pong package failed producer validation" +
                         (f": {detail}" if detail else "")) from error
    if set(inspected) != {"manifest", "package_id", "manifest_sha256", "core_rbf_sha256"}:
        raise ValueError("producer package inspection returned an unexpected result")
    return inspected


def _build_fes_pong(source):
    try:
        subprocess.run([sys.executable, "scripts/build_fes_pong.py", "--root", str(source),
                        "--package-output", str(Path(source) / "build/packages")],
                       cwd=source, check=True)
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError("selected FES Pong recipe failed") from error


def _matching_package_candidates(source, record):
    source = Path(source).absolute()
    build = source / "build"
    store = build / "packages"
    _plain_directory(source, "source checkout")
    for path, field in ((build, "build root"), (store, "store")):
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            return []
        except OSError as error:
            raise ValueError(f"selected package {field} is unavailable") from error
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
            raise ValueError(f"selected package {field} must be a non-symlink directory")
    matches = []
    for sidecar in sorted(store.glob("*.build-inputs.json")):
        if sidecar.is_symlink() or not sidecar.is_file():
            continue
        try:
            candidate_record = sidecar.read_bytes()
        except OSError:
            continue
        if candidate_record != record:
            continue
        identity = sidecar.name.removesuffix(".build-inputs.json")
        if HEX64.fullmatch(identity) is None:
            raise ValueError("matching package evidence has an invalid package ID filename")
        package = store / identity
        _plain_directory(package, "candidate")
        inspected = _inspect_package_candidate(source, package, sidecar)
        if inspected.get("package_id") != identity:
            raise ValueError("matching package evidence differs from inspected package identity")
        matches.append((package, sidecar, inspected))
    return matches


def _selection_bytes(selection):
    order = ("format", "kind", "core_id", "package_id", "payload_sha256",
             "misteross_revision", "mister_packages_revision", "install_path")
    if set(selection) != set(order) or selection["format"] != 2:
        raise ValueError("invalid FES Pong package selection")
    lines = [f"format = {selection['format']}"] + [
        f"{key} = {json.dumps(selection[key], ensure_ascii=False)}" for key in order[1:]]
    return ("\n".join(lines) + "\n").encode("utf-8")


def _publish_selection(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink() or (path.exists() and not path.is_file()):
        raise ValueError("package selection destination must be a regular file")
    temporary = path.with_name(path.name + ".tmp")
    temporary.unlink(missing_ok=True)
    try:
        temporary.write_bytes(data)
        temporary.chmod(0o444)
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


def resolve_core_package(source, mister_packages_revision, selection_path, force=False):
    """Resolve the unique authenticated package result and emit its closed selection."""
    if HEX40.fullmatch(str(mister_packages_revision)) is None:
        raise ValueError("mister-packages revision must be a full lowercase commit")
    source = Path(source).absolute()
    _plain_directory(source, "source checkout")
    record = canonical_package_record(source)
    if force:
        _build_fes_pong(source)
    candidates = _matching_package_candidates(source, record)
    if not candidates and not force:
        _build_fes_pong(source)
        candidates = _matching_package_candidates(source, record)
    if not candidates:
        raise ValueError("selected recipe did not produce a matching FES Pong package")
    if len(candidates) != 1:
        raise ValueError("multiple package IDs match the canonical FES Pong build inputs")
    package, _, inspected = candidates[0]
    manifest = inspected["manifest"]
    package_id = inspected["package_id"]
    payload_sha256 = manifest["payload"]["sha256"]
    misteross_revision = manifest["build"]["revision"]
    selection = {
        "format": 2,
        "kind": "core-package",
        "core_id": "fes.pong",
        "package_id": package_id,
        "payload_sha256": payload_sha256,
        "misteross_revision": misteross_revision,
        "mister_packages_revision": str(mister_packages_revision),
        "install_path": "/usr/share/mister-runtime/core-packages/" + package_id,
    }
    if (manifest.get("core", {}).get("id") != "fes.pong" or
            HEX64.fullmatch(str(package_id)) is None or
            HEX64.fullmatch(str(payload_sha256)) is None or
            HEX40.fullmatch(str(misteross_revision)) is None):
        raise ValueError("inspected package cannot satisfy the FES Pong selection")
    encoded = _selection_bytes(selection)
    manifest_sha256 = digest(package / "manifest.toml")
    core_rbf_sha256 = digest(package / "core.rbf")
    if (manifest_sha256 != inspected["manifest_sha256"] or
            core_rbf_sha256 != inspected["core_rbf_sha256"] or
            core_rbf_sha256 != payload_sha256):
        raise ValueError("selected package members changed after inspection")
    inputs = {
        "selection": selection,
        "selection_sha256": hashlib.sha256(encoded).hexdigest(),
        "manifest_sha256": manifest_sha256,
        "core_rbf_sha256": core_rbf_sha256,
    }
    _publish_selection(selection_path, encoded)
    return {"directory": package, "selection_path": Path(selection_path),
            "inputs": inputs}
