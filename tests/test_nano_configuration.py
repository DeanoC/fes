"""Run the actual contained configuration gate with injected memory/ownership."""
from pathlib import Path
import hashlib
import importlib.util
import os
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'
spec=importlib.util.spec_from_file_location('nano_digest_test_subject',FW/'nano_digest.py')
digest=importlib.util.module_from_spec(spec)
spec.loader.exec_module(digest)


class NanoConfigurationTests(unittest.TestCase):
    def test_digest_specialization_rejects_unreviewed_inputs_and_is_explicit(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for disabled in (False,None,0,1,'enabled'):
                self.assertIsNone(digest.install(root,disabled))
            self.assertEqual(list(root.iterdir()),[])
            with self.assertRaises(ValueError):digest.unrolled(b'not the reviewed source',b'header')
            self.assertEqual(digest.FLAGS,'$(BUILD_PLAT)/bl31/sha.o: override CFLAGS += -O3\n')

    def test_sequential_transfer_and_ownership_failure_paths(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'configuration'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I' + str(FW),
                       *(str(FW / name) for name in ('nano_bridges.c', 'nano_configuration.c',
                                                     'tests/nano_configuration_test.c')),
                       '-o', str(binary)]
            compiled = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            result = subprocess.run([binary], capture_output=True, text=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertRegex(result.stdout, r'PASS [0-9]+ contained configuration ownership and failure cases')

    def test_strong_cold_provider_with_pinned_sha_and_real_dispatch(self):
        reference=Path(os.environ.get('FES_NANO_SHA_REFERENCE',str(ROOT/'out/terasic-git-restore-tfa/plat/intel/soc/common/lib/sha/sha.c')))
        if not reference.is_file():
            self.skipTest('exact Terasic SHA source required for strong-provider integration')
        header=reference.with_name('sha.h')
        self.assertEqual(hashlib.sha256(reference.read_bytes()).hexdigest(),'b0e8cb2095a77957741a70f62b42443eaa8e4dfd502aef87bdd3b16f62785d58')
        self.assertEqual(hashlib.sha256(header.read_bytes()).hexdigest(),'76a51a6aba59f84f754d9b8102394ff12ce0cdad1fb89cec61130543b73f66fb')
        specialized=digest.unrolled(reference.read_bytes(),header.read_bytes())
        self.assertEqual(specialized.count(b'#pragma GCC unroll '),3)
        self.assertEqual(re.sub(rb'(?m)^\s*#pragma GCC unroll [0-9]+\n',b'',specialized),reference.read_bytes())
        for source,sha_header in ((reference.read_bytes()+b'\n',header.read_bytes()),
                                  (reference.read_bytes(),header.read_bytes()+b'\n'),
                                  (specialized,header.read_bytes())):
            with self.assertRaises(ValueError):digest.unrolled(source,sha_header)
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);inc=base/'include'
            for name in ('drivers','lib','common'): (inc/name).mkdir(parents=True,exist_ok=True)
            (inc/'arch_helpers.h').write_text('#include <stdint.h>\n#define MODE_EL3 3\n#define GET_EL(v) (((v)>>2)&3)\n#define MPIDR_AFFLVL1_VAL(v) (((v)>>8)&255)\nuint64_t read_current_el(void);\nuint64_t read_mpidr(void);\nuint64_t read_sctlr_el3(void);\nuint64_t read_cntpct_el0(void);\nvoid dsbsy(void);\n')
            (inc/'drivers/delay_timer.h').write_text('void udelay(unsigned);\n')
            (inc/'lib/mmio.h').write_text('#include <stdint.h>\nuint32_t mmio_read_32(uintptr_t);\nuint16_t mmio_read_16(uintptr_t);\nvoid mmio_write_32(uintptr_t,uint32_t);\nvoid mmio_write_16(uintptr_t,uint16_t);\n')
            (inc/'common/debug.h').write_text('#include <stdio.h>\n#define NOTICE(...) printf(__VA_ARGS__)\n')
            (inc/'socfpga_mailbox.h').write_text('#include <stdint.h>\n#define MBOX_JOB_ID 1U\n#define MBOX_CONFIG_STATUS 4U\n#define CMD_CASUAL 0U\n#define PIN_STATUS_NSTATUS (1U<<31)\n#define SOFTFUNC_STATUS_CONF_DONE 1U\n#define SOFTFUNC_STATUS_INIT_DONE 2U\n#define SOFTFUNC_STATUS_SEU_ERROR 8U\nint mailbox_send_cmd(uint32_t,uint32_t,uint32_t*,unsigned,uint32_t,uint32_t*,unsigned*);\n')
            (inc/'sha.h').write_bytes(header.read_bytes())
            # Preserve the pinned algorithm and constants; host-only includes
            # supply libc types. A watchdog access aborts this fixture.
            pure='#include <stdint.h>\n#include <string.h>\n#include <stdlib.h>\n#include <endian.h>\n#include <assert.h>\n#include "sha.h"\nstatic void watchdog_sw_rst(void) { assert(0); }\n'
            pure+=re.sub(r'(?m)^#include[^\n]*\n','',specialized.decode())
            sha_source=base/'vendor_sha.c';sha_source.write_text(pure)
            raw=bytes(range(256))*256+bytes(range(128));chunk=65536
            def array(digest):return '{'+','.join('0x%02x'%b for b in digest)+'}'
            values=','.join(str(i) for i in range(32))
            profile=('#define FES_NANO_COLD_CHAIN {0x42,0x17}\n#define FES_NANO_COLD_OWNER_SEED 17ULL\n'
                     '#define FES_NANO_HPS_CONTEXT_SHA {'+values+'}\n#define FES_NANO_CONTROLLED_SD 0\n'
                     '#define FES_NANO_CONTROL_PROBE 1\n#define FES_NANO_COLD_CONFIG 1\n'
                     '#define FES_NANO_CONTROL_BASE 0x20010400UL\n#define FES_NANO_CONTROL_ABI 0x10203040U\n'
                     '#define FES_NANO_CONTROL_BUILD {0x44556677,0x00112233,0xbbccddee,0x778899aa}\n'
                     '#define FES_NANO_CONTROL_MAP_SHA {1}\n#define FES_NANO_CONFIG_BASE 0x94800000ULL\n'
                     '#define FES_NANO_CONFIG_ARENA_SIZE (8U<<20)\n#define FES_NANO_CONFIG_BYTES '+str(len(raw))+'U\n'
                     '#define FES_NANO_CONFIG_CHUNK 65536U\n#define FES_NANO_CONFIG_SHA384 '+array(hashlib.sha384(raw).digest())+'\n'
                     '#define FES_NANO_CONFIG_CHUNK_DIGESTS {'+','.join(array(hashlib.sha384(raw[i:i+chunk]).digest()) for i in range(0,len(raw),chunk))+'}\n')
            (inc/'fes_nano_cold_profile.h').write_text(profile)
            flags=['-std=c11','-Wall','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all','-Wno-unused-const-variable','-I'+str(inc),'-I'+str(FW)]
            sha_object=base/'vendor_sha.o'
            compiled=subprocess.run(['cc',*flags,'-O3','-c',str(sha_source),'-o',str(sha_object)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            runner=base/'digest.c'
            runner.write_text('#include <stdint.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include "sha.h"\n'
                              'int main(int argc,char **argv) { if(argc!=3)return 2;\n'
                              'unsigned offset=(unsigned)strtoul(argv[1],0,10),step=(unsigned)strtoul(argv[2],0,10);\n'
                              'unsigned char *buffer=malloc((2U<<20)+8);if(!buffer || offset>7 || !step)return 3;\n'
                              'size_t n=fread(buffer+offset,1,2U<<20,stdin);if(ferror(stdin) || !feof(stdin))return 4;\n'
                              'sha512_context ctx;unsigned char output[48];sha384_init(&ctx);\n'
                              'for(size_t i=0;i<n;) { size_t amount=n-i;if(amount>step)amount=step;\n'
                              'sha384_update(&ctx,buffer+offset+i,(uint32_t)amount);i+=amount;}\n'
                              'sha384_finish(&ctx,output);free(buffer);return fwrite(output,1,48,stdout)!=48; }\n')
            digest_binary=base/'digest'
            compiled=subprocess.run(['cc',*flags,str(runner),str(sha_object),'-o',str(digest_binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            # Independent hashlib values cover padding boundaries, unaligned
            # input and streaming splits, including one complete bounded core.
            cases=[(b'',0,128),(b'abc',1,1)]
            for length in (1,111,112,113,127,128,129,255,256,257,65536,65664,2027520):
                payload=bytes((i*37+i//7)&255 for i in range(length))
                cases.extend((payload,offset,step) for offset,step in ((0,65536),(3,17),(7,128)))
            for payload,offset,step in cases:
                with self.subTest(bytes=len(payload),offset=offset,step=step):
                    result=subprocess.run([str(digest_binary),str(offset),str(step)],input=payload,capture_output=True,timeout=15)
                    self.assertEqual(result.returncode,0,result.stderr)
                    self.assertEqual(result.stdout,hashlib.sha384(payload).digest())
            binary=base/'strong-cold'
            sources=('nano_cold_platform.c','nano_cold.c','nano_boot.c','nano_bridges.c','nano_bridge_sip.c','nano_sd.c','nano_sd_platform.c','nano_identity.c','nano_configuration.c','tests/nano_cold_platform_test.c')
            compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*(str(FW/name) for name in sources),str(sha_object),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mutation in (0,43,44,45,46,47,48,49,50,51,52,53,56,57,58,59):
                with self.subTest(mutation=mutation):
                    result=subprocess.run([binary,str(mutation)],capture_output=True,text=True,timeout=15)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-2000:])
                    timings=re.findall(r'FES_NANO_CORE_TRANSFER [^\n]* counter=([0-9a-f]+) hash_ticks=([0-9a-f]+)',result.stdout)
                    self.assertTrue(timings)
                    last_counter=last_hash=0
                    for counter,checked in timings:
                        counter,checked=int(counter,16),int(checked,16)
                        self.assertGreater(counter,last_counter)
                        self.assertGreaterEqual(checked,last_hash)
                        self.assertLessEqual(checked-last_hash,counter-last_counter)
                        last_counter,last_hash=counter,checked
                    if mutation in (0,52,53,58,59):
                        self.assertIn('phase=complete',result.stdout)
                        self.assertIn('phase=closed',result.stdout)
