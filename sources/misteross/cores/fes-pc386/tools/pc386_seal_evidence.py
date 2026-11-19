"""Retain and recheck the source-bound evidence of a pc386 product seal.

A receipt is not a signature: it relies on the existing trusted local producer
and immutable cache boundary. Fresh seals regenerate the lockstep netlist; cache
validation rechecks retained report/STA bytes against their original Git source.
"""
import errno
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

import build_pc386_product as seal
if Path(seal.__file__).absolute() != Path(__file__).absolute().with_name('build_pc386_product.py'):
    raise ValueError('pc386 evidence helper loaded a producer from a different checkout')
from scripts.core_package import read_package
from scripts.export_core_package import build_identity, verify_record_source_at_revision

FILES = ('build-summary.json', 'lockstep-report.json', 'quartus-version.log',
         'top.fit.rpt', 'top.qsf', 'top.sta.rpt')


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
    paths = {seal.TOOLCHAIN_LOCK, seal.I386_LOCK, seal.ORACLE_PROVISIONER,
             seal.BOCHS_PATCH, *seal.SUITE_SOURCES,
             *(f'{seal.ORACLE_SIM}/{name}' for name in seal.ORACLE_SOURCES),
             *seal.yosys_netlist.CPU_SOURCES}
    source_prefix = fields['source_path'] + '/'
    for path in sorted(paths):
        expected = fields['source_inputs'].get(path)
        if not seal.valid_sha256(expected):
            raise ValueError('original record omits required lockstep source: ' + path)
        result = subprocess.run(['git', '-C', str(root), 'show',
                                 fields['revision'] + ':' + source_prefix + path],
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=False,
                                env=dict(os.environ, GIT_NO_LAZY_FETCH='1', GIT_OPTIONAL_LOCKS='0',
                                         GIT_TERMINAL_PROMPT='0', GIT_LITERAL_PATHSPECS='1'))
        if result.returncode or digest(result.stdout) != expected:
            raise ValueError('original lockstep source unavailable or changed: ' + path)
        target = destination / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(result.stdout)


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
            or fields['parameters'].get('identity_policy') != 'design-v1'):
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
    package_store = package_store.absolute()
    for directory in (*reversed(package_store.parents), package_store):
        try:
            mode = directory.lstat().st_mode
        except FileNotFoundError:
            directory.mkdir()
            mode = directory.lstat().st_mode
        if not stat.S_ISDIR(mode):
            raise ValueError('pc386 local entry store must not traverse a link')
    base = package_store / '.pc386-entries'
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
