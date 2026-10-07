"""Resolve independently sealed factory video parts for the factory package set.

Compiler work remains in the selected misteross producer. This module keeps its
original evidence in the host cache and exports only a closed archive inventory.
"""
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile

if __package__:
    from . import recipes
else:
    import recipes

PROFILES = ("direct", "scanlines")
MAX_ARCHIVE_BYTES = 32 << 20
MAX_EVIDENCE_BYTES = 128 << 20
MAX_INDEX_BYTES = 64 << 10
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
SHELL_MEMBERS = ("manifest.toml", "core.rbf", "routed.json", "socket.qsf",
                 "synth.json", "timing.json", "nextpnr.log", "build-inputs.json",
                 "build-summary.json")
PART_MEMBERS = ("build-summary.json", "timing.json", "cart.json",
                "cart-routed.json", "route.log", "cram-diff.json")
VIDEO_OUTPUTS = {"fes.coleco-video.socket/1": "build/fes-coleco-video",
                 "fes.coleco-native-video.socket/1": "build/fes-coleco-native-video"}
ST_MAP = "fes.atari-st-video.socket/1"
VIDEO_OUTPUTS[ST_MAP] = "build/fes-atari-st-oss"
ST_SHELL_MEMBERS = (*SHELL_MEMBERS, "rom-map.json")
NATIVE_MAP = "fes.coleco-native-video.socket/1"
VIDEO_INTERFACES = {"fes.fabric.video.raster-rgb888": "fes.coleco-video.socket/1",
                    "fes.fabric.video.native-pixels": NATIVE_MAP}


class MissingVideoShell(ValueError):
    """The resolved package has no exact, authenticated frozen shell companion."""


def video_shell_profile(fields):
    """Admit the closed descriptor markers used by catalog and core-dev shells."""
    interfaces = fields.get("interfaces", [])
    if not isinstance(interfaces, list) or any(not isinstance(row, dict)
            or not isinstance(row.get("id"), str) for row in interfaces):
        raise ValueError("video shell interfaces must be an array of tables")
    markers = [row for row in interfaces if row.get("id") in VIDEO_INTERFACES]
    if not markers:
        return None
    core, abi = fields.get("core"), fields.get("abi")
    st = isinstance(core, dict) and core.get("id") == "fes.atari-st"
    expected_format, expected_abi = (3, "fes.computer") if st else (2, "fes.application")
    if (len(markers) != 1 or type(fields.get("format")) is not int or fields["format"] != expected_format
            or not isinstance(core, dict) or core.get("id") not in ("fes.coleco", "fes.atari-st")
            or abi != {"id": expected_abi, "major": 1, "minor": 0}
            or type(abi.get("major")) is not int or type(abi.get("minor")) is not int
            or (st and (markers[0]["id"] != "fes.fabric.video.raster-rgb888"
                or not isinstance(fields.get("rom"), dict)
                or fields["rom"].get("role") != "firmware"
                or {"id": "fes.expansion.atari-st-bus", "major": 1, "minor": 0,
                    "required": False} not in interfaces))):
        raise ValueError("factory video requires an exact Coleco application or ST firmware shell profile")
    marker = markers[0]
    if (marker != {"id": marker["id"], "major": 1, "minor": 0, "required": False}
            or type(marker.get("major")) is not int or type(marker.get("minor")) is not int
            or marker.get("required") is not False):
        raise ValueError("factory video requires an exact optional version-1.0 profile marker")
    return ST_MAP if st else VIDEO_INTERFACES[marker["id"]]


def canonical(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True,
                       separators=(",", ":"), allow_nan=False) + "\n").encode()


def _sha(data):
    return hashlib.sha256(data).hexdigest()


def _plain(path, *, directory=False, sealed=False):
    path = Path(path).absolute()
    for parent in (*reversed(path.parents), path):
        metadata = parent.lstat()
        expected = stat.S_ISDIR if parent != path or directory else stat.S_ISREG
        if not expected(metadata.st_mode):
            raise ValueError(f"video artifact must not be linked or special: {parent}")
    if sealed and stat.S_IMODE(metadata.st_mode) != (0o555 if directory else 0o444):
        raise ValueError(f"video artifact is not sealed with the required mode: {path}")
    return path


