"""Validate a misteross bundle and install it as FogCast's native core input."""
from pathlib import Path
import hashlib
import json
import os
import re
import stat
import subprocess
import sys

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))
from recipes import FORMAT2_RECIPES, TOOLCHAIN_CACHE_ROOT, ARTIFACT_CACHE_ROOT, producer_environment, recipe_for
import artifact_cache

MISTEROSS_REPOSITORY = "https://github.com/DeanoC/misteross.git"
FES_REPOSITORY = "https://github.com/DeanoC/fes.git"
HEX40 = re.compile(r"[0-9a-f]{40}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")


def package_build_environment(env=None, recipe=None):
    """Build a child environment for a format-2 producer without selecting cache mode."""
    return producer_environment(env, recipe=recipe)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def authenticate_misteross_origin(source):
    """Replace a local clone URL with the selected canonical producer origin."""
    source = Path(source).resolve()
    try:
        top = Path(subprocess.check_output(
            ["git", "-C", str(source), "rev-parse", "--show-toplevel"], text=True).strip()).resolve()
        repository = MISTEROSS_REPOSITORY
        if top != source:
            if source.relative_to(top).as_posix() != "sources/misteross":
                raise ValueError("FPGA producer is outside the selected FES module")
            repository = FES_REPOSITORY
        urls = subprocess.check_output(
            ["git", "-C", str(source), "remote", "get-url", "--all", "origin"],
            text=True).splitlines()
        accepted = ([repository], [repository.removesuffix(".git")])
        if urls not in accepted:
            subprocess.run(["git", "-C", str(source), "remote", "set-url", "origin",
                           repository], check=True, stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, text=True)
            urls = subprocess.check_output(
                ["git", "-C", str(source), "remote", "get-url", "--all", "origin"],
                text=True).splitlines()
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError("cannot authenticate selected misteross repository origin") from error
    if urls not in accepted:
        raise ValueError("selected misteross checkout has an ambiguous repository origin")


def canonical_package_record(source, env=None, recipe=None):
    """Derive the selected producer's canonical pre-synthesis record in isolation."""
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    source = Path(source).resolve()
    authenticate_misteross_origin(source)
    program = r'''
import sys
from pathlib import Path
import importlib
root = Path.cwd().resolve()
producer = importlib.import_module(sys.argv[1])
authenticate = getattr(producer, sys.argv[2])
cache_root = Path(sys.argv[sys.argv.index("--cache-root") + 1])
identity_version = int(sys.argv[sys.argv.index("--identity-version") + 1])
options = {} if identity_version == 1 else {"identity_version": identity_version}
repository, revision = producer._require_clean_source(root, **options)
tools = authenticate(root, cache_root=cache_root)
identities = {name: tool.identity for name, tool in tools.items()}
if identity_version == 2:
    import tempfile
    from scripts.functional_execution import execution_environment, execution_inputs
    with tempfile.TemporaryDirectory(prefix="fes-canonical-home-") as home:
        paths = {name: tool.path for name, tool in tools.items()}
        environment = execution_environment(Path(home), paths)
        options["execution"] = execution_inputs(paths, environment, 0)
sys.stdout.buffer.write(producer.create_build_record(root, repository, revision, identities, **options))
'''
    cache_root = str(recipe.cache_root)
    command = [sys.executable, "-c", program, recipe.producer_module, recipe.authenticate,
               "--cache-root", cache_root, "--identity-version", str(recipe.identity_version)]
    try:
        record = subprocess.check_output(
            command, cwd=source, env=package_build_environment(env, recipe=recipe))
        parsed = json.loads(record)
    except (OSError, subprocess.CalledProcessError, UnicodeDecodeError,
            json.JSONDecodeError) as error:
        raise ValueError(
            f"cannot derive authenticated {recipe.core_id} build inputs in {source}; "
            f"inspect the producer error and, if the shared toolchain slot is missing, run "
            f"{recipe.producer_script} --cache-root {recipe.cache_root} after "
            f"make -C {source} toolchain with the recipe lock {recipe.lock_path}"
        ) from error
    canonical = json.dumps(parsed, ensure_ascii=False, separators=(",", ":"),
                           sort_keys=True).encode("utf-8") + b"\n"
    if record != canonical:
        raise ValueError("producer returned a noncanonical format-2 build-input record")
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


