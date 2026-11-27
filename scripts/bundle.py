"""Resolve authenticated functional FES packages for native image assembly."""
from pathlib import Path
import hashlib
import importlib.util
import json
import os
import re
import stat
import shlex
import subprocess
import sys
import tempfile

_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))
from recipes import (FORMAT2_RECIPES, TOOLCHAIN_CACHE_ROOT, ARTIFACT_CACHE_ROOT, producer_environment,
                     recipe_for, require_private_audit_cache, selected_gpu_router,
                     CUDA_ROUTER, CUDA_ARCHITECTURES)
import artifact_cache
import optional_cores
from module_sources import normalize_known_origin

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


def _de25_license_arguments(env, recipe):
    if recipe.board != 'de25-nano' or recipe.toolchain != 'quartus':
        return []
    selected = (os.environ if env is None else env).get('FES_DE25_QUARTUS_LICENSE_FILE')
    if not selected:
        return []
    path = Path(selected)
    if not path.is_absolute():
        raise ValueError('FES_DE25_QUARTUS_LICENSE_FILE must be absolute')
    # The producer snapshots and validates this local file. Use the same
    # selection for canonical execution capture and the actual compiler run.
    return ['--license-file', str(path)]


def authenticate_misteross_origin(source):
    """Validate snapshot provenance without modifying its repository config."""
    source = Path(source).resolve()
    try:
        top = Path(subprocess.check_output(
            ["git", "-C", str(source), "rev-parse", "--show-toplevel"], text=True).strip()).resolve()
        if top == source or source.relative_to(top).as_posix() != "sources/misteross":
            raise ValueError("FPGA producer must be the exact sources/misteross FES module")
        repository = FES_REPOSITORY
        urls = subprocess.check_output(
            ["git", "-C", str(source), "remote", "get-url", "--all", "origin"],
            text=True).splitlines()
        accepted = ([repository], [repository.removesuffix(".git")])
        if len(urls) != 1 or normalize_known_origin(urls[0]) != repository:
            raise ValueError(
                "producer origin is not the expected first-party repository; "
                "inspect the source origin and explicitly select the intended repository before staging")
        if urls not in accepted:
            raise ValueError(
                "producer snapshot requires canonical HTTPS provenance; "
                "restage through FES source materialization (the source SSH origin can remain unchanged)")
    except (OSError, subprocess.CalledProcessError) as error:
        raise ValueError("cannot authenticate selected misteross repository origin") from error
    if urls not in accepted:
        raise ValueError("selected misteross checkout has an ambiguous repository origin")


def _bounded_pc386_canonical(command, source, env, *, timeout=1800):
    """Bound the authenticated product's potentially large tool-tree scan."""
    helper = Path(source) / 'cores/fes-pc386/tools/pc386_owned_job.py'
    current = Path(source)
    parts = helper.relative_to(source).parts
    for index, part in enumerate(parts):
        current = current / part
        if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
            raise ValueError('pc386 job helper must be a regular source file')
    spec = importlib.util.spec_from_file_location('fes_pc386_canonical_job', helper)
    if spec is None or spec.loader is None:
        raise ValueError('cannot load pc386 job helper')
    job = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(job)
    with tempfile.TemporaryFile() as output:
        status = job.run(command, cwd=source, env=env, timeout=timeout,
                         label='pc386 canonical inputs', stdout=output, status_stream=sys.stderr)
        if status:
            raise subprocess.CalledProcessError(status, command)
        output.seek(0)
        record = output.read(8 * 1024 * 1024 + 1)
        if len(record) > 8 * 1024 * 1024:
            raise ValueError('pc386 canonical record exceeds size bound')
        return record