def _read(path, *, limit=MAX_EVIDENCE_BYTES, sealed=False):
    path = _plain(path, sealed=sealed)
    with path.open("rb") as stream:
        before = os.fstat(stream.fileno())
        if not 0 < before.st_size <= limit:
            raise ValueError(f"video artifact is not a bounded nonempty file: {path}")
        data = stream.read(limit + 1)
        after = os.fstat(stream.fileno())
    if (len(data) != before.st_size or before.st_mtime_ns != after.st_mtime_ns
            or before.st_ctime_ns != after.st_ctime_ns):
        raise ValueError(f"video artifact changed while reading: {path}")
    return data


def _closed(root, members, *, sealed=False):
    _plain(root, directory=True, sealed=sealed)
    if {entry.name for entry in root.iterdir()} != set(members):
        raise ValueError(f"video artifact has unexpected members: {root}")
    return {name: _read(root / name, sealed=sealed,
                        limit=MAX_ARCHIVE_BYTES if name == "archive.tar" else MAX_EVIDENCE_BYTES)
            for name in members}


def read_index(directory, *, sealed=True):
    """Validate the canonical closed factory inventory without compiler tools."""
    directory = _plain(directory, directory=True, sealed=sealed)
    encoded = _read(directory / "index.json", limit=MAX_INDEX_BYTES, sealed=sealed)
    try:
        value = json.loads(encoded)
        if canonical(value) != encoded:
            raise ValueError("video index must be canonical JSON")
    except (UnicodeDecodeError, json.JSONDecodeError, TypeError) as error:
        raise ValueError("video index must be canonical JSON") from error
    if (not isinstance(value, dict) or set(value) != {"version", "packages"}
            or type(value["version"]) is not int or value["version"] != 1
            or not isinstance(value["packages"], list)
            or not 1 <= len(value["packages"]) <= 32):
        raise ValueError("video index requires the exact version-1 schema")
    identities, files, previous_package = set(), {"index.json"}, ""
    for package in value["packages"]:
        if not isinstance(package, dict) or set(package) != {"package_id", "parts"}:
            raise ValueError("video package index has unexpected fields")
        package_id = package["package_id"]
        if (not isinstance(package_id, str) or not HEX64.fullmatch(package_id)
                or package_id <= previous_package):
            raise ValueError("video package identities must be unique and sorted")
        previous_package = package_id
        if not isinstance(package["parts"], list) or len(package["parts"]) != len(PROFILES):
            raise ValueError("video package requires direct and scanlines parts")
        expected_archives, previous_profile = set(), ""
        package_dir = _plain(directory / package_id, directory=True, sealed=sealed)
        for part in package["parts"]:
            if not isinstance(part, dict) or set(part) != {
                    "profile", "part_id", "archive_path", "archive_sha256", "archive_size"}:
                raise ValueError("video part index has unexpected fields")
            profile, part_id = part["profile"], part["part_id"]
            if not isinstance(profile, str) or profile not in PROFILES or profile <= previous_profile:
                raise ValueError("video profiles must be supported, unique and sorted")
            previous_profile = profile
            if (not isinstance(part_id, str) or not HEX64.fullmatch(part_id)
                    or part_id in identities):
                raise ValueError("video part identities must be unique SHA256 values")
            identities.add(part_id)
            relative = f"{package_id}/{part_id}.tar"
            if (part["archive_path"] != relative or not isinstance(part["archive_sha256"], str)
                    or not HEX64.fullmatch(part["archive_sha256"])
                    or type(part["archive_size"]) is not int
                    or not 1 <= part["archive_size"] <= MAX_ARCHIVE_BYTES):
                raise ValueError("video archive binding or size is invalid")
            payload = _read(directory / relative, limit=MAX_ARCHIVE_BYTES, sealed=sealed)
            if len(payload) != part["archive_size"] or _sha(payload) != part["archive_sha256"]:
                raise ValueError("video archive digest or size differs from index")
            expected_archives.add(part_id + ".tar")
            files.add(relative)
        if {entry.name for entry in package_dir.iterdir()} != expected_archives:
            raise ValueError("video package tree has unexpected members")
    expected_root = {"index.json", *(package["package_id"] for package in value["packages"])}
    if {entry.name for entry in directory.iterdir()} != expected_root:
        raise ValueError("video index tree has unexpected members")
    return value


