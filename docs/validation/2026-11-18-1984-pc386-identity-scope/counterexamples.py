#!/usr/bin/env python3
"""#1984: the counterexample for each proposed change, measured.

For every case it applies one edit to a scratch detached worktree of HEAD and
runs measure.py there, so each figure is what the producer's own code computes
on the edited tree. Nothing is built, sealed or pushed; the worktree is removed
at the end. It also runs three checks that need no edit:

  calibration   measure.py's tool-free design ID at the #1951 sealed head equals
                the sealed record's BUILD_ID
  immutability  adding the oracle and suite to design-v1's PACKAGING_INPUTS (in
                this process only) makes the sealed #1951 record fail validation
  reuse         the cache-reuse validator accepts the sealed #1951 entry from
                today's tree and from a tree whose lockstep suite was edited
                (only with --sealed-entry)

  python3 -I docs/validation/2026-11-18-1984-pc386-identity-scope/counterexamples.py \\
      [--sealed-entry DIR]

Needs build/firmware/pc386/pc386-{bios,vgabios}.bin in sources/misteross.
"""
import argparse
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
TOP = HERE.parents[2]
MISTEROSS = 'sources/misteross'
SEALED_HEAD = '8383b03b2d3c137c1520d226702e7bcda9ee9193'  # #1951's head, the sealed record's revision


def append(path, text):
    def edit(tree):
        target = tree / MISTEROSS / path
        target.write_text(target.read_text() + text)
    return edit


def replace(path, old, new):
    def edit(tree):
        target = tree / MISTEROSS / path
        text = target.read_text()
        if text.count(old) != 1:
            raise SystemExit(f'{path}: expected one {old!r}')
        target.write_text(text.replace(old, new))
    return edit


def both(*edits):
    def edit(tree):
        for e in edits:
            e(tree)
    return edit


# An import-time edit to the oracle provisioner that changes top.qsf through
# the producer's pin-file read, leaving every design parameter as it was.
ORACLE_QSF_PATCH = '''
# #1984 counterexample: runs in the seal process because the sealer imports this module.
import scripts.fes_build_common as _fbc
_fbc_read = _fbc._regular_input
def _fbc_patched(root, relative, *args, **kwargs):
    path = _fbc_read(root, relative, *args, **kwargs)
    if str(relative).endswith("constraints-oss.qsf"):
        import pathlib, tempfile
        extra = pathlib.Path(tempfile.mkdtemp()) / "pins.qsf"
        extra.write_text(path.read_text() + 'set_global_assignment -name OPTIMIZATION_MODE "AGGRESSIVE AREA"\\n')
        return extra
    return path
_fbc._regular_input = _fbc_patched
'''
ORACLE_ROM_PATCH = '''
# #1984 counterexample: flips the first ROM byte from the seal process.
import scripts.build_pc386_firmware as _fw
_fw_rom_hex = _fw.rom_hex
_fw.rom_hex = lambda paths: _fw_rom_hex(paths).replace("ff", "fe", 1)
'''
CASES = [
    # (id, what it models, should the BUILD_ID move?, edit, pin update)
    ('E1', '#1930: a lockstep testbench edit', False,
     append('cores/fes-common/sim/i386/lockstep_tb.cpp', '// #1984 counterexample\n'), False),
    ('E2', '#1976: a Bochs oracle adapter edit', False,
     append('cores/fes-common/sim/i386/bochs_oracle.cpp', '// #1984 counterexample\n'), False),
    ('E3', 'a lockstep runner edit', False,
     append('scripts/test_fes_i386_lockstep.py', '# #1984 counterexample\n'), False),
    ('D1', 'an RTL edit Yosys reads (a CPU source)', True,
     append('cores/fes-common/rtl/i386/fes_i386_cpu.sv', '// #1984 counterexample\n'), False),
    ('D2', 'an edit to the seal Yosys program module, which is also in SUITE_SOURCES', True,
     append('scripts/yosys_netlist.py', '# #1984 counterexample\n'), False),
    ('D3', 'a Quartus pin-constraint edit', True,
     append('cores/fes-pc386/constraints-oss.qsf', '# #1984 counterexample\n'), False),
    ('H1', 'oracle provisioner edit that changes top.qsf from inside the seal process', True,
     append('scripts/fes_i386_oracle.py', ORACLE_QSF_PATCH), False),
    ('H2', 'oracle provisioner edit that changes the ROM from inside the seal process', True,
     append('scripts/fes_i386_oracle.py', ORACLE_ROM_PATCH), False),
    ('P1', 'an input_identity.py edit (what slice S2 edits; v1 packaging)', False,
     append('scripts/input_identity.py', '# #1984 counterexample\n'), False),
    ('R1', '#1983: the firmware recipe failure-report path', False,
     replace('scripts/build_pc386_firmware.py', 'FAILURE_TAIL = 30', 'FAILURE_TAIL = 40'), False),
    ('R2', 'firmware.lock fetch metadata only (#1978 kind: the dev86 mirror URL, in both places)', False,
     both(replace('scripts/build_pc386_firmware.py', 'releases/download/v1.0.1/',
                  'releases/download/v1.0.1-mirror/'),
          replace('cores/fes-pc386/firmware/firmware.lock', 'releases/download/v1.0.1/',
                  'releases/download/v1.0.1-mirror/')), False),
    ('R3', 'ROM layout edit in the recipe (byte order within a beat), lock pin not updated', True,
     replace('scripts/build_pc386_firmware.py', 'data[at:at + ROM_BEAT][::-1].hex()', 'data[at:at + ROM_BEAT].hex()'),
     False),
    ('R3u', 'the same layout edit with the lock pin updated to the new ROM digest', True,
     replace('scripts/build_pc386_firmware.py', 'data[at:at + ROM_BEAT][::-1].hex()', 'data[at:at + ROM_BEAT].hex()'),
     True),
    ('R4', 'ROM_HEX path constant moved in the recipe; the RTL still reads the old path', True,
     replace('scripts/build_pc386_firmware.py', "ROM_HEX = BUILD / 'pc386-rom.hex'",
             "ROM_HEX = BUILD / 'pc386-rom-moved.hex'"), False),
]