def canonical_package_record(source, env=None, recipe=None, *, selection_only=False):
    """Derive the selected producer's canonical pre-synthesis record in isolation."""
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    license_arguments = _de25_license_arguments(env, recipe)
    source = Path(source).resolve()
    authenticate_misteross_origin(source)
    program = r'''
import sys
import json
from pathlib import Path
import importlib
import importlib.util
root = Path.cwd().resolve()
def load_producer(name, path):
    if not name.startswith('cores.'):
        return importlib.import_module(name)
    module_parts = name.split('.')
    expected = f"cores/{module_parts[1].replace('_', '-')}/tools/{module_parts[3]}.py" if len(module_parts) == 4 else None
    if path != expected:
        raise ValueError('core-owned producer module and source path differ')
    current = root
    parts = Path(path).parts
    for index, part in enumerate(parts):
        current = current / part
        if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
            raise ValueError('core-owned producer path must contain only regular source components')
    spec = importlib.util.spec_from_file_location(name, current)
    if spec is None or spec.loader is None:
        raise ValueError('cannot load core-owned producer')
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module
producer = load_producer(sys.argv[1], sys.argv[sys.argv.index("--producer-script") + 1])
authenticate = getattr(producer, sys.argv[2])
cache_root = Path(sys.argv[sys.argv.index("--cache-root") + 1])
identity_version = int(sys.argv[sys.argv.index("--identity-version") + 1])
assert identity_version == 2
options = dict(json.loads(sys.argv[3]), identity_version=2)
repository, revision = producer._require_clean_source(root, **options)
if "--selection-only" in sys.argv:
    # This record is only a source-selection request. Export and publication
    # reject the explicit marker; these strings are never compiler evidence.
    identities = (producer.selection_identities() if hasattr(producer, "selection_identities") else
                  ({name: "selection-only" for name in ("quartus", "qsys", "ipgenerate", "pfg")}
                   if sys.argv[1].startswith("boards.de25nano.")
                   else {"selection-only": "not authenticated"}))
    if "--quartus-yosys" in sys.argv and not hasattr(producer, "selection_identities"):
        identities = {"quartus": "selection-only", "yosys": "selection-only"}
    options["execution"] = {"selection_only": True, "gpu_device": "lease", "toolchain": "quartus"}
else:
    tools = authenticate(root, cache_root=cache_root)
    if hasattr(producer, "record_tool_options"):
        options.update(producer.record_tool_options(tools))
    identities = {name: tool.identity for name, tool in tools.items()}
    import tempfile
    from scripts.functional_execution import execution_environment, execution_inputs
    with tempfile.TemporaryDirectory(prefix="fes-canonical-home-") as home:
        paths = {name: tool.path for name, tool in tools.items()}
        if "--license-file" in sys.argv:
            license_file = Path(sys.argv[sys.argv.index("--license-file") + 1])
            environment = producer.execution_environment(Path(home), paths, license_file=license_file)
        else:
            environment = (producer.execution_environment(Path(home), paths)
                           if hasattr(producer, "execution_environment")
                           else execution_environment(Path(home), paths))
        options["execution"] = (producer.execution_inputs(paths, environment, 0)
                                if hasattr(producer, "execution_inputs")
                                else execution_inputs(paths, environment, 0))
sys.stdout.buffer.write(producer.create_build_record(root, repository, revision, identities, **options))
'''
    cache_root = str(recipe.cache_root)
    command = [sys.executable, "-c", program, recipe.producer_module, recipe.authenticate,
               json.dumps(dict(recipe.producer_options), sort_keys=True),
               "--cache-root", cache_root, "--identity-version", str(recipe.identity_version),
               "--producer-script", recipe.producer_script,
               *license_arguments, *(["--selection-only"] if selection_only else []),
               *(["--quartus-yosys"] if selection_only and recipe.toolchain == "quartus"
                 and recipe.front_end == "yosys" else [])]
    try:
        child_env = package_build_environment(env, recipe=recipe)
        selected_router = selected_gpu_router(recipe, path=child_env.get('PATH'))
        router_architecture = None
        if selected_router == CUDA_ROUTER:
            router_architecture = 'FES_TOOLCHAIN_CUDA_ARCHITECTURES=' + CUDA_ARCHITECTURES
        elif recipe.hip_architectures:
            router_architecture = 'FES_TOOLCHAIN_HIP_ARCHITECTURES=' + recipe.hip_architectures
        record = (_bounded_pc386_canonical(command, source, child_env)
                  if recipe.core_id == 'fes.pc386' and not selection_only else
                  subprocess.check_output(command, cwd=source, env=child_env))
        parsed = json.loads(record)
    except (OSError, subprocess.CalledProcessError, UnicodeDecodeError,
            json.JSONDecodeError) as error:
        if recipe.toolchain == "quartus":
            if recipe.board == "de25-nano":
                raise ValueError(f"cannot authenticate {recipe.core_id} DE25 Quartus inputs; set QUARTUS_ROOTDIR to the pinned Pro installation") from error
            extra = " and FES_PCXT_YOSYS" if recipe.lock_path == "cores/fes-pcxt/quartus.lock" else ""
            raise ValueError(f"cannot authenticate {recipe.core_id} Quartus inputs; set QUARTUS_ROOTDIR{extra} "
                             f"to the pinned installations ({recipe.lock_path})") from error
        raise ValueError(
            f"cannot derive authenticated {recipe.core_id} build inputs in {source}; "
            f"inspect the producer error and, if the shared toolchain slot is missing, run "
            + shlex.join(['env',
                'FES_TOOLCHAIN_CACHE_ROOT=' + str(recipe.cache_root),
                'FES_TOOLCHAIN_LOCKFILE=' + str(source / recipe.lock_path),
                'FES_TOOLCHAIN_GPU_ROUTER=' + selected_router,
                router_architecture,
                'make', '-C', str(source), 'toolchain']) +
            '; then rerun the original FES command. Use a shell without compiler/toolchain '
            'overrides: FES producer_environment sanitizes those overrides for producers, '
            'but this manual provisioning command inherits your shell environment.'
        ) from error
    canonical = json.dumps(parsed, ensure_ascii=False, separators=(",", ":"),
                           sort_keys=True).encode("utf-8") + b"\n"
    if artifact_cache.functional_key(record) is None or record != canonical:
        raise ValueError("producer returned a noncanonical format-2 build-input record")
    if selection_only and (parsed.get("parameters", {}).get("selection_only") is not True
                           or "identities" not in parsed):
        raise ValueError("selected producer does not support selection-only design records")
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
    members = {entry.name for entry in package.iterdir()}
    if members not in ({"manifest.toml", "core.rbf"}, {"manifest.toml", "core.rbf", "rom-map.json"}):
        raise ValueError("package input has unexpected members")
    for name in members:
        _sealed(package / name)
    program = r'''
import hashlib
import json
import sys
from pathlib import Path
import importlib
import importlib.util
board = sys.argv[4]
if board not in ('de10-nano', 'de25-nano'):
    raise ValueError('unsupported independently selected board')
namespace = 'boards.de25nano.package' if board == 'de25-nano' else 'scripts'
read_package = importlib.import_module(namespace + '.core_package').read_package
exporter = importlib.import_module(namespace + '.export_core_package')
build_identity, _decode_build_record = exporter.build_identity, exporter._decode_build_record
root = Path.cwd().resolve()
if sys.argv[3].startswith('cores.'):
    module_parts = sys.argv[3].split('.')
    expected = f"cores/{module_parts[1].replace('_', '-')}/tools/{module_parts[3]}.py" if len(module_parts) == 4 else None
    if sys.argv[5] != expected:
        raise ValueError('core-owned producer module and source path differ')
    current = root
    parts = Path(sys.argv[5]).parts
    for index, part in enumerate(parts):
        current = current / part
        if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
            raise ValueError('core-owned producer path must contain only regular source components')
    spec = importlib.util.spec_from_file_location(sys.argv[3], current)
    if spec is None or spec.loader is None:
        raise ValueError('cannot load core-owned producer')
    producer = importlib.util.module_from_spec(spec)
    sys.modules[sys.argv[3]] = producer
    spec.loader.exec_module(producer)
else:
    producer = importlib.import_module(sys.argv[3])
package_path = Path(sys.argv[1]).resolve()
record_path = Path(sys.argv[2]).resolve()
record = record_path.read_bytes()
record_fields = _decode_build_record(record)
if record_fields["parameters"].get("selection_only"):
    raise ValueError("selection-only record is not package build evidence")
package = read_package(package_path)
manifest = package.fields
if manifest.get("target", {}).get("platform") != sys.argv[4].replace("-", "_"):
    raise ValueError("cached package target differs from independently selected board")
if record_fields["format"] != 2:
    raise ValueError("normal FES packages require functional build record 2")
exporter.verify_record_source_at_revision(root, record)
build = manifest["build"]
from scripts.input_identity import manifest_toolchain
toolchain = manifest_toolchain(record_fields)
if package_path.name != package.package_id:
    raise ValueError("package directory name differs from package identity")
if (build["id"] != build_identity(record) or
    build["repository"] != record_fields["repository"] or
    build["revision"] != record_fields["revision"] or
    build["recipe_sha256"] != record_fields["recipe_sha256"] or
    build["toolchain"] != toolchain):
    raise ValueError("package descriptor differs from canonical build inputs")
inspected = {"manifest": manifest, "package_id": package.package_id,
             "manifest_sha256": hashlib.sha256(package.manifest_bytes).hexdigest(),
             "core_rbf_sha256": hashlib.sha256(package.payload_bytes).hexdigest()}
if manifest["format"] in (3, 4):
    inspected["rom_map_sha256"] = hashlib.sha256(package.rom_map_bytes).hexdigest()
print(json.dumps(inspected, sort_keys=True))
'''
    try:
        result = subprocess.run(
            [sys.executable, "-c", program, str(package), str(record_path), recipe.producer_module, recipe.board, recipe.producer_script],
            cwd=source, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        inspected = json.loads(result.stdout)
    except (OSError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) else ""
        raise ValueError(f"cached {recipe.core_id} package failed producer validation" +
                         (f": {detail}" if detail else "")) from error
    expected = {"manifest", "package_id", "manifest_sha256", "core_rbf_sha256"}
    if inspected.get("manifest", {}).get("format") in (3, 4):
        expected.add("rom_map_sha256")
    if set(inspected) != expected:
        raise ValueError("producer package inspection returned an unexpected result")
    return inspected


def _inspect_pc386_evidence(source, package, record_path, evidence_dir):
    """Recheck pc386's retained seal and lockstep on cache hits and misses."""
    _sealed(evidence_dir, directory=True)
    program = r'''
import sys
from pathlib import Path
root = Path.cwd()
current = root
parts = Path('cores/fes-pc386/tools/pc386_seal_evidence.py').parts
for index, part in enumerate(parts):
    current = current / part
    if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
        raise ValueError('pc386 evidence reader must be a regular, non-symlinked source file')
sys.path.insert(0, str(current.parent))
from pc386_seal_evidence import validate
print(validate(Path.cwd(), Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])))
'''
    try:
        result = subprocess.run([sys.executable, '-c', program, str(package), str(record_path), str(evidence_dir)],
                                cwd=source, text=True, capture_output=True, check=True)
    except (OSError, subprocess.CalledProcessError) as error:
        detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) else ''
        raise ValueError('pc386 seal evidence failed validation' + (': ' + detail if detail else '')) from error
    digest_value = result.stdout.strip()
    if HEX64.fullmatch(digest_value) is None:
        raise ValueError('pc386 seal evidence returned an invalid receipt digest')
    return digest_value