def _remove(root):
    if not root.exists():
        return
    for path in root.rglob("*"):
        if not path.is_symlink():
            path.chmod(0o755 if path.is_dir() else 0o644)
    root.chmod(0o755)
    shutil.rmtree(root)


def _publish(destination, files):
    """Publish a complete immutable tree in one rename; never replace a tree."""
    destination = Path(destination).absolute()
    destination.parent.mkdir(parents=True, exist_ok=True)
    _plain(destination.parent, directory=True)
    if destination.exists() or destination.is_symlink():
        found = {}
        directories = set()
        _plain(destination, directory=True, sealed=True)
        for path in destination.rglob("*"):
            _plain(path, directory=path.is_dir(), sealed=True)
            if path.is_file():
                found[path.relative_to(destination).as_posix()] = _read(path, sealed=True)
            else:
                directories.add(path.relative_to(destination).as_posix())
        expected_dirs = {parent.as_posix() for name in files for parent in Path(name).parents
                         if parent != Path(".")}
        if found != files or directories != expected_dirs:
            raise ValueError(f"existing immutable video artifacts differ: {destination}")
        return destination
    temporary = Path(tempfile.mkdtemp(prefix=".publish-video-", dir=destination.parent))
    try:
        for name, data in files.items():
            relative = Path(name)
            if relative.is_absolute() or ".." in relative.parts or relative.as_posix() != name:
                raise ValueError("invalid relative video publication path")
            path = temporary / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            path.chmod(0o444)
        for path in temporary.rglob("*"):
            if path.is_dir():
                path.chmod(0o555)
        temporary.chmod(0o555)
        temporary.rename(destination)
        return destination
    finally:
        _remove(temporary)


@contextmanager
def _locked(root):
    root.mkdir(parents=True, exist_ok=True)
    _plain(root, directory=True)
    lock = root / ".resolve.lock"
    if lock.exists() or lock.is_symlink():
        _plain(lock)
    with lock.open("a+b") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        yield


