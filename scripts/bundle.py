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


def load(directory, expected_recipe_sha256=None, *, system="megadrive", expected_revision=None):
    policies = {
        "megadrive": (REPOSITORY, REVISION, "scripts/rebuild_core.py"),
        "snes": ("https://github.com/MiSTer-devel/SNES_MiSTer",
                 "93d359e6f23c734ae3928984e88bed1d9b53cbac", "scripts/rebuild_core.py"),
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