def _pc386_reuse(source, package, record_path, evidence_dir, selected_record, *, run_lockstep=False,
                 recipe=None, env=None):
    """Ask the selected tree's evidence reader whether a sealed pc386 package may stand for it.

    With run_lockstep, run and record the fresh full lockstep an evidence-only
    change needs (#1984 S3). Neither path starts Quartus.
    """
    program = r'''
import json
import sys
from pathlib import Path
root = Path.cwd()
current = root
parts = Path('cores/fes-pc386/tools/pc386_seal_evidence.py').parts
for index, part in enumerate(parts):
    current = current / part
    if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
        raise ValueError('pc386 evidence reader must be a regular, non-symlinked source file')
sys.path.insert(0, str(current.parent))
import pc386_seal_evidence as evidence
package, record, directory, selected = map(Path, sys.argv[1:5])
store = root / 'build/packages'
if sys.argv[5] == 'run':
    status = evidence.run_fresh_lockstep(root, package, record, directory, selected.read_bytes(), store,
                                         cache_root=Path(sys.argv[6]))
else:
    status = evidence.reuse_gate(root, package, record, directory, selected.read_bytes(), store)
print(json.dumps(status, sort_keys=True))
'''
    with tempfile.TemporaryDirectory(prefix='fes-pc386-selected-') as private:
        selected = Path(private) / 'selected-build-inputs.json'
        selected.write_bytes(selected_record)
        mode = ['run', str(recipe.cache_root)] if run_lockstep else ['check']
        try:
            result = subprocess.run([sys.executable, '-c', program, str(package), str(record_path),
                                     str(evidence_dir), str(selected), *mode], cwd=source, text=True,
                                    capture_output=True, check=True,
                                    **({'env': package_build_environment(env, recipe=recipe)} if run_lockstep else {}))
        except (OSError, subprocess.CalledProcessError) as error:
            detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) else ''
            action = 'fresh full lockstep failed' if run_lockstep else 'reuse check failed'
            raise ValueError(f'pc386 evidence-only reuse refused: {action}' + (': ' + detail if detail else '')) from error
    status = json.loads(result.stdout)
    if (status.get('reuse') == 'fresh-lockstep'
            and HEX64.fullmatch(str(status.get('fresh_lockstep_sha256'))) is None):
        raise ValueError('pc386 reuse check returned an invalid fresh lockstep digest')
    if status.get('reuse') not in ('unchanged', 'refused', 'needs-fresh-lockstep', 'fresh-lockstep'):
        raise ValueError('pc386 reuse check returned an unknown result')
    return status


