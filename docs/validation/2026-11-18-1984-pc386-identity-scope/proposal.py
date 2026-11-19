"""#1984: the proposed design-v2 roles for pc386, as a prototype projection.

This is a model of the proposal for the counterexample checks, not production
code: it does not touch scripts/input_identity.py. design-v1 stays as it is.
design-v2 keeps every v1 role and adds two:

- evidence: files that validate a sealed design (the Bochs oracle, the lockstep
  suite and the tools that run it). They stay in the record's source_inputs,
  so the record identity and the seal's lockstep check still bind them, and
  they get their own "evidence" digest. They leave the design projection.
- firmware-generator: the firmware recipe and firmware.lock. They leave the
  design projection because the bytes they produce are bound instead by the
  "firmware" parameter, which under option F2 carries the BIOS, VGA BIOS and
  pc386-rom.hex digests that firmware.lock pins, and the seal checks the file
  Yosys reads against that pin.

Unknown files stay design inputs, as in v1.
"""
import hashlib
import json

# The i386 oracle and lockstep suite as fes_i386_oracle declares them, less
# scripts/yosys_netlist.py, which is also the seal's Yosys program.
ORACLE = ('cores/fes-common/sim/i386/i386_oracle.h', 'cores/fes-common/sim/i386/bochs_oracle.cpp',
          'cores/fes-common/sim/i386/bochs/instrument.h', 'cores/fes-common/sim/i386/bochs/flat-stack-limit.patch',
          'scripts/fes_i386_oracle.py')
SUITE = ('scripts/test_fes_i386_lockstep.py', 'scripts/test_fes_i386_vectors.py', 'scripts/test_fes_i386_frontend.py',
         'scripts/i386_verilator.py', 'cores/fes-common/sim/i386/lockstep_tb.cpp',
         'cores/fes-common/sim/i386/biu_dma_tb.cpp', 'cores/fes-common/sim/i386/divergences.toml',
         'cores/fes-common/sim/i386/smc_stale.h', 'cores/fes-pc386/sim/pc386_ddr_model.h')
RUNNERS = ('cores/fes-pc386/tools/pc386_run_lockstep.py', 'cores/fes-pc386/tools/build_pc386_with_lockstep.py')
EVIDENCE = frozenset(ORACLE + SUITE + RUNNERS)
FIRMWARE_GENERATOR = frozenset({'scripts/build_pc386_firmware.py', 'cores/fes-pc386/firmware/firmware.lock'})


def role_v2(path, v1_role):
    if path in EVIDENCE:
        return 'evidence'
    if path in FIRMWARE_GENERATOR:
        return 'firmware-generator'
    return v1_role(path)


def _digest(domain, fields):
    return hashlib.sha256(domain.encode() + b'\0' + json.dumps(
        fields, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def identities_v2(fields, v1_role, *, rom_hex_sha256=None, firmware_roles=True):
    """The design and evidence digests under the prototype. With firmware_roles
    False, only the evidence role is applied (option F0 for the firmware)."""
    def role(path):
        r = role_v2(path, v1_role)
        return v1_role(path) if r == 'firmware-generator' and not firmware_roles else r
    design = {k: v for k, v in fields.items()
              if k not in ('repository', 'revision', 'source_path', 'source_roots', 'tools', 'toolchain',
                           'source_inputs', 'parameters', 'identities')}
    design['source_inputs'] = {p: h for p, h in fields['source_inputs'].items() if role(p) == 'design'}
    from scripts.input_identity import TOOLCHAIN_PARAMETERS
    parameters = {k: v for k, v in fields['parameters'].items() if k not in TOOLCHAIN_PARAMETERS}
    if firmware_roles:
        firmware = json.loads(parameters['firmware'])
        firmware['rom_hex'] = rom_hex_sha256
        parameters['firmware'] = json.dumps(firmware, sort_keys=True, separators=(',', ':'))
    parameters['identity_policy'] = 'design-v2'
    design['parameters'] = parameters
    evidence = {p: h for p, h in fields['source_inputs'].items() if role(p) == 'evidence'}
    return {'design': _digest('fes-design-v2', design), 'evidence': _digest('fes-evidence-v2', evidence)}