# Run in isolation: FES and misteross both contain a Python package named
# scripts, so selected producer readers must not share the parent's imports.
_INSPECT = r'''
import hashlib, io, json, re, sys, tarfile, tempfile
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from scripts import build_video_part as producer, build_fes_coleco_socket_v2 as shell_producer
from scripts import native_video_parts, native_video_clock, coleco_expansion
from scripts import build_atari_st_video_part, build_fes_atari_st_oss, rom_map
from scripts.core_package import read_package
from scripts.export_core_package import source_input_closure, POLICY, verify_record_source_at_revision, build_identity
from scripts.functional_execution import execution_environment, execution_inputs, source_roots_for_inputs
from scripts.fes_build_common import _cell_counts, validate_timing_resources
from scripts.cyclonev_rbf import rbf_load, CramRect, classify_cram_diff, overlay_cram
root = Path(sys.argv[1]); mode = sys.argv[2]; args = json.load(sys.stdin)
def sha(data): return hashlib.sha256(data).hexdigest()
def encode(value): return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False).encode()
def require(condition, message):
    if not condition: raise ValueError(message)
def functional(record):
    fields = json.loads(record)
    return sha(b'fes-functional-inputs-v2\0' + encode({k:v for k,v in fields.items() if k not in ('repository','revision','source_path')}))
def bounded(path, maximum=128<<20):
    require(not path.is_symlink() and path.is_file() and 0 < path.stat().st_size <= maximum, 'unbounded producer evidence: '+str(path))
    data=path.read_bytes()
    if args.get('directory') and path.parent==Path(args['directory']) and path.name in args.get('evidence_sha256',{}):
        require(sha(data)==args['evidence_sha256'][path.name], 'video evidence changed after snapshot: '+path.name)
    return data
package = read_package(Path(args['package']))
st = package.fields['core']['id']=='fes.atari-st'
if st: producer, shell_producer = build_atari_st_video_part, build_fes_atari_st_oss
layout = producer.package_profile(package)
native = layout is native_video_parts
strict = native or st
options = {} if st else ({'native_video':True} if native else {'video_socket':True})
clock_producer = producer.slot_recipe if st else producer.sgm
factory = shell_producer if st else shell_producer.factory
shell_members = ('manifest.toml','core.rbf','routed.json','socket.qsf') + (('rom-map.json',) if st else ())
if mode == 'canonical':
    require(args['producer_options']==options, 'resolved video profile differs from selected factory recipe')
    _, revision = shell_producer._require_clean_source(root, **options)
    authenticate = shell_producer._authenticate_atari_st_tools if st else shell_producer.authenticate_tools
    tools = authenticate(root, Path(args['toolchain_cache']))
    roots = source_roots_for_inputs(producer.NATIVE_INPUTS if native else producer.INPUTS)
    with tempfile.TemporaryDirectory(prefix='fes-video-canonical-') as home:
        paths = {name:tool.path for name,tool in tools.items()}
        execution = execution_inputs(paths, execution_environment(Path(home), paths), 0)
    result = {'inputs':source_input_closure(root, roots, policy=POLICY),
              'source_roots':roots, 'source_closure_policy':POLICY,
              'tools':{name:tool.identity for name,tool in tools.items()},
              'execution':execution, 'revision':revision, 'map':layout.MAP}
else:
    shell = Path(args['shell'])
    current = args['current']
    require(current['map']==layout.MAP, 'video inputs target a different shell profile')
    require(not st or bounded(shell/'rom-map.json')==package.rom_map_bytes, 'frozen firmware map differs from resolved package')
    require(bounded(shell/'manifest.toml')==package.manifest_bytes and bounded(shell/'core.rbf',32<<20)==package.payload_bytes, 'frozen video shell differs from resolved package')
    if mode == 'shell':
        record = bounded(shell/'build-inputs.json'); fields = verify_record_source_at_revision(root, record)
        require(functional(record)==args['functional_inputs_sha256'], 'frozen shell functional inputs differ from selected package')
        expected = (shell_producer.create_build_record(root, fields['repository'], fields['revision'], current['tools'], execution=current['execution']) if st else
                    shell_producer.create_build_record(root, fields['repository'], fields['revision'], current['tools'], current['execution'], **options))
        require(functional(record)==functional(expected), 'frozen video shell differs from current recipe')
        summary = json.loads(bounded(shell/'build-summary.json'))
        if st:
            tools = shell_producer._authenticate_atari_st_tools(root, Path(args['toolchain_cache']))
            database = rom_map.read_database(tools['mistral'].path.parents[2]/'src/mistral', shell_producer.ROM_DATABASE_SHA256)
            evidence = shell_producer.validate_build_evidence(shell, ram_database=database)
            mapping, map_evidence = rom_map.build_rom_map(database, package.payload_bytes,
                routed=json.loads(bounded(shell/'routed.json')), lane_rows=shell_producer.FIRMWARE_LANE_ROWS, expected_async_read=0)
            shell_producer.check_firmware_outside_sockets(mapping)
            require((encode(mapping)+b'\n')==package.rom_map_bytes and summary['rom_map']==map_evidence, 'frozen firmware map differs from producer evidence')
            require(bounded(shell/'socket.qsf') == shell_producer.socket_qsf((root/shell_producer.QSF).read_text()).encode(), 'frozen shell constraints differ from current recipe')
            for key,value in evidence.items():
                require(summary[key]==value if key!='route' else all(summary[key].get(k)==v for k,v in value.items()), 'frozen shell '+key+' evidence differs')
            require(summary['rom']==package.fields['rom'], 'frozen shell firmware receipt differs')
            manifest = shell_producer._manifest(record,summary,fields['repository'],fields['revision'],fields['tools'])
        else:
            coleco_expansion.validate_routed_shell(shell/'routed.json',version=2)
            layout.validate_boundary(json.loads(bounded(shell/'routed.json'))['modules']['top'],routed=True)
            require(bounded(shell/'socket.qsf') == layout.shell_qsf(coleco_expansion.shell_qsf((root/shell_producer.factory.QSF).read_text(),version=2)).encode(), 'frozen shell constraints differ from current recipe')
            evidence = shell_producer.factory.validate_build_evidence(shell,root)
            for key in ('status','timing','resources','synthesis_cells','rbf'):
                require(summary[key]==evidence[key], 'frozen shell '+key+' evidence differs')
            manifest = shell_producer.manifest(record,summary,fields['repository'],fields['revision'],fields['tools'],**options)
        require(summary['build_id']==build_identity(record) and summary['tools']==current['tools'] and summary['execution']==current['execution'], 'frozen shell summary differs from authenticated inputs')
        require(manifest==package.manifest_bytes, 'frozen shell manifest differs from producer evidence')
        result={'package_id':package.package_id,'build_record_sha256':sha(record)}
    elif mode == 'part':
        directory=Path(args['directory']); variant=args['profile']; archive=Path(args['archive'])
        require(variant in ('direct','scanlines'), 'unknown video profile')
        raw=bounded(archive,32<<20)
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:') as tar:
            members=tar.getmembers()
            require(len(members)==2 and {m.name for m in members}=={'manifest.json','cart.rbf'} and all(m.isfile() and not m.pax_headers and 0<m.size<=32<<20 for m in members), 'video archive requires only regular manifest.json and cart.rbf')
            require(next(m.size for m in members if m.name=='manifest.json')<=65536, 'video manifest exceeds its bound')
            encoded=tar.extractfile('manifest.json').read(); cart=tar.extractfile('cart.rbf').read()
        manifest=json.loads(encoded)
        require(encode(manifest)==encoded, 'video manifest must be canonical JSON')
        require(set(manifest)=={'cart_sha256','cart_size','device','format','map','recipe_sha256','revision','shell_build_id','shell_package_id','shell_sha256','slot','slot_major','slot_minor'}, 'unexpected video manifest fields')
        require(all(type(manifest[name]) is int for name in ('cart_size','format','slot_major','slot_minor')), 'video manifest integer fields must be integers')
        require(manifest['format']==1 and manifest['device']=='5CSEBA6U23I7' and manifest['map']==layout.MAP and manifest['slot']==layout.INTERFACE and manifest['slot_major']==1 and manifest['slot_minor']==0, 'video manifest has incompatible role or map')
        require(manifest['cart_size']==len(cart) and manifest['cart_sha256']==sha(cart), 'video cart digest or size differs')
        require(manifest['shell_package_id']==package.package_id and manifest['shell_build_id']==package.fields['build']['id'] and manifest['shell_sha256']==sha(package.payload_bytes), 'video part targets a different shell')
        roots=current['source_roots']
        clocks=clock_producer.cart_clock_constraints(root)
        recipe={key:current[key] for key in ('inputs','source_roots','source_closure_policy','tools','execution')}
        recipe.update(shell={name:sha(bounded(shell/name)) for name in shell_members},variant=variant,slot_clock=layout.CLOCK,map=layout.MAP,cram_region=list(layout.CRAM),required_clocks_mhz=clock_producer.REQUIRED_CLOCKS_MHZ,clock_constraints_sha256=sha(clocks))
        if st: recipe['placer_seed']=producer.PLACER_SEED
        recipe_sha=sha(encode(recipe)); part_id=sha(b'fes-expansion-v1\0'+encoded)
        require(manifest['recipe_sha256']==recipe_sha, 'video part differs from current build recipe')
        # Authenticate original source provenance without rewriting its revision.
        shell_fields=json.loads(bounded(shell/'build-inputs.json'))
        history=dict(shell_fields,revision=manifest['revision'],source_roots=roots,source_inputs=current['inputs'])
        history['recipe']='scripts/build_atari_st_video_part.py' if st else 'scripts/build_video_part.py'; history['recipe_sha256']=current['inputs'][history['recipe']]
        verify_record_source_at_revision(root,encode(history)+b'\n')
        summary=json.loads(bounded(directory/'build-summary.json'))
        require(summary['recipe']==recipe and summary['part_id']==part_id and summary['manifest']==manifest, 'video producer summary differs from sealed part')
        log=bounded(directory/'route.log').decode()
        require('Info: Program finished normally.' in log and 'unrouted' not in log.lower() and not re.search(r'^\s*(?:ERROR|FATAL)\b',log,re.M|re.I), 'video route did not complete')
        backend=factory._require_gpu_backend(log)
        timing=json.loads(bounded(directory/'timing.json')); measured=clock_producer.validate_cart_timing(timing)
        resources=validate_timing_resources(timing.get('utilization'),factory.ORDINARY_RESOURCES|set(factory.REQUIRED_RESOURCES)|factory.FORBIDDEN_RESOURCES|factory.REQUIRED_ZERO_RESOURCES)
        if not st: measured={name:factory._frequency_row(timing['fmax'],freq,name,name) for name,freq in clock_producer.REQUIRED_CLOCKS_MHZ.items()}
        prepared=bounded(directory/'cart.json',32<<20)
        if native:
            synthesized=bounded(directory/'cart-synth.json',32<<20)
            normalized, proof=native_video_clock.normalize_native_clock_inputs(json.loads(synthesized,object_pairs_hook=native_video_clock._object))
            expected=(json.dumps(normalized,sort_keys=True,separators=(',',':'))+'\n').encode()
            require(prepared==expected, 'native prepared cart differs from proven clock normalization')
            proof.update(synth_sha256=sha(synthesized),prepared_sha256=sha(prepared))
            require(summary.get('native_clock_boundary')==proof, 'native clock normalization evidence differs')
        counts=_cell_counts(json.loads(prepared))
        require(not any(counts.get(name,0) for name in factory.FORBIDDEN_RESOURCES|set(factory.REQUIRED_RESOURCES)), 'video part owns a forbidden resource')
        require(not native or sum(counts.get(name,0) for name in ('MISTRAL_M10K','MISTRAL_M10K_TDP'))==48, 'native part must own exactly 48 M10K frame-buffer blocks')
        require(not st or not any(counts.get(name,0) for name in ('MISTRAL_M10K','MISTRAL_M10K_TDP')), 'ST raster part must not allocate RAM')
        pins=producer.validate_clocks(json.loads(bounded(directory/'cart-routed.json')),layout=layout)
        require(not (native or variant=='scanlines') or pins>0, 'video state did not survive synthesis')
        require(summary['checked_clock_pins']==pins and summary['resources']==resources and summary['synthesis_cells']==counts and summary['timing']==json.loads(json.dumps(measured)) and summary['route']=={'complete':True,'gpu_backend':backend}, 'video timing, resource or clock evidence differs')
        base,placed=rbf_load(package.payload_bytes),rbf_load(cart)
        require(base.header==placed.header, 'video part changes configuration header')
        changes=classify_cram_diff(base,placed,CramRect(*layout.CRAM),include_outside_coordinates=True,ignore_ecc_columns=not strict)
        require(changes['bits_outside_slot']==0 and summary['cram_diff']==changes, 'video part changes outside its CRAM region or containment evidence differs')
        require(summary.get('cram_policy')==('strict-rectangle-v1' if strict else 'legacy-columns-v1'), 'video containment policy differs')
        preview_matches=overlay_cram(base,placed,CramRect(*layout.CRAM)).cram==placed.cram
        require(summary.get('preview_matches_routed_cram') is preview_matches and (not strict or preview_matches), 'video preview differs from routed CRAM')
        report=json.loads(bounded(directory/'cram-diff.json'))
        require(report=={'archive_published':True,'cart_sha256':sha(cart),'cram_diff':changes,'cram_region':list(layout.CRAM),'map':layout.MAP,'part_id':part_id,'route_contract':'passed'}, 'video containment publication evidence differs')
        result={'profile':variant,'part_id':part_id,'recipe_sha256':recipe_sha,'revision':manifest['revision'],'archive_sha256':sha(raw),'archive_size':len(raw),'build_summary_sha256':sha(bounded(directory/'build-summary.json')),'cram_report_sha256':sha(bounded(directory/'cram-diff.json'))}
    else: raise ValueError('unknown video inspection mode')
print(json.dumps(result,sort_keys=True,separators=(',',':')))
'''


