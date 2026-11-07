"""Execute mixed-role strong cold callbacks with actual SHA and injected MMIO."""
import hashlib
import os
from pathlib import Path
import re
import json
import shutil
import subprocess
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoMixedColdTests(unittest.TestCase):
    def test_actual_provider_rom_stop_and_sd_return_cycles_and_failures(self):
        reference = Path(os.environ.get('FES_NANO_SHA_REFERENCE', str(
            ROOT / 'out/terasic-git-restore-tfa/plat/intel/soc/common/lib/sha/sha.c')))
        if not reference.is_file():
            self.skipTest('exact Terasic SHA source required for strong cold integration')
        self.assertEqual(hashlib.sha256(reference.read_bytes()).hexdigest(),
                         'b0e8cb2095a77957741a70f62b42443eaa8e4dfd502aef87bdd3b16f62785d58')
        self.assertEqual(hashlib.sha256(reference.with_name('sha.h').read_bytes()).hexdigest(),
                         '76a51a6aba59f84f754d9b8102394ff12ce0cdad1fb89cec61130543b73f66fb')
        evidence = os.environ.get('FES_NANO_MIXED_COLD_EVIDENCE')
        target = Path(evidence).absolute() if evidence else None
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
            (base / 'fes_nano_cold_profile.h').write_text(profile)
            sources = ('nano_cold_platform.c', 'nano_cold.c', 'nano_boot.c', 'nano_bridges.c',
                'nano_bridge_sip.c', 'nano_sd.c', 'nano_usb_service.c', 'nano_sd_platform.c',
                'nano_identity.c', 'nano_configuration.c', 'nano_program.c', 'nano_rom.c', 'nano_smmu.c',
                'nano_ddr_firewall.c', 'nano_fabric_stop.c', 'nano_probe_stop.c', 'tests/nano_cold_platform_test.c')
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-Wno-unused-const-variable',
                '-DFES_TEST_PROGRAM_ROLES', '-DFES_TEST_HPS_CONTEXT', '-DFES_TEST_CONFIGURATION_EPOCHS',
                '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I' + str(base), '-I' + str(FW),
                *(str(FW / name) for name in sources), str(base / 'vendor_sha.o'), '-o', str(base / 'mixed')]
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            if target is not None:
                (target / 'compile.log').write_text(result.stdout + result.stderr)
            self.assertEqual(result.returncode, 0, result.stderr)
            for mode in (0, 1, 2, 3, 11, 12, 13, 14, 15, 16, 17, 19, 20):
                with self.subTest(mode=mode):
                    result = subprocess.run([base / 'mixed', str(mode)], capture_output=True, text=True, timeout=30)
                    if target is not None:
                        (target / ('case-' + str(mode) + '.log')).write_text(result.stdout + result.stderr)
                    self.assertEqual(result.returncode, 0, result.stderr + result.stdout[-5000:])
                    self.assertIn('PASS', result.stdout)
            # The combined provider requires the full CPU reservation even
            # though the original computer reader remains only8KiB.
            (base / 'fes_nano_cold_profile.h').write_text(profile.replace(
                '#define FES_NANO_READER_ARENA_BYTES (8U<<20)', '#define FES_NANO_READER_ARENA_BYTES (2U<<20)'))
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('source-bound mixed role resources and union memory', result.stderr)
            if target is not None:
                (target / 'short-arena-rejection.log').write_text(result.stdout + result.stderr)
                shutil.copytree(base, target / 'fixture')
                (target / 'fixture/qualified-profile.h').write_text(profile)
                source_files = [FW / name for name in sources] + [
                    FW / name for name in ('nano_program_control.inc', 'nano_computer_control.inc',
                        'nano_window_control.inc', 'nano_rom_platform.inc', 'tests/nano_mixed_cold_test.inc')]
                (target / 'contract.json').write_text(json.dumps(dict(command=command,
                    profile=profile, source_sha256={str(p.relative_to(ROOT)):
                        hashlib.sha256(p.read_bytes()).hexdigest() for p in source_files},
                    scope='actual strong cold/SD/ROM/stop engines, pinned SHA; injected MMIO/transport; no matched board boot'), indent=2) + '\n')


if __name__ == '__main__':
    unittest.main()