def _discard_producer_report(path):
    """Remove a producer miss report without following a symlink."""
    path = Path(path)
    try:
        path.lstat()
    except FileNotFoundError:
        return
    path.unlink()


def _build_package(source, recipe=None, env=None):
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    report_path = Path(source) / "build" / optional_cores.PRODUCER_REPORT_NAME
    _discard_producer_report(report_path)
    try:
        subprocess.run(
            [sys.executable, recipe.producer_script, "--root", str(source),
             "--package-output", str(Path(source) / "build/packages"),
             "--cache-root", str(recipe.cache_root),
             "--identity-version", "2", *recipe.producer_arguments,
             *_de25_license_arguments(env, recipe)],
            cwd=source, check=True, env=package_build_environment(env, recipe=recipe))
    except (OSError, subprocess.CalledProcessError) as error:
        report = optional_cores.read_producer_report(report_path)
        _discard_producer_report(report_path)
        if report is not None and report.get("core_id") not in (None, recipe.core_id):
            report = None
        failure = optional_cores.OptionalBuildFailure(f"selected {recipe.core_id} recipe failed")
        failure.unavailable = report
        raise failure from error
    _discard_producer_report(report_path)


def _build_fes_pong(source, env=None):
    _build_package(source, recipe=recipe_for("fes.pong"), env=env)