def _inspect(source, mode, arguments, recipe, env):
    result = subprocess.run([sys.executable, "-I", "-B", "-c", _INSPECT,
                             str(source), mode], input=canonical(arguments),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            cwd=source, env=recipes.producer_environment(env, recipe=recipe))
    if result.returncode:
        raise ValueError(f"video {mode} validation failed: {result.stderr.decode(errors='replace').strip()}")
    if len(result.stdout) > 4 << 20:
        raise ValueError("video inspector returned unbounded evidence")
    return json.loads(result.stdout)


def _build_part(source, shell, package, profile, recipe, env):
    result = subprocess.run([sys.executable, ("scripts/build_atari_st_video_part.py" if recipe.core_id == "fes.atari-st" else "scripts/build_video_part.py"), "--root", str(source),
                             "--shell", str(shell), "--package", str(package),
                             "--variant", profile, "--cache-root", str(recipe.cache_root), "--gpu", "0"],
                            cwd=source, env=recipes.producer_environment(env, recipe=recipe),
                            stdout=subprocess.PIPE, check=True, text=True)
    path = Path(result.stdout.strip().splitlines()[-1])
    _plain(path)
    if not path.is_relative_to(source / ("build/atari-st-video-parts" if recipe.core_id == "fes.atari-st" else "build/video-parts") / profile):
        raise ValueError("video producer returned an unexpected archive path")
    return path


