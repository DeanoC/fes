"""Execute mixed-role strong cold callbacks with actual SHA and injected MMIO."""
import hashlib
import os
from pathlib import Path
import re
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'
sys.path.insert(0, str(ROOT / 'platform/embedded/nano'))
import usb_readiness
import tape_ownership_cost


class NanoMixedColdTests(unittest.TestCase):
    def test_actual_provider_rom_stop_and_sd_return_cycles_and_failures(self):
        self.exercise(False)

    def test_actual_shared_usb_provider_menu_core_and_return_cycles_and_failures(self):
        self.exercise(True)

    def test_actual_initial_tape_supplier_dispatch_hash_and_return(self):
        self.exercise(True, initial_tape=True)

    def exercise(self, shared_usb, initial_tape=False):
        reference = Path(os.environ.get('FES_NANO_SHA_REFERENCE', str(
            ROOT / 'out/terasic-git-restore-tfa/plat/intel/soc/common/lib/sha/sha.c')))
        if not reference.is_file():
            self.skipTest('exact Terasic SHA source required for strong cold integration')
        self.assertEqual(hashlib.sha256(reference.read_bytes()).hexdigest(),
                         'b0e8cb2095a77957741a70f62b42443eaa8e4dfd502aef87bdd3b16f62785d58')
        self.assertEqual(hashlib.sha256(reference.with_name('sha.h').read_bytes()).hexdigest(),
                         '76a51a6aba59f84f754d9b8102394ff12ce0cdad1fb89cec61130543b73f66fb')
        evidence = os.environ.get('FES_NANO_MIXED_COLD_EVIDENCE')
        target = Path(evidence + ('-initial-tape' if initial_tape else '-shared-usb' if shared_usb else '')).absolute() if evidence else None
        if target is not None:
            self.assertTrue(target.is_relative_to(ROOT / 'out') and not target.exists())
            target.mkdir(parents=True)
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            for name in ('drivers', 'lib', 'common'):
                (base / name).mkdir()
            (base / 'arch_helpers.h').write_text('#include <stdint.h>\n#define MODE_EL3 3\n'
                '#define GET_EL(v) (((v)>>2)&3)\n#define MPIDR_AFFLVL1_VAL(v) (((v)>>8)&255)\n'
                'uint64_t read_current_el(void);\nuint64_t read_mpidr(void);\nuint64_t read_sctlr_el3(void);\n'
                'uint64_t read_cntpct_el0(void);\nuint32_t read_cntfrq_el0(void);\nvoid dsbsy(void);\n')
            (base / 'drivers/delay_timer.h').write_text('void udelay(unsigned);\n')
            (base / 'lib/mmio.h').write_text('#include <stdint.h>\nuint32_t mmio_read_32(uintptr_t);\n'
                'uint16_t mmio_read_16(uintptr_t);\nvoid mmio_write_32(uintptr_t,uint32_t);\nvoid mmio_write_16(uintptr_t,uint16_t);\n')
            (base / 'common/debug.h').write_text('#include <stdio.h>\n#define NOTICE(...) printf(__VA_ARGS__)\n')
            (base / 'socfpga_mailbox.h').write_text('#include <stdint.h>\n#define MBOX_JOB_ID 1U\n'
                '#define MBOX_CONFIG_STATUS 4U\n#define CMD_CASUAL 0U\n#define PIN_STATUS_NSTATUS (1U<<31)\n'
                '#define SOFTFUNC_STATUS_CONF_DONE 1U\n#define SOFTFUNC_STATUS_INIT_DONE 2U\n'
                '#define SOFTFUNC_STATUS_SEU_ERROR 8U\n'
                'int mailbox_send_cmd(uint32_t,uint32_t,uint32_t*,unsigned,uint32_t,uint32_t*,unsigned*);\n')
            (base / 'sha.h').write_bytes(reference.with_name('sha.h').read_bytes())
            pure = '#include <stdint.h>\n#include <string.h>\n#include <stdlib.h>\n#include <endian.h>\n'
            pure += '#include <assert.h>\n#include "sha.h"\nstatic void watchdog_sw_rst(void) { assert(0); }\n'
            pure += re.sub(r'(?m)^#include[^\n]*\n', '', reference.read_text())
            (base / 'vendor_sha.c').write_text(pure)
            result = subprocess.run(['cc', '-std=c11', '-Wall', '-Werror',
                '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I' + str(base),
                '-c', str(base / 'vendor_sha.c'), '-o', str(base / 'vendor_sha.o')],
                capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stderr)
            def array(data):
                return '{' + ','.join(str(b) for b in data) + '}'
            payload = bytes(range(256))
            rom = bytes((i ^ 0xa5) & 255 for i in range(4096))
            defines = dict(FES_NANO_COLD_CHAIN='{0x42,0x17}', FES_NANO_COLD_OWNER_SEED='17ULL',
                FES_NANO_HPS_CONTEXT_SHA='{0x42}', FES_NANO_CONTROL_MAP_SHA='{1}',
                FES_NANO_CONTROLLED_SD=1, FES_NANO_CONTROL_PROBE=1, FES_NANO_COLD_CONFIG=1,
                FES_NANO_COLD_COMPUTER=1, FES_NANO_COLD_ROM=1, FES_NANO_PROGRAM_ROLES=1,
                FES_NANO_WINDOW_MEMORY=1, FES_NANO_MENU_MEMORY=1, FES_NANO_WINDOW_READ_DIAGNOSTIC=1,
                FES_NANO_WINDOW_PRESENT_DIAGNOSTIC=1, FES_NANO_WINDOW_STORAGE_RETURN=1,
                FES_NANO_COMPUTER_STORAGE_RETURN=1, FES_NANO_WINDOW_READ_ATTRS_VERIFIED=1,
                FES_NANO_CONTROL_BASE='0x20010400UL', FES_NANO_CONTROL_ABI='0x10203041U',
                FES_NANO_CONTROL_BUILD='{0x44556677,0x00112233,0xbbccddee,0x778899aa}',
                FES_NANO_CONFIG_BASE='0x94800000ULL', FES_NANO_CONFIG_ARENA_SIZE='(8U<<20)',
                FES_NANO_CONFIG_BYTES=256, FES_NANO_CONFIG_CHUNK=64,
                FES_NANO_CONFIG_SHA384=array(hashlib.sha384(payload).digest()),
                FES_NANO_CONFIG_CHUNK_DIGESTS='{' + ','.join(array(hashlib.sha384(payload[i:i+64]).digest()) for i in range(0,256,64)) + '}',
                FES_NANO_READER_CPU_BASE='0x95000000ULL', FES_NANO_READER_BUS_BASE='0x95000000ULL',
                FES_NANO_READER_BYTES='8192U', FES_NANO_READER_ARENA_BYTES='(8U<<20)',
                FES_NANO_MENU_SLOTS=2, FES_NANO_MENU_SLOT_BYTES='(4U<<20)',
                FES_NANO_WINDOW_FRAME_BYTES=3686400, FES_NANO_WINDOW_ROW_BYTES=5120,
                FES_NANO_WINDOW_STRIDE=5120, FES_NANO_WINDOW_ROWS=720,
                FES_NANO_PROBE_CPU_BASE='0x20010100UL', FES_NANO_PROBE_SPAN=128,
                FES_NANO_ROM_CPU_BASE='0x20010500UL', FES_TEST_ROM_CRC=zlib.crc32(rom),
                FES_TEST_ROM_SHA256=array(hashlib.sha256(rom).digest()),
                FES_TEST_ROM_SHA384=array(hashlib.sha384(rom).digest()))
            profile = '\n'.join('#define ' + name + ' ' + str(value) for name,value in defines.items()) + '\n'
            if shared_usb:
                project = base / 'project'
                handoff = project / 'board/hps/project/output_files/hps_bootloader_handoff.bin'
                handoff.parent.mkdir(parents=True)
                data = (ROOT / 'tests/fixtures/nano-usb-board/hps_bootloader_handoff.bin').read_bytes()
                handoff.write_bytes(data)
                (project / 'board/hps/build.json').write_text(json.dumps({'generated_handoff_and_sdc_sha256': {
                    'output_files/hps_bootloader_handoff.bin': hashlib.sha256(data).hexdigest()}}))
                (base / 'usb_board_policy.h').write_text(usb_readiness.header(project, {'hps_io_hash': 'd'*64}))
                profile += ('#include "usb_board_policy.h"\n#define FES_NANO_CONTROLLED_USB 1\n'
                    '#define FES_NANO_SHARED_USB 1\n#define FES_NANO_HPS_IO_OBSERVE 1\n'
                    '#define FES_NANO_HPS_IO_REGISTERS FES_NANO_USB_BOARD_PINS\n'
                    '#define FES_NANO_HPS_IO_SHA FES_NANO_USB_BOARD_DIGEST\n')
            if initial_tape:
                profile += '#define FES_NANO_INITIAL_TAPE 1\n'
            (base / 'fes_nano_cold_profile.h').write_text(profile)
            sources = ('nano_cold_platform.c', 'nano_cold.c', 'nano_boot.c', 'nano_bridges.c',
                'nano_bridge_sip.c', 'nano_sd.c', 'nano_usb_service.c', 'nano_sd_platform.c',
                'nano_identity.c', 'nano_configuration.c', 'nano_program.c', 'nano_rom.c', 'nano_smmu.c',
                'nano_ddr_firewall.c', 'nano_fabric_stop.c', 'nano_probe_stop.c', 'tests/nano_cold_platform_test.c')
            if shared_usb:
                sources += ('nano_usb_platform.c', 'nano_usb_board.c', 'nano_usb_composition.c', 'nano_hps_io.c')
            if initial_tape:
                sources += ('nano_tape.c', 'nano_tape_service.c', 'tests/nano_tape_endpoint_test.c')
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-Wno-unused-const-variable',
                '-DFES_TEST_PROGRAM_ROLES', '-DFES_TEST_HPS_CONTEXT', '-DFES_TEST_CONFIGURATION_EPOCHS',
                '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I' + str(base), '-I' + str(FW),
                *(str(FW / name) for name in sources), str(base / 'vendor_sha.o'), '-o', str(base / 'mixed')]
            if shared_usb:
                command.insert(1, '-DFES_TEST_SHARED_USB')
            if initial_tape:
                command.insert(1, '-DFES_TEST_INITIAL_TAPE')
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            if target is not None:
                (target / 'compile.log').write_text(result.stdout + result.stderr)
            self.assertEqual(result.returncode, 0, result.stderr)
            modes = (0, 1, 2, 3, 11, 12, 13, 14, 15, 16, 17, 19, 20)
            if shared_usb:
                modes += (21, 22, 23, 24, 25, 26)
            if initial_tape:
                modes += (30,31,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51)
            for mode in modes:
                with self.subTest(mode=mode):
                    result = subprocess.run([base / 'mixed', str(mode)], capture_output=True, text=True, timeout=30)
                    if target is not None:
                        (target / ('case-' + str(mode) + '.log')).write_text(result.stdout + result.stderr)
                    self.assertEqual(result.returncode, 0, result.stderr + result.stdout[-5000:])
                    self.assertIn('PASS', result.stdout)
                    if initial_tape and mode in (31,33,34,35,38,40,41,42,43,44,45,46,47,48,49,50,51):
                        self.assertEqual(result.stdout.count('FES_NANO_TAPE_FAILURE '), 1)
                    if initial_tape:
                        costs = re.findall(r'FES_NANO_TAPE_COST owner=([0-9a-f]+) generation=(\d+) '
                            r'kind=(\w+) calls=(\d+) samples=(\d+) period=(\d+) ticks=([0-9a-f]+) '
                            r'max=([0-9a-f]+) clock_invalid=(\d+) complete=(\d+) hz=(\d+)', result.stdout)
                        self.assertEqual(len(costs) % 7, 0)
                        for start in range(0, len(costs), 7):
                            group = costs[start:start+7]
                            self.assertEqual([row[2] for row in group], ['guard', 'owns', 'read', 'write', 'barrier', 'image', 'hash'])
                            self.assertEqual(len({row[:2] for row in group}), 1)
                            for owner, generation, kind, calls, samples, period, ticks, maximum, invalid, complete, hz in group:
                                self.assertTrue(int(owner, 16) and int(generation))
                                period = int(period)
                                self.assertEqual(period, 64 if kind in ('owns', 'read', 'write', 'barrier') else 1)
                                self.assertEqual(int(samples), (int(calls)+period-1)//period)
                                self.assertGreaterEqual(int(ticks, 16), int(maximum, 16))
                                self.assertEqual((invalid, hz), ('0', '400000000'))
                                self.assertIn(complete, ('0', '1'))
                        details = re.findall(r'FES_NANO_TAPE_OWNERSHIP_COST owner=([0-9a-f]+) generation=(\d+) '
                            r'kind=(\w+) samples=(\d+) period=(\d+) ticks=([0-9a-f]+) max=([0-9a-f]+) '
                            r'clock_invalid=(\d+) complete=(\d+) hz=(\d+) parent_calls=(\d+) parent_samples=(\d+)', result.stdout)
                        self.assertEqual(len(details), len(costs)//7*4)
                        for start in range(0, len(details), 4):
                            group = details[start:start+4]
                            parent = costs[start//4*7+1]
                            self.assertEqual([row[2] for row in group], ['before', 'usb', 'registers', 'after'])
                            remaining = int(parent[4])
                            for owner, generation, kind, samples, period, ticks, maximum, invalid, complete, hz, calls, parent_samples in group:
                                self.assertEqual((owner, generation), parent[:2])
                                self.assertEqual((calls, parent_samples), parent[3:5])
                                self.assertEqual((period, invalid, complete, hz), ('64', '0', parent[9], parent[10]))
                                self.assertLessEqual(int(samples), remaining)
                                self.assertGreaterEqual(int(ticks, 16), int(maximum, 16))
                                if not int(samples):self.assertEqual((int(ticks, 16), int(maximum, 16)), (0, 0))
                                remaining = int(samples)
                        if mode in (44, 50, 51):
                            self.assertTrue(costs)
                            parent = costs[1]
                            lines = result.stdout.splitlines()
                            ownership_profile = dict(status='diagnostic-profile-observed', owner=int(parent[0], 16),
                                generation=int(parent[1]), failure=dict(line=next(i+1 for i, line in enumerate(lines)
                                    if 'FES_NANO_TAPE_FAILURE ' in line)),
                                costs=[dict(kind='owns', owner=int(parent[0], 16), generation=int(parent[1]),
                                    calls=int(parent[3]), samples=int(parent[4]), period=int(parent[5]),
                                    ticks=int(parent[6], 16), line=next(i+1 for i, line in enumerate(lines)
                                        if 'FES_NANO_TAPE_COST ' in line and ' kind=owns ' in line),
                                    clock_invalid=int(parent[8]), complete=int(parent[9]), hz=int(parent[10]))])
                            decoded = tape_ownership_cost.decode(result.stdout.encode(), ownership_profile)
                            self.assertFalse(decoded['performance_qualified'])
                            self.assertFalse(decoded['hardware_authority'])
                            self.assertEqual([v['kind'] for v in decoded['parts']], list(tape_ownership_cost.PARTS))
                            # Execute malformed/foreign/replayed records from the
                            # real C output, not a separately assembled fixture.
                            raw = result.stdout.encode()
                            marker = b'FES_NANO_TAPE_OWNERSHIP_COST'
                            row = next(v for v in raw.splitlines() if marker in v)
                            for changed in (raw.replace(row, row.replace(b'period=64', b'period=1'), 1),
                                    raw.replace(row, row.replace(('owner='+parent[0]).encode(), b'owner=0'), 1),
                                    raw.replace(row, row.replace(b'clock_invalid=0', b'clock_invalid=1'), 1),
                                    raw.replace(row, row.replace(b'parent_calls=', b'wrong_calls='), 1),
                                    raw+row+b'\n', raw.replace(row+b'\n', b'', 1)):
                                with self.assertRaises(ValueError):tape_ownership_cost.decode(changed, ownership_profile)
                        contained = re.findall(r'FES_NANO_TAPE_CONTAINMENT owner=([0-9a-f]+) '
                            r'generation=(\d+) attempted=(\d+) status=(-?\d+)', result.stdout)
                        if mode in (31,33,34,35,38,40,41,42,43,44,45,46,47,48,49,50,51):
                            self.assertEqual(len(contained), 1)
                            self.assertEqual(contained[0][:2], costs[0][:2])
                            self.assertEqual(contained[0][2], '1')
                            self.assertEqual(int(contained[0][3]), -1 if mode in (50, 51) else 0)
                            self.assertTrue(all(row[9] == '0' for row in costs))
                            self.assertLess(result.stdout.rindex('FES_NANO_TAPE_COST '), result.stdout.index('FES_NANO_TAPE_FAILURE '))
                            self.assertLess(result.stdout.rindex('FES_NANO_TAPE_OWNERSHIP_COST '), result.stdout.index('FES_NANO_TAPE_FAILURE '))
                            self.assertLess(result.stdout.index('FES_NANO_TAPE_CONTAINMENT '), result.stdout.index('FES_NANO_TAPE_FAILURE '))
                        else:
                            self.assertFalse(contained)
            # The combined provider requires the full CPU reservation even
            # though the original computer reader remains only8KiB.
            (base / 'fes_nano_cold_profile.h').write_text(profile.replace(
                '#define FES_NANO_READER_ARENA_BYTES (8U<<20)', '#define FES_NANO_READER_ARENA_BYTES (2U<<20)'))
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('source-bound mixed role resources and union memory', result.stderr)
            if target is not None:
                (target / 'short-arena-rejection.log').write_text(result.stdout + result.stderr)
            if initial_tape:
                for name in ('FES_NANO_COLD_CONFIG', 'FES_NANO_COLD_COMPUTER', 'FES_NANO_COLD_ROM',
                             'FES_NANO_SHARED_USB', 'FES_NANO_CONTROLLED_SD'):
                    with self.subTest(missing_tape_prerequisite=name):
                        (base / 'fes_nano_cold_profile.h').write_text(profile.replace(
                            '#define ' + name + ' 1\n', '#define ' + name + ' 0\n'))
                        rejected = subprocess.run(command, capture_output=True, text=True, timeout=30)
                        self.assertNotEqual(rejected.returncode, 0)
                        diagnostic = ('cold computer control requires the sealed configuration path'
                                      if name == 'FES_NANO_COLD_CONFIG' else 'Initial_tape_requires_the_matched')
                        self.assertIn(diagnostic, rejected.stderr)
                        if target is not None:
                            (target / ('missing-' + name + '.log')).write_text(rejected.stdout + rejected.stderr)
                (base / 'fes_nano_cold_profile.h').write_text(profile)
            if target is not None:
                shutil.copytree(base, target / 'fixture')
                (target / 'fixture/qualified-profile.h').write_text(profile)
                source_files = [FW / name for name in sources] + [
                    FW / name for name in ('nano_program_control.inc', 'nano_computer_control.inc',
                        'nano_window_control.inc', 'nano_rom_platform.inc', 'tests/nano_mixed_cold_test.inc')]
                if initial_tape:
                    source_files += [FW / name for name in ('nano_tape_platform.inc', 'nano_tape.h', 'nano_tape_service.h', 'tests/nano_mixed_tape_test.inc', 'tests/nano_tape_test.c')]
                if shared_usb:
                    source_files += [FW / name for name in ('nano_usb_boot.inc', 'tests/nano_mixed_usb_test.inc')]
                    source_files += [ROOT / name for name in ('platform/embedded/nano/usb_readiness.py',
                        'platform/embedded/nano/hps_io.py', 'tests/fixtures/nano-usb-board/hps_bootloader_handoff.bin')]
                source_files.append(Path(__file__))
                (target / 'contract.json').write_text(json.dumps(dict(command=command,
                    profile=profile, source_sha256={str(p.relative_to(ROOT)):
                        hashlib.sha256(p.read_bytes()).hexdigest() for p in source_files},
                    shared_usb=shared_usb, modes=list(modes),
                    vendor_sha256=hashlib.sha256(reference.read_bytes()).hexdigest(),
                    scope='actual strong cold/SD/ROM/stop engines and optional confirmed shared USB board/master composition; pinned SHA; injected MMIO/transport; no matched board boot or USB enumeration'), indent=2) + '\n')


if __name__ == '__main__':
    unittest.main()