def _inspect_package_candidate(source, package, record_path, recipe=None):
    """Use the selected producer/reader to validate package and build evidence."""
    recipe = recipe_for("fes.pong") if recipe is None else recipe
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
import importlib
from scripts.core_package import read_package
from scripts.export_core_package import build_identity, _decode_build_record, _verify_build_evidence
producer = importlib.import_module(sys.argv[3])
root = Path.cwd().resolve()
package_path = Path(sys.argv[1]).resolve()
record_path = Path(sys.argv[2]).resolve()
record = record_path.read_bytes()
record_fields = _decode_build_record(record)
package = read_package(package_path)
manifest = package.fields
if record_fields["format"] == 2:
    from scripts.export_core_package import verify_record_source_at_revision
    verify_record_source_at_revision(root, record)
else:
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
        result = subprocess.run(
            [sys.executable, "-c", program, str(package), str(record_path), recipe.producer_module],
            cwd=source, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        inspected = json.loads(result.stdout)
    except (OSError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) else ""
        raise ValueError(f"cached {recipe.core_id} package failed producer validation" +
                         (f": {detail}" if detail else "")) from error
    if set(inspected) != {"manifest", "package_id", "manifest_sha256", "core_rbf_sha256"}:
        raise ValueError("producer package inspection returned an unexpected result")
    return inspected


def _build_package(source, recipe=None, env=None):
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    try:
        subprocess.run(
            [sys.executable, recipe.producer_script, "--root", str(source),
             "--package-output", str(Path(source) / "build/packages"),
             "--cache-root", str(recipe.cache_root),
             *(["--identity-version", "2"] if recipe.identity_version == 2 else [])],
            cwd=source, check=True, env=package_build_environment(env, recipe=recipe))
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError(f"selected {recipe.core_id} recipe failed") from error


def _build_fes_pong(source, env=None):
    _build_package(source, recipe=recipe_for("fes.pong"), env=env)


def _matching_package_candidates(source, record, recipe=None, *, store_override=None):
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    source = Path(source).absolute()
    build = source / "build"
    store = build / "packages" if store_override is None else Path(store_override)
    _plain_directory(source, "source checkout")
    directories = ((build, "build root"), (store, "store")) if store_override is None else ((store, "store"),)
    for path, field in directories:
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            return []
        except OSError as error:
            raise ValueError(f"selected package {field} is unavailable") from error
        if stat.S_ISLNK(metadata.st_mode) or not stat.S_ISDIR(metadata.st_mode):
            raise ValueError(f"selected package {field} must be a non-symlink directory")
    matches = []
    sidecars = sorted(store.glob("*.build-inputs.json")) if store_override is None else sorted(store.glob("*/build-inputs.json"))
    for sidecar in sidecars:
        if store_override is not None:
            if sidecar.parent.name.startswith(".publish-"):
                continue
            _plain_directory(sidecar.parent, "cache entry")
        if sidecar.is_symlink() or not sidecar.is_file():
            continue
        try:
            candidate_record = sidecar.read_bytes()
        except OSError:
            continue
        if candidate_record != record:
            if (recipe.identity_version != 2 or
                    artifact_cache.functional_key(candidate_record) != artifact_cache.functional_key(record)):
                continue
        identity = sidecar.name.removesuffix(".build-inputs.json") if store_override is None else sidecar.parent.name
        if HEX64.fullmatch(identity) is None:
            raise ValueError("matching package evidence has an invalid package ID filename")
        package = store / identity if store_override is None else sidecar.parent / identity
        _plain_directory(package, "candidate")
        inspected = _inspect_package_candidate(source, package, sidecar, recipe=recipe)
        if inspected.get("package_id") != identity:
            raise ValueError("matching package evidence differs from inspected package identity")
        matches.append((package, sidecar, inspected))
    return matches


def _selection_bytes(selection):
    order = ("format", "kind", "core_id", "package_id", "payload_sha256",
             "misteross_revision", "mister_packages_revision", "install_path")
    if set(selection) != set(order) or selection["format"] != 2:
        raise ValueError("invalid format-2 package selection")
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


def resolve_core_package(source, mister_packages_revision, selection_path, force=False, env=None,
                         recipe=None):
    """Resolve the unique authenticated package result and emit its closed selection."""
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    if HEX40.fullmatch(str(mister_packages_revision)) is None:
        raise ValueError("mister-packages revision must be a full lowercase commit")
    source = Path(source).absolute()
    _plain_directory(source, "source checkout")
    produce = {} if env is None else {"env": env}
    record = canonical_package_record(source, recipe=recipe, **produce)
    if force:
        _build_package(source, recipe=recipe, **produce)
    candidates = _matching_package_candidates(source, record, recipe=recipe)
    if not candidates and not force and recipe.identity_version == 2:
        candidates = _matching_package_candidates(source, record, recipe=recipe,
            store_override=artifact_cache.store_for(ARTIFACT_CACHE_ROOT, record))
    if not candidates and not force:
        _build_package(source, recipe=recipe, **produce)
        candidates = _matching_package_candidates(source, record, recipe=recipe)
    if not candidates:
        raise ValueError(f"selected recipe did not produce a matching {recipe.core_id} package")
    if len(candidates) > 1 and recipe.identity_version == 2:
        # Commits with identical functional inputs may have different immutable
        # manifests. Equivalent payloads can use the first stable package ID.
        if len({item[2]["core_rbf_sha256"] for item in candidates}) == 1:
            candidates = candidates[:1]
    if len(candidates) != 1:
        raise ValueError("multiple package IDs match the canonical format-2 build inputs")
    package, original_record_path, inspected = candidates[0]
    manifest = inspected["manifest"]
    package_id = inspected["package_id"]
    payload_sha256 = manifest["payload"]["sha256"]
    misteross_revision = manifest["build"]["revision"]
    selection = {
        "format": 2,
        "kind": "core-package",
        "core_id": recipe.core_id,
        "package_id": package_id,
        "payload_sha256": payload_sha256,
        "misteross_revision": misteross_revision,
        "mister_packages_revision": str(mister_packages_revision),
        "install_path": "/usr/share/mister-runtime/core-packages/" + package_id,
    }
    if (manifest.get("core", {}).get("id") != recipe.core_id or
            HEX64.fullmatch(str(package_id)) is None or
            HEX64.fullmatch(str(payload_sha256)) is None or
            HEX40.fullmatch(str(misteross_revision)) is None):
        raise ValueError(f"inspected package cannot satisfy the {recipe.core_id} selection")
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
    if recipe.identity_version == 2:
        original_record = original_record_path.read_bytes()
        original = json.loads(original_record)
        selected = json.loads(record)
        # Receipt describes selection of the original bytes; the manifest and
        # original build-input record are never relabelled for the new commit.
        inputs["source_selection"] = {
            "format": 1,
            "selected_repository": selected["repository"],
            "selected_revision": selected["revision"],
            "selected_source_path": selected["source_path"],
            "original_repository": original["repository"],
            "original_revision": original["revision"],
            "original_source_path": original["source_path"],
            "functional_inputs_sha256": artifact_cache.functional_key(record),
            "original_record_sha256": hashlib.sha256(original_record).hexdigest(),
            "selected_record_sha256": hashlib.sha256(record).hexdigest(),
            "package_id": package_id,
            "core_rbf_sha256": core_rbf_sha256,
        }
        artifact_cache.publish(ARTIFACT_CACHE_ROOT, original_record_path, package)
        receipt = json.dumps(inputs["source_selection"], sort_keys=True, indent=2).encode() + b"\n"
        _publish_selection(Path(selection_path).with_suffix(".provenance.json"), receipt)
    _publish_selection(selection_path, encoded)
    return {"directory": package, "selection_path": Path(selection_path),
            "inputs": inputs}