def resolve_video_parts(source, resolved_package, destination, *, recipe=None,
                        env=None, cache_root=None):
    """Return a sealed single-shell inventory plus immutable build fingerprint data.

    The caller supplies bundle.resolve_core_package's authenticated result and a
    fresh staging destination, then includes the returned tree in its generation.
    """
    recipe = recipes.recipe_for("fes.coleco") if recipe is None else recipe
    if recipe.core_id not in ("fes.coleco", "fes.atari-st"):
        raise ValueError("factory video parts support only fes.coleco and fes.atari-st")
    source = _plain(source, directory=True)
    package = _plain(resolved_package["directory"], directory=True)
    package_id = resolved_package["inputs"]["selection"]["package_id"]
    if not isinstance(package_id, str) or not HEX64.fullmatch(package_id) or package.name != package_id:
        raise ValueError("resolved video shell package identity is invalid")
    functional_key = resolved_package["inputs"]["source_selection"]["functional_inputs_sha256"]
    if not isinstance(functional_key, str) or not HEX64.fullmatch(functional_key):
        raise ValueError("resolved video shell functional identity is invalid")
    cache = Path(cache_root or recipes.CACHE_ROOT / "core-video-parts").absolute()
    shells = cache.parent / "core-video-shells"
    canonical_arguments = {"toolchain_cache": str(recipe.cache_root), "package": str(package),
                           "producer_options": dict(recipe.producer_options)}
    current = _inspect(source, "canonical", canonical_arguments, recipe, env)
    if current["map"] not in VIDEO_OUTPUTS:
        raise ValueError("unsupported factory video profile")
    shell_members = ST_SHELL_MEMBERS if current["map"] == ST_MAP else SHELL_MEMBERS
    part_members = (*PART_MEMBERS, "cart-synth.json") if current["map"] == NATIVE_MAP else PART_MEMBERS
    companion = shells / package_id
    arguments = {"package": str(package), "shell": str(companion), "current": current,
                 "functional_inputs_sha256": functional_key, "toolchain_cache": str(recipe.cache_root)}
    with _locked(shells):
        if companion.exists() or companion.is_symlink():
            _closed(companion, shell_members, sealed=True)
            shell_evidence = _inspect(source, "shell", arguments, recipe, env)
        else:
            producer_output = source / VIDEO_OUTPUTS[current["map"]]
            if not producer_output.is_dir() or any(not (producer_output / name).is_file() for name in shell_members):
                raise MissingVideoShell("resolved video package has no frozen companion; force-resolve its shell and retry")
            try:
                initial = dict(arguments, shell=str(producer_output))
                shell_evidence = _inspect(source, "shell", initial, recipe, env)
            except ValueError as error:
                raise MissingVideoShell("current frozen video output differs from resolved package; force-resolve its shell and retry") from error
            files = {name: _read(producer_output / name) for name in shell_members}
            _publish(companion, files)
            shell_evidence = _inspect(source, "shell", arguments, recipe, env)
    entries, evidence, files = [], [], {}
    # The key includes the exact package and current controlled compiler inputs.
    # Original revision belongs in part provenance, not functional cache identity.
    key_inputs = {key: value for key, value in current.items() if key != "revision"}
    cache_key = _sha(b"fes-factory-video-parts-v1\0" + canonical(
        {"package_id": package_id, "current": key_inputs}))
    store = cache / cache_key
    with _locked(store):
        for profile in PROFILES:
            slot = store / profile
            if slot.exists() or slot.is_symlink():
                cached = _closed(slot, (*part_members, "archive.tar"), sealed=True)
                archive, directory = slot / "archive.tar", slot
            else:
                archive = _build_part(source, companion, package, profile, recipe, env)
                directory = archive.parent
                cached = {name: _read(directory / name) for name in part_members}
                cached["archive.tar"] = _read(archive, limit=MAX_ARCHIVE_BYTES)
            inspected = _inspect(source, "part", dict(arguments, profile=profile,
                                  archive=str(archive), directory=str(directory),
                                  evidence_sha256={name: _sha(cached[name]) for name in part_members}), recipe, env)
            if not HEX64.fullmatch(inspected["part_id"]) or inspected["profile"] != profile:
                raise ValueError("video inspector returned a mismatched part identity")
            if (_sha(cached["archive.tar"]) != inspected["archive_sha256"]
                    or len(cached["archive.tar"]) != inspected["archive_size"]
                    or inspected["archive_size"] > MAX_ARCHIVE_BYTES):
                raise ValueError("video archive changed after validation")
            _publish(slot, cached)
            relative = f"{package_id}/{inspected['part_id']}.tar"
            files[relative] = cached["archive.tar"]
            entries.append({name: inspected[name] for name in
                            ("profile", "part_id", "archive_sha256", "archive_size")} | {"archive_path": relative})
            evidence.append(inspected)
    index = {"version": 1, "packages": [{"package_id": package_id, "parts": entries}]}
    files["index.json"] = canonical(index)
    if _inspect(source, "canonical", canonical_arguments, recipe, env) != current:
        raise ValueError("authenticated video inputs changed during resolution")
    destination = _publish(destination, files)
    read_index(destination)
    return {"directory": destination, "index_path": destination / "index.json",
            "inputs": {"index_sha256": _sha(files["index.json"]),
                       "index_size": len(files["index.json"]), "index": index,
                       "shell": shell_evidence, "parts": evidence,
                       "functional_inputs_sha256": cache_key}}
