"""Versioned design/toolchain/record projections shared by producers and caches.

The exceptions below were audited at the module boundary. They may inspect,
authenticate or package a design, but must not supply source bytes to a compiler.
Unknown files and parameters always remain design inputs. Keep v1 semantics
immutable: existing sealed records must remain independently verifiable.
"""
import hashlib
import json
from pathlib import Path, PurePosixPath
import tomllib

POLICY = 'design-v1'
PACKAGING_INPUTS = frozenset({
    'scripts/input_identity.py',
    'scripts/export_core_package.py',
    'scripts/core_package.py',
    'scripts/rfc3986_validator.py',
    'scripts/source_repository.py',
    'scripts/source_provenance.py',
    'scripts/source_closure.py',
    'scripts/source_closures.json',
    'boards/de25nano/package/export_core_package.py',
    'boards/de25nano/package/core_package.py',
    # Audited at the module boundary like the rest of this set (DeanoC/fes#1806
    # review). It is fitted *evidence* for two reviewed asynchronous HDMI inputs:
    # its own docstring states it does not change constraints, waive Design
    # Assistant rules or qualify external/reset timing, and every function reads
    # or verifies Quartus reports (`review_reports`, `input_findings`,
    # `bindings`, `collect`, `verify`). It inspects a finished build and supplies
    # no source bytes to a compiler, so it cannot change what is built.
    'boards/de25nano/scanout/async_inputs.py',
})
TOOLCHAIN_INPUTS = frozenset({
    'scripts/bootstrap.sh', 'scripts/lockfile.py', 'scripts/toolchain_cache.py',
    'scripts/functional_execution.py', 'scripts/compiler_read_audit.py',
    'scripts/quartus_tools.py',
})
TOOLCHAIN_PARAMETERS = frozenset({
    'execution_sha256', 'gpu_device', 'gpu_backend', 'gpu_architectures',
    'toolchain_configuration', 'toolchain_lock', 'toolchain_lock_sha256',
    'compiler', 'pnr_recipe_sha256', 'quartus_version', 'quartus_threads',
    'selection_only', 'toolchain_pins',
    # Which tool elaborates the logic, and that tool's own program text. A front
    # end is a compiler, so by this policy a change of front end is advisory for
    # an already sealed design rather than a rebuild (DeanoC/fes#1806).
    'front_end', 'front_end_program_sha256',
    # The canonical QSF digest is computed over the recipe *including* Quartus's
    # thread count, which this policy already classifies as a toolchain
    # parameter. A design input here would move the design id on a thread-count
    # change, which is the behaviour quartus_recipe_parameters() exists to stop.
    'quartus_qsf_sha256',
    # `device_family` is deliberately NOT here. It names what the design is
    # compiled for, not what compiled it, and the build record carries no other
    # device field -- so classifying it as toolchain would stop the design id
    # distinguishing a Cyclone V build from an Agilex 5 one.
})