def git(*args, cwd=TOP, **kwargs):
    return subprocess.run(['git', *args], cwd=cwd, check=True, text=True, capture_output=True, **kwargs).stdout


def measure(tree, pin):
    out = subprocess.run([sys.executable, '-I', str(HERE / 'measure.py'), str(tree / MISTEROSS), pin or ''],
                         check=True, text=True, capture_output=True)
    return json.loads(out.stdout)


def with_images(tree):
    source = TOP / MISTEROSS / 'build/firmware/pc386'
    target = tree / MISTEROSS / 'build/firmware/pc386'
    target.mkdir(parents=True, exist_ok=True)
    for name in ('pc386-bios.bin', 'pc386-vgabios.bin'):
        shutil.copyfile(source / name, target / name)


def immutability():
    sys.path.insert(0, str(TOP / MISTEROSS))
    sys.path.insert(0, str(HERE))
    from scripts import input_identity
    import proposal
    record = json.loads((HERE / 'sealed-1951-build-inputs.json').read_text())
    input_identity.validate_identities(record)  # passes as recorded
    present = sorted(p for p in proposal.EVIDENCE if p in record['source_inputs'])
    firmware = sorted(p for p in proposal.FIRMWARE_GENERATOR if p in record['source_inputs'])
    saved = input_identity.PACKAGING_INPUTS
    input_identity.PACKAGING_INPUTS = saved | frozenset(present)
    try:
        input_identity.validate_identities(record)
        verdict = 'still validates'
    except ValueError as exc:
        verdict = f'rejected: {exc}'
    finally:
        input_identity.PACKAGING_INPUTS = saved
    return {'sealed_record_design': record['identities']['design'][:32],
            'evidence_paths_in_sealed_record': f'{len(present)} of {len(proposal.EVIDENCE)}',
            'firmware_generator_paths_in_sealed_record': f'{len(firmware)} of {len(proposal.FIRMWARE_GENERATOR)}',
            'after_editing_v1_sets': verdict}


