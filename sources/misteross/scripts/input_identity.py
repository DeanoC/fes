"""Versioned design/toolchain/record projections shared by producers and caches.

The exceptions below were audited at the module boundary. They may inspect,
authenticate or package a design, but must not supply source bytes to a compiler.
Unknown files and parameters always remain design inputs. Keep each policy's
semantics immutable once a record uses it: existing sealed records must remain
independently verifiable. design-v1 is frozen. design-v2 (DeanoC/fes#1984)
keeps every v1 role and adds two, for files that leave the design projection:

- evidence: the i386 lockstep oracle, the lockstep suite and the pc386 lockstep
  runners, and PC/XT's Verilator lint configuration and testbenches. They
  validate a sealed design and no compiler reads them. They stay in
  source_inputs, so the record identity binds them, and get their own
  `evidence` digest.
- firmware-generator: the pc386 firmware recipe and firmware.lock. Their
  output is bound instead by the record's `firmware` parameter, which a
  design-v2 record holding them must give the pinned ROM hex digest.

The v2 sets are literals and append-only, and only for paths no sealed v2
record contains: reclassifying a file a sealed record holds orphans that
record. They are not derived from the oracle's source lists, so an edit to a
list (itself evidence) can never move a file out of the design role.
"""
import hashlib
import json
from pathlib import Path, PurePosixPath
import tomllib