def _matching_package_candidates(source, record, recipe=None, *, store_override=None, pending=None):
    """Validated packages for record. A pc386 package that needs a fresh
    lockstep before reuse is left out and appended to pending instead."""
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
    local_pc386 = store_override is None and recipe.core_id == 'fes.pc386'
    if local_pc386:
        entries = store / '.pc386-entries'
        if not entries.exists() and not entries.is_symlink():
            return []
        _plain_directory(entries, 'pc386 local entry store')
        sidecars = sorted(entries.glob('*/build-inputs.json'))
    else:
        sidecars = sorted(store.glob("*.build-inputs.json")) if store_override is None else sorted(store.glob("*/build-inputs.json"))
    for sidecar in sidecars:
        if store_override is not None or local_pc386:
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
            if artifact_cache.functional_key(candidate_record) != artifact_cache.functional_key(record):
                continue
        candidate_fields = json.loads(candidate_record)
        if store_override is None and not local_pc386:
            identity, separator, evidence_id = sidecar.name.removesuffix('.build-inputs.json').partition('.')
            if separator and evidence_id != candidate_fields.get('identities', {}).get('record'):
                raise ValueError('package evidence filename differs from its record digest')
        elif 'identities' in candidate_fields:
            if sidecar.parent.name != candidate_fields['identities']['record']:
                raise ValueError('cache evidence directory differs from its record digest')
            ignored = {'build-inputs.json'} | ({'evidence'} if recipe.core_id == 'fes.pc386' else set())
            members = [path for path in sidecar.parent.iterdir() if path.name not in ignored]
            if len(members) != 1:
                raise ValueError('cache evidence must name exactly one package')
            identity = members[0].name
        else:
            identity = sidecar.parent.name
        if HEX64.fullmatch(identity) is None:
            raise ValueError("matching package evidence has an invalid package ID filename")
        package = store / identity if store_override is None and not local_pc386 else sidecar.parent / identity
        evidence = sidecar.parent / 'evidence' if recipe.core_id == 'fes.pc386' else None
        _plain_directory(package, "candidate")
        inspected = _inspect_package_candidate(source, package, sidecar, recipe=recipe)
        if inspected.get("package_id") != identity:
            raise ValueError("matching package evidence differs from inspected package identity")
        if recipe.core_id == 'fes.pc386':
            inspected['pc386_evidence'] = {
                'directory': evidence,
                'receipt_sha256': _inspect_pc386_evidence(source, package, sidecar, evidence),
            }
            if candidate_record != record:
                reuse = _pc386_reuse(source, package, sidecar, evidence, record)
                if reuse['reuse'] == 'refused':
                    print(f"pc386 sealed package {identity} not reused: {reuse['reason']}", file=sys.stderr)
                    continue
                if reuse['reuse'] == 'needs-fresh-lockstep':
                    if pending is not None:
                        pending.append((package, sidecar, evidence))
                    continue
                inspected['pc386_evidence']['reuse'] = reuse
        matches.append((package, sidecar, inspected))
    return _rank_package_candidates(matches, record)


