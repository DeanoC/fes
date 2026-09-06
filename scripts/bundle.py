"""Validate a misteross bundle and install it as FogCast's native core input."""
from pathlib import Path
import hashlib
import re
import shutil
import tomllib

REPOSITORY = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
REVISION = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
TOOLCHAIN = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def load(directory, expected_recipe_sha256=None):
    directory = Path(directory)
    artifact = directory / "megadrive.rbf"
    manifest_path = directory / "megadrive-rbf.toml"
    if artifact.is_symlink() or manifest_path.is_symlink():
        raise ValueError("bundle files must not be symlinks")
    manifest = tomllib.loads(manifest_path.read_text())
    fixed = {
        "format": 1, "abi": "mister", "system": "megadrive",
        "artifact": "megadrive.rbf", "repository": REPOSITORY,
        "revision": REVISION, "recipe": "scripts/rebuild_core.py",
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


def prepare(fogcast, directory, expected_recipe_sha256=None):
    fogcast = Path(fogcast)
    directory = Path(directory)
    manifest = load(directory, expected_recipe_sha256)
    cache = fogcast / "build/cache/target-image/native"
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