def canonical(fields):
    return json.dumps(fields, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode('utf-8')


def digest(domain, fields):
    return hashlib.sha256(domain.encode() + b'\0' + canonical(fields)).hexdigest()


def input_role(path):
    if path in PACKAGING_INPUTS:
        return 'packaging'
    if path in TOOLCHAIN_INPUTS or PurePosixPath(path).name in ('toolchain.lock', 'quartus.lock'):
        return 'toolchain'
    return 'design'


def projections(fields):
    if fields.get('format') != 2 or fields.get('parameters', {}).get('identity_policy') != POLICY:
        raise ValueError('separated identities require a source-closed design-v1 record')
    design = {key: value for key, value in fields.items()
              if key not in ('repository', 'revision', 'source_path', 'source_roots',
                             'tools', 'toolchain', 'source_inputs', 'parameters', 'identities')}
    design['source_inputs'] = {path: value for path, value in fields['source_inputs'].items()
                               if input_role(path) == 'design'}
    design['parameters'] = {key: value for key, value in fields['parameters'].items()
                            if key not in TOOLCHAIN_PARAMETERS}
    toolchain = {
        'tools': fields['tools'],
        'source_inputs': {path: value for path, value in fields['source_inputs'].items()
                          if input_role(path) == 'toolchain'},
        'parameters': {key: value for key, value in fields['parameters'].items()
                       if key in TOOLCHAIN_PARAMETERS and key != 'selection_only'},
    }
    if 'toolchain' in fields:
        toolchain['toolchain'] = fields['toolchain']
    return design, toolchain


def identities(fields):
    design, toolchain = projections(fields)
    original = {key: value for key, value in fields.items() if key != 'identities'}
    return {'design': digest('fes-design-v1', design),
            'toolchain': digest('fes-toolchain-v1', toolchain),
            'record': hashlib.sha256(canonical(original) + b'\n').hexdigest()}


def with_identities(fields, *, root=None):
    result = dict(fields, parameters=dict(fields['parameters'], identity_policy=POLICY))
    if root is not None:
        pins = {}
        for path, expected in result['source_inputs'].items():
            if PurePosixPath(path).name not in ('toolchain.lock', 'quartus.lock'):
                continue
            data = (Path(root) / path).read_bytes()
            if hashlib.sha256(data).hexdigest() != expected:
                raise ValueError('toolchain lock changed while recording: ' + path)
            try:
                lock = tomllib.loads(data.decode('utf-8'))
            except (ValueError, UnicodeError):
                lock = {}  # A diagnostic fixture may carry only opaque lock bytes.
            pins[path] = {name: {key: value for key, value in entry.items()
                                if key in ('repo', 'commit', 'edition', 'version',
                                           'device_family', 'quartus_sh_sha256')}
                          for name, entry in lock.get('tool', {}).items() if isinstance(entry, dict)}
            if isinstance(lock.get('quartus'), dict):
                pins[path]['quartus'] = lock['quartus']
        result['parameters']['toolchain_pins'] = canonical(pins).decode('utf-8')
    result['identities'] = identities(result)
    return result


def validate_identities(fields):
    enabled = fields.get('parameters', {}).get('identity_policy')
    if enabled is None and 'identities' not in fields:
        return  # Historical artifacts retain their original identity semantics.
    if enabled != POLICY or fields.get('identities') != identities(fields):
        raise ValueError('build record identities do not match canonical inputs')


def functional_key(fields):
    if fields.get('format') != 2 or not fields.get('source_inputs'):
        return None
    validate_identities(fields)
    if 'identities' in fields:
        return fields['identities']['design']
    functional = {key: value for key, value in fields.items()
                  if key not in ('repository', 'revision', 'source_path')}
    return digest('fes-functional-inputs-v2', functional)


def manifest_toolchain(record):
    """Describe the tools; recording location and provenance do not enter it."""
    fields = json.loads(record) if isinstance(record, bytes) else record
    return '; '.join(f'{name} {value}' for name, value in sorted(fields['tools'].items()))


def video_design_inputs(current):
    """Keep exact-shell/part choices, excluding recording and compiler metadata."""
    result = {key: value for key, value in current.items()
              if key not in ('revision', 'tools', 'execution', 'source_roots')}
    if 'inputs' in result:
        result['inputs'] = {path: value for path, value in result['inputs'].items()
                            if input_role(path) == 'design'}
    return result


def toolchain_selection(fields):
    """Compare declared compiler choices without authenticating an execution."""
    return {
        'source_inputs': {path: value for path, value in fields.get('source_inputs', {}).items()
                          if input_role(path) == 'toolchain'},
        'parameters': {key: value for key, value in fields.get('parameters', {}).items()
                       if key in TOOLCHAIN_PARAMETERS - {
                           'execution_sha256', 'selection_only', 'pnr_recipe_sha256', 'gpu_device'}},
        'toolchain': fields.get('toolchain'),
    }
