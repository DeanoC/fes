"""Retain and recheck the source-bound evidence of a pc386 product seal.

A receipt is not a signature: it relies on the existing trusted local producer
and immutable cache boundary. Fresh seals regenerate the lockstep netlist; cache
validation rechecks retained report/STA bytes against their original Git source.

Reuse after an evidence-only change (DeanoC/fes#1984 S3, decision 2(b)): when
the selected tree differs from a sealed record only in evidence inputs (the
oracle, the lockstep suite and the pc386 lockstep runners), plus the packaging
and toolchain inputs reuse already tolerates, the sealed package is reused only
with a fresh full lockstep of the selected tree against the regenerated CPU
netlist, recorded beside the package. No Quartus runs and the package bytes do
not change. A design or firmware-generator change is never reused this way.
"""
import errno
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile

import build_pc386_product as seal
if Path(seal.__file__).absolute() != Path(__file__).absolute().with_name('build_pc386_product.py'):
    raise ValueError('pc386 evidence helper loaded a producer from a different checkout')
from scripts import input_identity
from scripts.core_package import read_package
from scripts.export_core_package import build_identity, verify_record_source_at_revision

FILES = ('build-summary.json', 'lockstep-report.json', 'quartus-version.log',
         'top.fit.rpt', 'top.qsf', 'top.sta.rpt')
FRESH_LOCKSTEP_FILES = ('lockstep-report.json', 'receipt.json')
FRESH_LOCKSTEP_STORE = '.pc386-fresh-lockstep'
LOCKSTEP_RUNNER = 'cores/fes-pc386/tools/pc386_run_lockstep.py'
# The seal summary's lockstep fields that name the CPU netlist the sealed
# package was built from. A fresh lockstep must have tested that netlist.
NETLIST_BINDING = ('netlist_sha256', 'netlist_program_sha256', 'cpu_source_closure_sha256', 'yosys_tool')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def snapshot(path):
    """Read one bounded, immutable regular file without following a link."""
    path = Path(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if (not stat.S_ISREG(before.st_mode) or stat.S_IMODE(before.st_mode) & 0o222
                or before.st_size > 64 * 1024 * 1024):
            raise ValueError('pc386 evidence must contain bounded sealed regular files')
        data = stream.read(64 * 1024 * 1024 + 1)
        after = os.fstat(stream.fileno())
        if (len(data) != before.st_size or len(data) > 64 * 1024 * 1024
                or (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                   != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)):
            raise ValueError('pc386 evidence changed while reading')
        return data


def make_receipt(package, record, files):
    package = read_package(package)
    if set(files) != set(FILES):
        raise ValueError('pc386 evidence set is incomplete')
    return (json.dumps({'format': 1, 'package_id': package.package_id,
                        'manifest_sha256': digest(package.manifest_bytes),
                        'payload_sha256': digest(package.payload_bytes),
                        'record_sha256': digest(record),
                        'files': {name: digest(files[name]) for name in FILES}},
                       sort_keys=True, separators=(',', ':')) + '\n').encode()


def _historical_sources(root, fields, destination):
    """Write every file the original record lists, at its recorded revision.

    The list is the record's own, never today's oracle, suite or CPU source
    constants: a later addition to one of those must not make an older seal
    unverifiable, and a file the record does not hold is not judged here.
    """
    paths = sorted(fields['source_inputs'])
    source_prefix = fields['source_path'] + '/'
    request = ''.join(f"{fields['revision']}:{source_prefix}{path}\n" for path in paths).encode()
    result = subprocess.run(['git', '-C', str(root), 'cat-file', '--batch'], input=request,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False,
                            env=dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0',
                                     GIT_TERMINAL_PROMPT='0', GIT_LITERAL_PATHSPECS='1'))
    if result.returncode:
        raise ValueError('original record sources are unavailable')
    output, offset = result.stdout, 0
    for path in paths:
        end = output.find(b'\n', offset)
        header = output[offset:end].split() if end >= 0 else []
        if len(header) != 3 or header[1] != b'blob':
            raise ValueError('original lockstep source unavailable or changed: ' + path)
        size = int(header[2])
        data = output[end + 1:end + 1 + size]
        offset = end + 2 + size
        if len(data) != size or digest(data) != fields['source_inputs'][path]:
            raise ValueError('original lockstep source unavailable or changed: ' + path)
        target = destination / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)