DESIGN_V1 = 'design-v1'
DESIGN_V2 = 'design-v2'
POLICIES = (DESIGN_V1, DESIGN_V2)
PACKAGING_INPUTS = frozenset({
    'scripts/input_identity.py',
    'scripts/export_core_package.py',
    'scripts/core_package.py',
    'scripts/rfc3986_validator.py',
    'scripts/source_repository.py',
    'scripts/source_provenance.py',
    'scripts/source_closure.py',
    'scripts/source_closures.json',
    # pc386 evidence reads and validates a completed seal; it does not feed
    # compiler inputs or generate the payload.
    'cores/fes-pc386/tools/pc386_seal_evidence.py',
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
    'scripts/bootstrap.sh', 'scripts/bootstrap-build.sh', 'scripts/bootstrap-build.py', 'scripts/lockfile.py', 'scripts/toolchain_cache.py',
    'scripts/functional_execution.py', 'scripts/compiler_read_audit.py',
    'scripts/quartus_tools.py',
    # Captures execution identity only; the sealer constructs the environment.
    'cores/fes-pc386/tools/pc386_execution.py',
    'cores/fes-pc386/tools/pc386_owned_job.py',
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


# design-v2 only. The oracle, the suite (less scripts/yosys_netlist.py, which is
# also the seal's Yosys program) and the two pc386 lockstep runners: the files
# docs/validation/2026-11-18-1984-pc386-identity-scope/classification.json
# assigns the evidence role, plus #1984 S1's oracle_sources.toml, the data file
# that lists the oracle and suite for the provisioner and the sealer.
PC386_EVIDENCE_INPUTS = frozenset({
    'cores/fes-common/sim/i386/i386_oracle.h',
    'cores/fes-common/sim/i386/bochs_oracle.cpp',
    'cores/fes-common/sim/i386/bochs/instrument.h',
    'cores/fes-common/sim/i386/bochs/flat-stack-limit.patch',
    'cores/fes-common/sim/i386/oracle_sources.toml',
    'scripts/fes_i386_oracle.py',
    'scripts/test_fes_i386_lockstep.py',
    'scripts/test_fes_i386_vectors.py',
    'scripts/test_fes_i386_frontend.py',
    'scripts/i386_verilator.py',
    'cores/fes-common/sim/i386/lockstep_tb.cpp',
    'cores/fes-common/sim/i386/biu_dma_tb.cpp',
    'cores/fes-common/sim/i386/divergences.toml',
    'cores/fes-common/sim/i386/smc_stale.h',
    'cores/fes-pc386/sim/pc386_ddr_model.h',
    'cores/fes-pc386/tools/pc386_run_lockstep.py',
    'cores/fes-pc386/tools/build_pc386_with_lockstep.py',
})
# design-v2 only. fes.pcxt-dos's Verilator lint configuration (cpu.vlt,
# chipset.vlt), testbench C++ and testbench top modules: the files under
# cores/fes-pcxt/sim that docs/validation/2026-11-22-pcxt-sim-evidence/
# classification.json measured as read by no compiler, by none of the seal's
# Python that generates compiler input, and by neither proof the seal runs.
# The record binds them through `git ls-files cores/fes-pcxt`. Its .S
# programs and the BIOS and FreeDOS locks stay design.
PCXT_EVIDENCE_INPUTS = frozenset({
    'cores/fes-pcxt/sim/bios_top.sv',
    'cores/fes-pcxt/sim/capture_pipeline_tb.cpp',
    'cores/fes-pcxt/sim/capture_pipeline_top.sv',
    'cores/fes-pcxt/sim/chipset.vlt',
    'cores/fes-pcxt/sim/clock_bridge_tb.cpp',
    'cores/fes-pcxt/sim/cpu.vlt',
    'cores/fes-pcxt/sim/cpu_tb.cpp',
    'cores/fes-pcxt/sim/cpu_top.sv',
    'cores/fes-pcxt/sim/display_machine_tb.cpp',
    'cores/fes-pcxt/sim/display_machine_top.sv',
    'cores/fes-pcxt/sim/display_tb.cpp',
    'cores/fes-pcxt/sim/display_top.sv',
    'cores/fes-pcxt/sim/dma_tb.cpp',
    'cores/fes-pcxt/sim/dos_tb.cpp',
    'cores/fes-pcxt/sim/fdc_tb.cpp',
    'cores/fes-pcxt/sim/fdc_top.sv',
    'cores/fes-pcxt/sim/hid_keyboard_tb.cpp',
    'cores/fes-pcxt/sim/isa_tb.cpp',
    'cores/fes-pcxt/sim/isa_top.sv',
    'cores/fes-pcxt/sim/keyboard_bat_tb.cpp',
    'cores/fes-pcxt/sim/keyboard_bat_top.sv',
    'cores/fes-pcxt/sim/keyboard_tb.cpp',
    'cores/fes-pcxt/sim/machine_settings_tb.cpp',
    'cores/fes-pcxt/sim/machine_settings_top.sv',
    'cores/fes-pcxt/sim/machine_tb.cpp',
    'cores/fes-pcxt/sim/machine_top.sv',
    'cores/fes-pcxt/sim/media_arbiter_tb.sv',
    'cores/fes-pcxt/sim/media_diag_tb.cpp',
    'cores/fes-pcxt/sim/media_diag_top.sv',
    'cores/fes-pcxt/sim/media_empty_tb.cpp',
    'cores/fes-pcxt/sim/media_runtime_tb.cpp',
    'cores/fes-pcxt/sim/media_runtime_top.sv',
    'cores/fes-pcxt/sim/memory_machine_tb.cpp',
    'cores/fes-pcxt/sim/memory_machine_top.sv',
    'cores/fes-pcxt/sim/memory_model.hpp',
    'cores/fes-pcxt/sim/memory_model_tb.cpp',
    'cores/fes-pcxt/sim/memory_tb.cpp',
    'cores/fes-pcxt/sim/memory_top.sv',
    'cores/fes-pcxt/sim/motherboard_bench.h',
    'cores/fes-pcxt/sim/motherboard_tb.cpp',
    'cores/fes-pcxt/sim/motherboard_top.sv',
    'cores/fes-pcxt/sim/reboot_tb.cpp',
    'cores/fes-pcxt/sim/sector_writer_tb.sv',
    'cores/fes-pcxt/sim/speaker_tb.cpp',
    'cores/fes-pcxt/sim/system_tb.cpp',
    'cores/fes-pcxt/sim/system_top.sv',
    'cores/fes-pcxt/sim/video_machine_tb.cpp',
    'cores/fes-pcxt/sim/hercules_tb.cpp',
    'cores/fes-pcxt/sim/hercules_top.sv',
    'cores/fes-pcxt/sim/video_dos_tb.cpp',
    'cores/fes-pcxt/sim/video_dos_top.sv',
    'cores/fes-pcxt/sim/video_dynamic_tb.cpp',
    'cores/fes-pcxt/sim/video_dynamic_top.sv',
    'cores/fes-pcxt/sim/video_machine_top.sv',
    'cores/fes-pcxt/sim/video_tb.cpp',
    'cores/fes-pcxt/sim/video_top.sv',
})
EVIDENCE_INPUTS = PC386_EVIDENCE_INPUTS | PCXT_EVIDENCE_INPUTS
# design-v2 only. The recipe lays out the ROM hex Yosys reads and the lock pins
# its images and that ROM hex; the seal checks the file Yosys reads against the
# pin, and the record's `firmware` parameter carries the pinned digests.
FIRMWARE_GENERATOR_INPUTS = frozenset({
    'scripts/build_pc386_firmware.py',
    'cores/fes-pc386/firmware/firmware.lock',
})
# The digests that parameter must carry: the two images firmware.lock pins and
# the ROM hex Yosys reads.
FIRMWARE_OUTPUTS = frozenset({'bios', 'vgabios', 'rom_hex'})
_HEX64 = frozenset('0123456789abcdef')


def refuse_compiler_read(rows, evidence=EVIDENCE_INPUTS, firmware_generator=FIRMWARE_GENERATOR_INPUTS,
                         cleared=frozenset()):
    """Check the design-v2 sets against a measured classification of a build record.

    `rows` are the rows of a classification such as #1984's classification.json:
    each names a `path` and whether Yosys read it (`yosys_read`, traced), the
    generated QSF names it (`quartus_read`) or the producer pastes it into the
    QSF (`inlined_in_qsf`). A file a compiler reads supplies design bytes, so it
    may take neither role. Evidence must also not run while the compiler input
    is generated (`generator_code`); the firmware recipe does, and is bound by
    its pinned output instead. A member with no row was never measured and is
    refused unless the caller has `cleared` it by a live check of its own.
    """
    refused = [f'{path}: not in the classification'
               for path in sorted((evidence | firmware_generator) - {row['path'] for row in rows} - set(cleared))]
    for row in rows:
        path = row['path']
        if path not in evidence and path not in firmware_generator:
            continue
        if row['yosys_read'] or row['quartus_read'] or row['inlined_in_qsf']:
            refused.append(f'{path}: read by a compiler')
        elif path in evidence and row['generator_code']:
            refused.append(f'{path}: generates compiler input')
    if refused:
        raise ValueError('design-v2 role refused: ' + '; '.join(refused))

def canonical(fields):
    return json.dumps(fields, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode('utf-8')


def digest(domain, fields):
    return hashlib.sha256(domain.encode() + b'\0' + canonical(fields)).hexdigest()


def input_role(path, policy=DESIGN_V1):
    if policy == DESIGN_V2:
        if path in EVIDENCE_INPUTS:
            return 'evidence'
        if path in FIRMWARE_GENERATOR_INPUTS:
            return 'firmware-generator'
    elif policy != DESIGN_V1:
        raise ValueError(f'unknown identity policy: {policy!r}')
    if path in PACKAGING_INPUTS:
        return 'packaging'
    if path in TOOLCHAIN_INPUTS or PurePosixPath(path).name in ('toolchain.lock', 'quartus.lock'):
        return 'toolchain'
    return 'design'


def _policy(fields):
    policy = fields.get('parameters', {}).get('identity_policy')
    if fields.get('format') != 2 or policy not in POLICIES:
        raise ValueError('separated identities require a source-closed design-v1 or design-v2 record')
    return policy


def _is_hex64(value):
    return isinstance(value, str) and len(value) == 64 and set(value) <= _HEX64


def _require_firmware_output(fields):
    """A v2 record that drops the firmware recipe from its design must bind the ROM hex instead."""
    if not FIRMWARE_GENERATOR_INPUTS & set(fields['source_inputs']):
        return
    try:
        firmware = json.loads(fields['parameters']['firmware'])
    except (KeyError, TypeError, ValueError):
        firmware = None
    if (not isinstance(firmware, dict) or set(firmware) != FIRMWARE_OUTPUTS
            or not all(_is_hex64(value) for value in firmware.values())):
        raise ValueError('a design-v2 record with firmware-generator inputs must pin '
                         'the firmware output digests (bios, vgabios, rom_hex) in its firmware parameter')


def projections(fields):
    policy = _policy(fields)
    if policy == DESIGN_V2:
        _require_firmware_output(fields)
    design = {key: value for key, value in fields.items()
              if key not in ('repository', 'revision', 'source_path', 'source_roots',
                             'tools', 'toolchain', 'source_inputs', 'parameters', 'identities')}
    design['source_inputs'] = {path: value for path, value in fields['source_inputs'].items()
                               if input_role(path, policy) == 'design'}
    design['parameters'] = {key: value for key, value in fields['parameters'].items()
                            if key not in TOOLCHAIN_PARAMETERS}
    toolchain = {
        'tools': fields['tools'],
        'source_inputs': {path: value for path, value in fields['source_inputs'].items()
                          if input_role(path, policy) == 'toolchain'},
        'parameters': {key: value for key, value in fields['parameters'].items()
                       if key in TOOLCHAIN_PARAMETERS and key != 'selection_only'},
    }
    if 'toolchain' in fields:
        toolchain['toolchain'] = fields['toolchain']
    return design, toolchain


def identities(fields):
    design, toolchain = projections(fields)
    original = {key: value for key, value in fields.items() if key != 'identities'}
    record = hashlib.sha256(canonical(original) + b'\n').hexdigest()
    if fields['parameters']['identity_policy'] == DESIGN_V1:
        return {'design': digest('fes-design-v1', design),
                'toolchain': digest('fes-toolchain-v1', toolchain),
                'record': record}
    evidence = {path: value for path, value in fields['source_inputs'].items()
                if input_role(path, DESIGN_V2) == 'evidence'}
    return {'design': digest('fes-design-v2', design),
            'toolchain': digest('fes-toolchain-v2', toolchain),
            'evidence': digest('fes-evidence-v2', evidence),
            'record': record}


def with_identities(fields, *, root=None, policy=DESIGN_V1):
    if policy not in POLICIES:
        raise ValueError(f'unknown identity policy: {policy!r}')
    result = dict(fields, parameters=dict(fields['parameters'], identity_policy=policy))
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
    if enabled not in POLICIES or fields.get('identities') != identities(fields):
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