def reuse(tree, entry):
    """Run the cache-reuse validator (the one scripts/bundle.py calls) on the sealed entry."""
    package = next(p for p in entry.iterdir() if p.is_dir() and p.name != 'evidence')
    program = ('import sys; from pathlib import Path; sys.path.insert(0, "cores/fes-pc386/tools"); '
               'from pc386_seal_evidence import validate; '
               'print(validate(Path.cwd(), Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])))')
    run = subprocess.run([sys.executable, '-c', program, str(package), str(entry / 'build-inputs.json'),
                          str(entry / 'evidence')], cwd=tree / MISTEROSS, text=True, capture_output=True)
    return 'accepted' if run.returncode == 0 else 'rejected: ' + run.stderr.strip().splitlines()[-1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--sealed-entry', type=Path)
    args = parser.parse_args()
    head = git('rev-parse', 'HEAD').strip()
    result = {'revision': head, 'immutability': immutability()}
    scratch = Path(tempfile.mkdtemp(prefix='fes-1984-'))
    tree = scratch / 'tree'
    try:
        git('worktree', 'add', '--detach', str(tree), SEALED_HEAD)
        with_images(tree)
        sealed = json.loads((HERE / 'sealed-1951-build-inputs.json').read_text())['identities']['design'][:32]
        measured = measure(tree, '')['v1_build_id']
        result['calibration'] = {'sealed_head': SEALED_HEAD, 'sealed_build_id': sealed, 'measured': measured,
                                 'equal': sealed == measured}
        if args.sealed_entry:
            # At the sealed head, then with a suite edit there: does reuse consult today's suite?
            result['reuse'] = {'sealed_head': reuse(tree, args.sealed_entry)}
            CASES[0][3](tree)
            result['reuse']['sealed_head_suite_edited'] = reuse(tree, args.sealed_entry)
            git('checkout', '--', '.', cwd=tree)
        git('checkout', '--detach', head, cwd=tree)
        base = measure(tree, '')
        pin = base['rom_hex_sha256']
        base = measure(tree, pin)
        result['baseline'] = base
        if args.sealed_entry:
            result['reuse']['head'] = reuse(tree, args.sealed_entry)
        cases = []
        for case, title, should_move, edit, update_pin in CASES:
            edit(tree)
            m = measure(tree, '')
            m = measure(tree, m['rom_hex_sha256'] if update_pin else pin)
            git('checkout', '--', '.', cwd=tree)
            rom_gate = 'pass' if m['rom_hex_sha256'] == (m['rom_hex_sha256'] if update_pin else pin) else 'refuse'
            # The seal's current ROM check compares the file it wrote with root/ROM_HEX, both
            # from the recipe's own constant; F2 checks the file the RTL names against the pin.
            path_gate = 'pass' if m['producer_rom_path'] == m['rtl_rom_path'] else 'refuse'
            cases.append({
                'case': case, 'models': title, 'should_move': should_move,
                'v1_moves': m['v1_build_id'] != base['v1_build_id'],
                'v2_moves': m['v2_build_id'] != base['v2_build_id'],
                'v2_evidence_only_moves': m['v2_build_id_evidence_only'] != base['v2_build_id_evidence_only'],
                'evidence_digest_moves': m['evidence_sha256'] != base['evidence_sha256'],
                'compiler_bytes_change': {
                    'yosys_program': m['program_sha256'] != base['program_sha256'],
                    'qsf': m['qsf_sha256'] != base['qsf_sha256'],
                    'rom_hex': m['rom_hex_sha256'] != base['rom_hex_sha256'],
                    'rom_path_read_by_yosys': m['rtl_rom_path'] != m['producer_rom_path']},
                'f2_rom_gate': rom_gate, 'f2_path_gate': path_gate,
                'todays_rom_check': m['current_rom_check'],
                'todays_check_wrote_the_file_yosys_reads': m['rtl_rom_file_written_by_seal_step'],
            })
        result['cases'] = cases
    finally:
        subprocess.run(['git', 'worktree', 'remove', '--force', str(tree)], cwd=TOP, check=False)
        shutil.rmtree(scratch, ignore_errors=True)
    (HERE / 'counterexamples.json').write_text(json.dumps(result, indent=1) + '\n')
    rows = ['| Case | Edit models | Should move | v1 moves | v2 moves (E+F2) | v2 moves (E only, F0) '
            '| Evidence digest moves | Generated compiler input changes | F2 ROM gate | F2 path gate '
            '| Today\'s ROM check | Today\'s ROM step wrote the file Yosys reads |',
            '| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |']
    yn = {True: 'yes', False: 'no'}
    for c in result['cases']:
        changed = ', '.join(k for k, v in c['compiler_bytes_change'].items() if v) or 'none'
        rows.append(f"| {c['case']} | {c['models']} | {yn[c['should_move']]} | {yn[c['v1_moves']]} | "
                    f"{yn[c['v2_moves']]} | {yn[c['v2_evidence_only_moves']]} | {yn[c['evidence_digest_moves']]} | "
                    f"{changed} | {c['f2_rom_gate']} | {c['f2_path_gate']} | {c['todays_rom_check'][:40]} | "
                    f"{yn[c['todays_check_wrote_the_file_yosys_reads']]} |")
    (HERE / 'counterexamples.md').write_text(
        '# Counterexamples (generated by counterexamples.py; do not edit)\n\n'
        f"Revision `{result['revision']}`.\n\n" + '\n'.join(rows) + '\n\n```json\n'
        + json.dumps({k: v for k, v in result.items() if k not in ('cases',)}, indent=1) + '\n```\n')
    print(json.dumps({k: v for k, v in result.items() if k != 'cases'}, indent=1))
    print('\n'.join(rows))


if __name__ == '__main__':
    main()