def validate(root, package, record_path, evidence_dir):
    """Recheck retained evidence against exact original record and payload."""
    evidence_dir = Path(evidence_dir)
    st = evidence_dir.lstat()
    if not stat.S_ISDIR(st.st_mode) or stat.S_IMODE(st.st_mode) & 0o222:
        raise ValueError('pc386 evidence directory must be sealed')
    if {p.name for p in evidence_dir.iterdir()} != set(FILES) | {'receipt.json'}:
        raise ValueError('pc386 evidence set is missing or has extra members')
    files = {name: snapshot(evidence_dir / name) for name in FILES}
    receipt = snapshot(evidence_dir / 'receipt.json')
    record = snapshot(record_path)
    fields = verify_record_source_at_revision(Path(root), record)
    if (fields.get('source_path') != 'sources/misteross'
            or fields['parameters'].get('core_id') != 'fes.pc386'
            or fields['parameters'].get('identity_policy') not in input_identity.POLICIES):
        raise ValueError('pc386 evidence has the wrong source or product identity')
    package_obj = read_package(package)
    if Path(package).name != package_obj.package_id or receipt != make_receipt(package, record, files):
        raise ValueError('pc386 evidence receipt does not bind the exact record and package')
    manifest = package_obj.fields
    if (manifest['core']['id'] != 'fes.pc386' or manifest['build']['id'] != build_identity(record)
            or manifest['build']['revision'] != fields['revision']
            or manifest['build']['repository'] != fields['repository']
            or manifest['build']['recipe_sha256'] != fields['recipe_sha256']
            or manifest['build']['toolchain'] != seal.manifest_toolchain(fields)):
        raise ValueError('pc386 manifest does not match the original record')
    try:
        summary = json.loads(files['build-summary.json'])
        report = json.loads(files['lockstep-report.json'])
        parameters = fields['parameters']
        router = report['netlist']['yosys']['gpu_router']
        if router not in ('HIP', 'CUDA'):
            raise ValueError('invalid lockstep toolchain slot')
        if parameters['gpu_backend'] != router.lower():
            raise ValueError('lockstep slot differs from the original record')
        tool = seal.board.AuthenticatedTool(Path('/unused/cache-validation'), fields['tools']['yosys'])
        with tempfile.TemporaryDirectory(prefix='fes-pc386-historical-') as temporary:
            source = Path(temporary)
            _historical_sources(Path(root), fields, source)
            lockstep = seal.check_lockstep_metadata(source, files['lockstep-report.json'], tool, router)
        timing = seal.validate_timing_report(files['top.sta.rpt'].decode('utf-8', errors='replace'))
        resources = seal.fit_resources(files['top.fit.rpt'].decode('utf-8', errors='replace'))
        qsf = files['top.qsf'].decode('utf-8')
        build_id = build_identity(record)
        qsf_normalized = qsf.replace(build_id, '0' * 32)
        if (seal.I2C_SITE not in qsf or digest(qsf_normalized.encode()) != parameters['quartus_qsf_sha256']):
            raise ValueError('retained Quartus project differs from the recorded project')
        if files['quartus-version.log'].decode('utf-8') != parameters['quartus_version'] + '\n':
            raise ValueError('Quartus version differs from the original record')
        original_inputs = summary['inputs']
        # The original Git-verified record binds every source byte. The
        # summary's declared inputs must be its matching subset, not today's
        # possibly changed list of packaging dependencies.
        required = {seal.RECIPE, seal.TOOLCHAIN_LOCK, seal.I386_LOCK, seal.ABI_DEFINITION,
                    seal.FIRMWARE_LOCK, *seal.yosys_netlist.CPU_SOURCES}
        if (not isinstance(original_inputs, dict) or not required <= original_inputs.keys()
                or any(fields['source_inputs'].get(path) != value for path, value in original_inputs.items())):
            raise ValueError('pc386 summary source inputs differ from the original record')
        firmware = json.loads(parameters['firmware'])
        if (summary['status'] != 'pass' or summary['build_id'] != build_id
                or summary['device'] != parameters['device'] or summary['top'] != parameters['top']
                or summary['seed'] != parameters['seed'] or summary['gpu_backend'] != parameters['gpu_backend']
                or summary['tools'] != fields['tools']
                or summary.get('toolchain') != seal.manifest_toolchain(fields)
                or summary['rbf'] != {'sha256': digest(package_obj.payload_bytes), 'size': len(package_obj.payload_bytes)}
                or summary['timing'] != timing or summary['resources'] != resources
                or summary['guest_rates'] != json.loads(parameters['guest_rates'])
                or any(summary['firmware'].get(k) != v for k, v in firmware.items())
                or not seal.valid_sha256(summary['firmware'].get('rom_hex'))
                or summary['lockstep'] != lockstep):
            raise ValueError('pc386 seal summary differs from retained evidence or original record')
        netlist = summary['netlist']
        if (netlist['program_sha256'] != lockstep['netlist_program_sha256']
                or netlist['cpu_source_sha256'] != lockstep['cpu_source_closure_sha256']
                or netlist['yosys'] != report['netlist']['yosys']
                or not seal.valid_sha256(netlist['machine_netlist_sha256'])):
            raise ValueError('pc386 seal netlist binding differs from lockstep')
    except (KeyError, TypeError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError('malformed pc386 seal evidence') from exc
    return digest(receipt)


def evidence_drift(original, selected):
    """The evidence inputs that changed between a sealed record and the selected tree.

    Raises when the design identity differs or any design or firmware-generator
    input changed: those are never reused. Roles are design-v2's, the finest
    split, whatever policy the records use. Under design-v1 an evidence file is
    a design input, so an equal design identity there means no evidence drift.
    """
    if input_identity.functional_key(original) != input_identity.functional_key(selected):
        raise ValueError('pc386 reuse refused: the design identity differs from the sealed record')
    before, after = original['source_inputs'], selected['source_inputs']
    changed = sorted(path for path in before.keys() | after.keys() if before.get(path) != after.get(path))
    roles = {path: input_identity.input_role(path, input_identity.DESIGN_V2) for path in changed}
    refused = [f'{path} ({role})' for path, role in roles.items() if role in ('design', 'firmware-generator')]
    if refused:
        raise ValueError('pc386 reuse refused: design or firmware-generator input changed: ' + ', '.join(refused))
    return [path for path in changed if roles[path] == 'evidence']


def _evidence_inputs(fields):
    return {path: value for path, value in sorted(fields['source_inputs'].items())
            if input_identity.input_role(path, input_identity.DESIGN_V2) == 'evidence'}


def _require_selected_tree(root, selected):
    """The selected record must describe the bytes in root, not a claim about them."""
    for path, expected in selected['source_inputs'].items():
        if digest(seal.board._regular_input(Path(root), path).read_bytes()) != expected:
            raise ValueError('selected tree differs from its record: ' + path)


def _router(original):
    router = original['parameters'].get('gpu_backend', '').upper()
    if router not in ('HIP', 'CUDA'):
        raise ValueError('sealed record names no HIP or CUDA toolchain slot')
    return router


def _require_sealed_netlist(lockstep, sealed):
    differing = [key for key in NETLIST_BINDING if lockstep.get(key) != sealed.get(key)]
    if differing:
        raise ValueError('fresh lockstep did not test the sealed CPU netlist: ' + ', '.join(differing) + ' differ')


def _fresh_key(original, selected):
    evidence = input_identity.canonical(_evidence_inputs(selected))
    return original['identities']['record'] + '.' + digest(evidence)


def _fresh_receipt(package_id, record, sealed_receipt, selected, lockstep):
    original = json.loads(record)
    return (json.dumps({'format': 1, 'package_id': package_id, 'record_sha256': digest(record),
                        'sealed_evidence_sha256': sealed_receipt,
                        'design_id': original['identities']['design'],
                        'evidence_inputs': _evidence_inputs(selected), 'lockstep': lockstep},
                       sort_keys=True, separators=(',', ':')) + '\n').encode()


def reuse_gate(root, package, record_path, evidence_dir, selected_record, store):
    """Whether a sealed package may stand for the selected tree, and on what evidence.

    Returns {'reuse': 'unchanged'} when no evidence input changed;
    {'reuse': 'refused', 'reason': ...} when a design or firmware-generator
    input did; {'reuse': 'needs-fresh-lockstep', ...} when only evidence
    changed and no fresh lockstep is recorded for the selected evidence; and
    {'reuse': 'fresh-lockstep', ...} with that record's digest once it is.
    A recorded fresh lockstep that fails its checks raises.
    """
    root, store = Path(root), Path(store)
    record = snapshot(record_path)
    original, selected = json.loads(record), json.loads(selected_record)
    try:
        drift = evidence_drift(original, selected)
    except ValueError as exc:
        return {'reuse': 'refused', 'reason': str(exc)}
    if not drift:
        return {'reuse': 'unchanged'}
    sealed_receipt = validate(root, package, record_path, evidence_dir)
    _require_selected_tree(root, selected)
    directory = store / FRESH_LOCKSTEP_STORE / _fresh_key(original, selected)
    try:
        metadata = directory.lstat()
    except FileNotFoundError:
        return {'reuse': 'needs-fresh-lockstep', 'evidence_drift': drift}
    if (not stat.S_ISDIR(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) & 0o222
            or {p.name for p in directory.iterdir()} != set(FRESH_LOCKSTEP_FILES)):
        raise ValueError('pc386 fresh lockstep record must be a sealed directory of exactly its members')
    files = {name: snapshot(directory / name) for name in FRESH_LOCKSTEP_FILES}
    tool = seal.board.AuthenticatedTool(Path('/unused/cache-validation'), original['tools']['yosys'])
    try:
        lockstep = seal.check_lockstep_metadata(root, files['lockstep-report.json'], tool, _router(original))
        _require_sealed_netlist(lockstep, json.loads(snapshot(Path(evidence_dir) / 'build-summary.json'))['lockstep'])
    except seal.board.BuildError as exc:
        raise ValueError(f'pc386 fresh lockstep record is not valid for the selected tree: {exc}') from exc
    if files['receipt.json'] != _fresh_receipt(read_package(package).package_id, record, sealed_receipt,
                                                selected, lockstep):
        raise ValueError('pc386 fresh lockstep receipt does not bind this package, sealed record and evidence')
    return {'reuse': 'fresh-lockstep', 'evidence_drift': drift,
            'fresh_lockstep_sha256': digest(files['receipt.json'])}


def attest_fresh_lockstep(root, package, record_path, evidence_dir, selected_record, report, yosys, store):
    """Record a passing fresh full lockstep of the selected tree for a sealed package.

    The report must pass the seal's own lockstep gate at the selected tree,
    which regenerates the CPU netlist with the authenticated Yosys, and that
    netlist must be the one the package was sealed from. Runs no Quartus.
    """
    root, store = Path(root), Path(store)
    status = reuse_gate(root, package, record_path, evidence_dir, selected_record, store)
    if status['reuse'] != 'needs-fresh-lockstep':
        raise ValueError(f"pc386 fresh lockstep not applicable: reuse is {status['reuse']}"
                         + (f": {status['reason']}" if 'reason' in status else ''))
    record = snapshot(record_path)
    original, selected = json.loads(record), json.loads(selected_record)
    if yosys.identity != original['tools']['yosys']:
        raise ValueError('fresh lockstep Yosys differs from the sealed record')
    sealed = json.loads(snapshot(Path(evidence_dir) / 'build-summary.json'))['lockstep']
    with tempfile.TemporaryDirectory(prefix='fes-pc386-fresh-lockstep-') as scratch:
        try:
            lockstep = seal.check_lockstep_report(root, report, yosys, _router(original), Path(scratch))
        except seal.board.BuildError as exc:
            raise ValueError(f'pc386 fresh lockstep refused: {exc}') from exc
    _require_sealed_netlist(lockstep, sealed)
    receipt = _fresh_receipt(read_package(package).package_id, record,
                             validate(root, package, record_path, evidence_dir), selected, lockstep)
    base = _plain_store(store) / FRESH_LOCKSTEP_STORE
    base.mkdir(exist_ok=True)
    if not stat.S_ISDIR(base.lstat().st_mode):
        raise ValueError('pc386 fresh lockstep store must be a real directory')
    target = base / _fresh_key(original, selected)
    staged = Path(tempfile.mkdtemp(prefix='.publish-', dir=base))
    try:
        for name, data in (('lockstep-report.json', report), ('receipt.json', receipt)):
            (staged / name).write_bytes(data)
            (staged / name).chmod(0o444)
        staged.chmod(0o555)
        try:
            staged.rename(target)
        except OSError as exc:
            if exc.errno not in (errno.EEXIST, errno.ENOTEMPTY):
                raise
    finally:
        if staged.exists():
            staged.chmod(0o755)
            shutil.rmtree(staged)
    status = reuse_gate(root, package, record_path, evidence_dir, selected_record, store)
    if status['reuse'] != 'fresh-lockstep':
        raise ValueError('recorded pc386 fresh lockstep did not validate')
    return status


def run_fresh_lockstep(root, package, record_path, evidence_dir, selected_record, store, *, cache_root):
    """Run the full default netlist lockstep on the selected tree, then record it. No Quartus."""
    root = Path(root).absolute()
    status = reuse_gate(root, package, record_path, evidence_dir, selected_record, store)
    if status['reuse'] != 'needs-fresh-lockstep':
        return status
    router = _router(json.loads(snapshot(record_path)))
    tools = seal.authenticate_product_tools(root, cache_root=cache_root, gpu_router=router)
    runs = root / 'build/sim/fes-i386-lockstep-runs'
    runs.mkdir(parents=True, exist_ok=True)
    if runs.is_symlink() or not runs.is_dir():
        raise ValueError('pc386 run store must be a real directory')
    run_dir = Path(tempfile.mkdtemp(prefix='run-', dir=runs))
    command = [sys.executable, str(root / LOCKSTEP_RUNNER), '--output', str(run_dir), '--netlist', '--download',
               '--gpu-router', router, *(('--cache-root', str(cache_root)) if cache_root is not None else ())]
    try:
        exit_status = seal.pc386_owned_job.run(command, cwd=root, timeout=6 * 60 * 60,
                                               label='pc386 fresh full lockstep')
    except TimeoutError as exc:
        raise ValueError(f'pc386 fresh full lockstep timed out; output retained at {run_dir}') from exc
    if exit_status:
        raise ValueError(f'pc386 fresh full lockstep failed (exit {exit_status}); output retained at {run_dir}')
    fd = os.open(run_dir / 'report.json', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError('pc386 fresh lockstep did not write a regular report')
        report = stream.read(64 * 1024 * 1024 + 1)
    if len(report) > 64 * 1024 * 1024:
        raise ValueError('pc386 fresh lockstep report exceeds its size bound')
    return attest_fresh_lockstep(root, package, record_path, evidence_dir, selected_record, report,
                                 tools['yosys'], store)


def _plain_store(store):
    """Create store's missing ancestors, refusing any link on the way."""
    store = Path(store).absolute()
    for directory in (*reversed(store.parents), store):
        try:
            mode = directory.lstat().st_mode
        except FileNotFoundError:
            directory.mkdir()
            mode = directory.lstat().st_mode
        if not stat.S_ISDIR(mode):
            raise ValueError('pc386 local entry store must not traverse a link')
    return store


def publish(root, package, record_path, output):
    """Snapshot and validate all seal evidence before exposing its sidecar."""
    package, record_path, output = Path(package), Path(record_path), Path(output)
    fields = json.loads(record_path.read_bytes())
    target = package.parent / (package.name + '.' + fields['identities']['record'] + '.pc386-evidence')
    files = {name: (output / ('project/top.qsf' if name == 'top.qsf' else name)).read_bytes() for name in FILES}
    receipt = make_receipt(package, record_path.read_bytes(), files)
    staged = Path(tempfile.mkdtemp(prefix='.pc386-evidence-', dir=package.parent))
    try:
        for name, data in {**files, 'receipt.json': receipt}.items():
            (staged / name).write_bytes(data)
            (staged / name).chmod(0o444)
        staged.chmod(0o555)
        # Exporter's record sidecars are read-only. Validate staged bytes, not
        # the mutable Quartus output, before the atomic rename.
        validate(root, package, record_path, staged)
        try:
            staged.rename(target)
        except OSError as exc:
            if exc.errno not in (errno.EEXIST, errno.ENOTEMPTY):
                raise
            validate(root, package, record_path, target)
            if any((target / n).read_bytes() != data for n, data in {**files, 'receipt.json': receipt}.items()):
                raise ValueError('existing pc386 evidence differs')
        return target
    finally:
        if staged.exists():
            staged.chmod(0o755)
            shutil.rmtree(staged)


def publish_local(root, package_store, package, record_path, evidence_dir):
    """Atomically expose a validated pc386 package, original record and evidence."""
    package_store, package, record_path, evidence_dir = map(Path, (package_store, package, record_path, evidence_dir))
    record = snapshot(record_path)
    fields = json.loads(record)
    record_id = fields['identities']['record']
    if not seal.valid_sha256(record_id):
        raise ValueError('invalid pc386 record identity')
    package_obj = read_package(package)
    if package.name != package_obj.package_id:
        raise ValueError('pc386 package directory differs from its identity')
    if {p.name for p in package.iterdir()} != {'manifest.toml', 'core.rbf'}:
        raise ValueError('pc386 package has unexpected members')
    contents = {name: snapshot(package / name) for name in ('manifest.toml', 'core.rbf')}
    ev = {name: snapshot(evidence_dir / name) for name in (*FILES, 'receipt.json')}
    if {p.name for p in evidence_dir.iterdir()} != set(ev):
        raise ValueError('pc386 evidence has unexpected members')
    base = _plain_store(package_store) / '.pc386-entries'
    base.mkdir(exist_ok=True)
    if not stat.S_ISDIR(base.lstat().st_mode):
        raise ValueError('pc386 local entry store must be a real directory')
    target = base / record_id
    staged = Path(tempfile.mkdtemp(prefix='.publish-', dir=base))
    try:
        for name in (package_obj.package_id, 'evidence'):
            (staged / name).mkdir()
        for name, data in contents.items():
            (staged / package_obj.package_id / name).write_bytes(data)
            (staged / package_obj.package_id / name).chmod(0o444)
        for name, data in ev.items():
            (staged / 'evidence' / name).write_bytes(data)
            (staged / 'evidence' / name).chmod(0o444)
        (staged / 'build-inputs.json').write_bytes(record)
        (staged / 'build-inputs.json').chmod(0o444)
        (staged / package_obj.package_id).chmod(0o555)
        (staged / 'evidence').chmod(0o555)
        validate(root, staged / package_obj.package_id, staged / 'build-inputs.json', staged / 'evidence')
        staged.chmod(0o555)
        try:
            staged.rename(target)
        except OSError as exc:
            if exc.errno not in (errno.EEXIST, errno.ENOTEMPTY):
                raise
            if not stat.S_ISDIR(target.lstat().st_mode):
                raise ValueError('existing pc386 local entry is not a directory')
            if {p.name for p in target.iterdir()} != {package_obj.package_id, 'build-inputs.json', 'evidence'}:
                raise ValueError('existing pc386 local entry differs')
            validate(root, target / package_obj.package_id, target / 'build-inputs.json', target / 'evidence')
            if (snapshot(target / 'build-inputs.json') != record
                    or any(snapshot(target / package_obj.package_id / n) != b for n, b in contents.items())
                    or any(snapshot(target / 'evidence' / n) != b for n, b in ev.items())):
                raise ValueError('existing pc386 local entry differs')
        return target / package_obj.package_id
    finally:
        if staged.exists():
            staged.chmod(0o755)
            for directory in (staged / package_obj.package_id, staged / 'evidence'):
                if directory.exists():
                    directory.chmod(0o755)
            shutil.rmtree(staged)