def _rank_package_candidates(matches, record):
    # Prefer an exact current build when available. Otherwise retain a stable
    # already-qualified artifact rather than requiring a compiler upgrade.
    selected_fields = json.loads(record)
    selected = selected_fields.get('identities', {})
    def rank(item):
        original = item[1].read_bytes()
        built_fields = json.loads(original)
        built = built_fields.get('identities', {})
        pin_mismatch = (artifact_cache.identity.toolchain_selection(built_fields) !=
                        artifact_cache.identity.toolchain_selection(selected_fields))
        return (original != record, pin_mismatch,
                built.get('toolchain') != selected.get('toolchain'), item[2]['package_id'])
    return sorted(matches, key=rank)


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
    # Checked before any producer runs or anything is published to ARTIFACT_CACHE_ROOT.
    require_private_audit_cache(os.environ if env is None else env)
    produce = {} if env is None else {"env": env}
    record = canonical_package_record(source, recipe=recipe, selection_only=not force, **produce)
    if force:
        _build_package(source, recipe=recipe, **produce)
    def matching(pending=None):
        found = _matching_package_candidates(source, record, recipe=recipe, pending=pending)
        if not force and (not found or 'identities' in json.loads(record)):
            found += _matching_package_candidates(source, record, recipe=recipe, pending=pending,
                store_override=artifact_cache.store_for(ARTIFACT_CACHE_ROOT, record))
        return found
    pending = []
    candidates = matching(pending)
    if not candidates and pending and not force:
        # Only evidence changed since a pc386 seal (#1984 S3): reuse it only
        # after a fresh full lockstep of this tree passes. A failure refuses;
        # it never falls back to the old package or to a rebuild.
        package, original_record_path, evidence_dir = pending[0]
        _pc386_reuse(source, package, original_record_path, evidence_dir, record, run_lockstep=True,
                     recipe=recipe, env=env)
        candidates = matching()
        if not candidates:
            raise ValueError('pc386 fresh lockstep was recorded but the sealed package is still not reusable')
    if not candidates and not force:
        # A cache miss adopts the selected pin deliberately. Authentication is
        # deferred until here, so reusing a design does not install a new tool.
        authenticated = canonical_package_record(source, recipe=recipe, **produce)
        if artifact_cache.functional_key(authenticated) != artifact_cache.functional_key(record):
            raise ValueError("design selection changed while authenticating the producer")
        record = authenticated
        _build_package(source, recipe=recipe, **produce)
        candidates = _matching_package_candidates(source, record, recipe=recipe)
    if not candidates:
        raise ValueError(f"selected recipe did not produce a matching {recipe.core_id} package")
    if 'identities' in json.loads(record):
        candidates = _rank_package_candidates(candidates, record)
        executions = {}
        for item in candidates:
            built = json.loads(item[1].read_bytes())
            execution = built.get('identities', {}).get('toolchain')
            if execution is not None:
                outcome = (item[2]['core_rbf_sha256'], item[2].get('rom_map_sha256'))
                if execution in executions and executions[execution] != outcome:
                    raise ValueError('different payloads claim the same design and toolchain identity')
                executions[execution] = outcome
    if force:
        # A forced update must select the exact freshly captured execution,
        # never a previous toolchain's artifact with the same design ID.
        candidates = [item for item in candidates if item[1].read_bytes() == record]
        if not candidates:
            raise ValueError("forced build did not produce the selected execution record")
    if 'identities' not in json.loads(record) and len(candidates) > 1:
        # Historical combined-key records promised a unique routed result.
        if len({(item[2]["core_rbf_sha256"], item[2].get("rom_map_sha256")) for item in candidates}) != 1:
            raise ValueError("multiple package IDs match the canonical format-2 build inputs")
    candidates = candidates[:1]
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
    if manifest.get("format") in (3, 4):
        rom_map_sha256 = digest(package / "rom-map.json")
        if rom_map_sha256 != inspected.get("rom_map_sha256"):
            raise ValueError("selected package members changed after inspection")
        inputs["rom_map_sha256"] = rom_map_sha256
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
    if 'identities' in selected:
        def toolchain_selection(fields):
            return {path: value for path, value in fields['source_inputs'].items()
                    if artifact_cache.identity.input_role(path) == 'toolchain'}
        inputs['source_selection'].update({
            'identity_policy': original['parameters']['identity_policy'],
            'design_id': original['identities']['design'][:32],
            'built_toolchain_id': original['identities']['toolchain'],
            'built_record_id': original['identities']['record'],
            'built_with': original['tools'],
            'built_pins': json.loads(original['parameters'].get('toolchain_pins', '{}')),
            'selected_pins': json.loads(selected['parameters'].get('toolchain_pins', '{}')),
            'built_toolchain_inputs': toolchain_selection(original),
            'selected_toolchain_inputs': toolchain_selection(selected),
            'toolchain_update_available': (artifact_cache.identity.toolchain_selection(original) !=
                                           artifact_cache.identity.toolchain_selection(selected)),
        })
    require_private_audit_cache(os.environ if env is None else env)
    if recipe.core_id == 'fes.pc386':
        evidence_info = inspected['pc386_evidence']
        evidence_dir = evidence_info['directory']
        _sealed(evidence_dir, directory=True)
        evidence = {}
        for item in evidence_dir.iterdir():
            _sealed(item)
            evidence[item.name] = item.read_bytes()
        def validate_staged(staged_package, staged_record, staged_evidence):
            observed = _inspect_pc386_evidence(source, staged_package, staged_record, staged_evidence)
            if observed != evidence_info['receipt_sha256']:
                raise ValueError('pc386 evidence changed before cache publication')
        artifact_cache.publish(ARTIFACT_CACHE_ROOT, original_record_path, package,
                               evidence=evidence, validate=validate_staged)
        inputs['source_selection']['pc386_evidence_sha256'] = evidence_info['receipt_sha256']
        reuse = evidence_info.get('reuse')
        if reuse is not None and reuse['reuse'] == 'fresh-lockstep':
            inputs['source_selection']['pc386_evidence_drift'] = reuse['evidence_drift']
            inputs['source_selection']['pc386_fresh_lockstep_sha256'] = reuse['fresh_lockstep_sha256']
    else:
        artifact_cache.publish(ARTIFACT_CACHE_ROOT, original_record_path, package)
    receipt = json.dumps(inputs["source_selection"], sort_keys=True, indent=2).encode() + b"\n"
    _publish_selection(Path(selection_path).with_suffix(".provenance.json"), receipt)
    _publish_selection(selection_path, encoded)
    return {"directory": package, "selection_path": Path(selection_path),
            "inputs": inputs}
