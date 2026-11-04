"""Run the actual contained configuration gate with injected memory/ownership."""
from pathlib import Path
import hashlib
import importlib.util
import json
import os
import re
import shutil
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
            chunk=0xfffc0;raw=(bytes(range(256))*((chunk+383)//256))[:chunk+128]
            def array(digest):return '{'+','.join('0x%02x'%b for b in digest)+'}'
            values=','.join(str(i) for i in range(32))
            profile=('#define FES_NANO_COLD_CHAIN {0x42,0x17}\n#define FES_NANO_COLD_OWNER_SEED 17ULL\n'
                     '#define FES_NANO_HPS_CONTEXT_SHA {'+values+'}\n#define FES_NANO_CONTROLLED_SD 0\n'
                     '#define FES_NANO_CONTROL_PROBE 1\n#define FES_NANO_COLD_CONFIG 1\n'
                     '#define FES_NANO_CONTROL_BASE 0x20010400UL\n#define FES_NANO_CONTROL_ABI 0x10203040U\n'
                     '#define FES_NANO_CONTROL_BUILD {0x44556677,0x00112233,0xbbccddee,0x778899aa}\n'
                     '#define FES_NANO_CONTROL_MAP_SHA {1}\n#define FES_NANO_CONFIG_BASE 0x94800000ULL\n'
                     '#define FES_NANO_CONFIG_ARENA_SIZE (8U<<20)\n#define FES_NANO_CONFIG_BYTES '+str(len(raw))+'U\n'
                     '#define FES_NANO_CONFIG_CHUNK '+str(chunk)+'U\n#define FES_NANO_CONFIG_SHA384 '+array(hashlib.sha384(raw).digest())+'\n'
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
            sources=('nano_cold_platform.c','nano_cold.c','nano_boot.c','nano_bridges.c','nano_bridge_sip.c','nano_sd.c','nano_sd_platform.c','nano_identity.c','nano_configuration.c','nano_program.c','tests/nano_cold_platform_test.c')
            compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*(str(FW/name) for name in sources),str(sha_object),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mutation in (0,43,44,45,46,47,48,49,50,51,52,53,56,57,58,59,60,61,62,63,64,65,66):
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
            # Combine the actual secure SD provider, configuration gate and
            # identity probe in one image. Storage must stop before generation4
            # can be captured, and cannot resume during an armed transfer.
            (inc/'fes_nano_cold_profile.h').write_text(profile.replace('#define FES_NANO_CONTROLLED_SD 0', '#define FES_NANO_CONTROLLED_SD 1'))
            compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*(str(FW/name) for name in sources),str(sha_object),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mutation in range(67,74):
                with self.subTest(storage_configuration=mutation):
                    result=subprocess.run([binary,str(mutation)],capture_output=True,text=True,timeout=15)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-2000:])
                    if mutation in (67,68,69,70):
                        self.assertIn('phase=complete',result.stdout)
                        self.assertRegex(result.stdout,r'phase=closed owner=[0-9a-f]+ generation=6 ')
                    if mutation==73:self.assertRegex(result.stdout,r'FES_NANO_CORE_RETIRED_STATUS owner=[0-9a-f]+ generation=4 ')
            # The exact owned dispatcher must keep generic control unavailable
            # without opt-in, and use strong board callbacks when it is enabled.
            result=subprocess.run([binary,'95'],capture_output=True,text=True,timeout=15)
            self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-2000:])
            self.assertIn('computer control absent without matched opt-in',result.stdout)
            (inc/'fes_nano_cold_profile.h').write_text(profile.replace('#define FES_NANO_CONTROLLED_SD 0', '#define FES_NANO_CONTROLLED_SD 1')+'\n#define FES_NANO_COLD_COMPUTER 1\n')
            compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*(str(FW/name) for name in sources),str(sha_object),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mutation in (*range(90,95),*range(96,103)):
                with self.subTest(computer_configuration=mutation):
                    result=subprocess.run([binary,str(mutation)],capture_output=True,text=True,timeout=15)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-3000:])
                    self.assertIn('PASS strong computer',result.stdout)
            # Match the board's held-DDR reader inventory, including both
            # real master-stop helpers, across a second computer configuration.
            computer_profile=(inc/'fes_nano_cold_profile.h').read_text()
            reader_profile=computer_profile.replace('0x10203040U','0x10203041U')+(
                '#define FES_NANO_READER_CPU_BASE 0x95000000ULL\n'
                '#define FES_NANO_READER_BUS_BASE 0x95000000ULL\n'
                '#define FES_NANO_READER_BYTES 8192U\n'
                '#define FES_NANO_READER_ARENA_BYTES 2097152U\n'
                '#define FES_NANO_PROBE_CPU_BASE 0x20010100UL\n'
                '#define FES_NANO_PROBE_SPAN 128U\n')
            (inc/'fes_nano_cold_profile.h').write_text(reader_profile)
            reader_sources=(*sources,'nano_smmu.c','nano_ddr_firewall.c','nano_fabric_stop.c','nano_probe_stop.c')
            activation=base/'strong-reload'
            command=['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT','-DFES_TEST_CONFIGURATION_EPOCHS',
                     *(str(FW/name) for name in reader_sources),str(sha_object),'-o',str(activation)]
            compiled=subprocess.run(command,capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            evidence=os.environ.get('FES_NANO_ACTIVATION_EVIDENCE')
            if evidence:
                target=Path(evidence).resolve()
                shutil.copytree(base,target)
                (target/'activation-build.json').write_text(json.dumps(
                    {'command':command,'profile':reader_profile,'reader_sources':reader_sources,
                     'sha_source_sha256':hashlib.sha256(reference.read_bytes()).hexdigest()},indent=2)+'\n')
            for mutation in range(146,153):
                with self.subTest(reader_computer_reload=mutation):
                    result=subprocess.run([activation,str(mutation)],capture_output=True,text=True,timeout=15)
                    if evidence:(target/('activation-'+str(mutation)+'.log')).write_text(result.stdout+result.stderr)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-6000:])
                    self.assertIn('PASS strong computer',result.stdout)
                    expected=1 if mutation in (147,149) else 2
                    self.assertEqual(result.stdout.count('FES_NANO_FABRIC_ACTIVATION '),expected)
                    if mutation in (146,148):
                        self.assertEqual(result.stdout.count('FES_NANO_MASTER_STOP status=0 '),2)
                        self.assertIn('PASS fresh configuration inventory epochs=2 ',result.stdout)
            # The existing matched two-load profile does not enable SD return.
            result=subprocess.run([activation,'153'],capture_output=True,text=True,timeout=15)
            self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-6000:])
            self.assertIn('storage return absent without matched opt-in',result.stdout)
            (inc/'fes_nano_cold_profile.h').write_text(reader_profile+'\n#define FES_NANO_COMPUTER_STORAGE_RETURN 1\n')
            storage_activation=base/'strong-storage-return'
            storage_command=[str(storage_activation) if str(arg)==str(activation) else arg for arg in command]
            compiled=subprocess.run(storage_command,capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mutation in range(153,157):
                with self.subTest(storage_computer_reload=mutation):
                    result=subprocess.run([storage_activation,str(mutation)],capture_output=True,text=True,timeout=15)
                    if evidence:(target/('storage-activation-'+str(mutation)+'.log')).write_text(result.stdout+result.stderr)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-6000:])
                    self.assertIn('PASS strong computer',result.stdout)
                    self.assertEqual(result.stdout.count('FES_NANO_FABRIC_ACTIVATION '),1 if mutation==156 else 2)
            # The independent reader-RTL fixture keeps the original selection.
            (inc/'fes_nano_cold_profile.h').write_text(reader_profile)
            (inc/'fes_nano_cold_profile.h').write_text(computer_profile)
            # Reuse that exact strong provider, dispatcher, digest and SD lease,
            # replacing only the FPGA register model with its actual Avalon RTL.
            objects=[sha_object]
            for name in sources:
                obj=base/(Path(name).name+'.o')
                extra=['-DFES_TEST_READER_RTL','-Dmain=fes_strong_reader_main'] if name.startswith('tests/') else []
                compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*extra,
                                         '-c',str(FW/name),'-o',str(obj)],capture_output=True,text=True,timeout=30)
                self.assertEqual(compiled.returncode,0,compiled.stderr)
                objects.append(obj)
            fpga=ROOT/'sources/misteross'
            compiled=subprocess.run(['verilator','--cc','--exe','--build','-j','2',
                '--top-module','fes_de25_embedded','-Wall','-Wno-UNUSEDSIGNAL','--Mdir',str(base/'reader-obj'),
                '-I'+str(fpga/'cores/fes-common/generated'),
                "-DFES_DE25_COMPUTER_BUILD_ID=128'h778899aabbccddee0011223344556677",
                '-CFLAGS','-std=c++17 -O1 -UNDEBUG -DFES_TEST_STRONG_READER -fsanitize=undefined -fno-sanitize-recover=all -I'+str(FW),
                '-LDFLAGS','-fsanitize=undefined '+' '.join(str(p) for p in objects),
                str(fpga/'cores/fes-common/rtl/fes_embedded_control.sv'),
                str(fpga/'prototypes/de25_board_control/fes_de25_embedded.sv'),
                str(fpga/'prototypes/de25_board_control/fes_de25_read_width.sv'),
                str(FW/'tests/nano_reader_rtl_test.cpp')],capture_output=True,text=True,timeout=90)
            self.assertEqual(compiled.returncode,0,compiled.stdout+'\n'+compiled.stderr)
            for mutation in range(74,82):
                with self.subTest(strong_reader_rtl=mutation):
                    result=subprocess.run([base/'reader-obj/Vfes_de25_embedded',str(mutation)],
                                          capture_output=True,text=True,timeout=15)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-3000:])
                    self.assertIn('phase=complete',result.stdout)
                    reader_spec=importlib.util.spec_from_file_location('nano_actual_reader_trace',ROOT/'platform/embedded/nano/reader_trace.py')
                    reader_trace=importlib.util.module_from_spec(reader_spec);reader_spec.loader.exec_module(reader_trace)
                    events=reader_trace.extract(result.stdout)
                    opened=re.search(r'FES_NANO_IDENTITY phase=open owner=([0-9a-f]+) generation=4 ',result.stdout)
                    self.assertIsNotNone(opened)
                    observed=reader_trace.validate(events['events'],owner=int(opened[1],16),source_generation=4,
                                                   abi=0x10203040,build_id='778899aabbccddee0011223344556677')
                    self.assertFalse(events['errors'])
                    self.assertEqual(observed['complete'],mutation==74,observed)
                    if mutation==74:
                        self.assertEqual(observed['reads'],83)
                        self.assertEqual(observed['writes'],12)
                        self.assertFalse(observed['ddr_drain'])
                        for stage in range(9,14):self.assertIn('FES_NANO_READER_CYCLE phase='+str(stage)+' ',result.stdout)
                        self.assertRegex(result.stdout,r'phase=closed owner=[0-9a-f]+ generation=6 ')
                    else:self.assertIn('phase=before offset=',result.stdout)
            print('PASS 8 strong-provider/SD/configuration/actual-reader-RTL success and failure cases',flush=True)
            # Join the same real SD lease, SHA-384 configuration gate and owned
            # dispatcher to both real FPGA masters and the full-frame reader.
            window_profile=(reader_profile.replace('FES_NANO_COLD_COMPUTER 1','FES_NANO_COLD_COMPUTER 0')
                            .replace('0x10203041U','0x10203042U').replace('8192U','4194304U')
                            .replace('2097152U','4194304U')+
                '#define FES_NANO_WINDOW_MEMORY 1\n#define FES_NANO_WINDOW_READ_DIAGNOSTIC 1\n'
                '#define FES_NANO_WINDOW_PRESENT_DIAGNOSTIC 1\n'
                '#define FES_NANO_WINDOW_READ_ATTRS_VERIFIED 1\n'
                '#define FES_NANO_WINDOW_FRAME_BYTES 3686400U\n#define FES_NANO_WINDOW_ROW_BYTES 5120U\n'
                '#define FES_NANO_WINDOW_STRIDE 5120U\n#define FES_NANO_WINDOW_ROWS 720U\n')
            (inc/'fes_nano_cold_profile.h').write_text(window_profile)
            window_objects=[sha_object]
            for name in reader_sources:
                obj=base/('window-'+Path(name).name+'.o')
                extra=['-DFES_TEST_READER_RTL','-DFES_TEST_WINDOW_RTL','-Dmain=fes_strong_window_main'] if name.startswith('tests/') else []
                compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*extra,
                                         '-c',str(FW/name),'-o',str(obj)],capture_output=True,text=True,timeout=30)
                self.assertEqual(compiled.returncode,0,compiled.stderr)
                window_objects.append(obj)
            probe=base/'window-probe'
            compiled=subprocess.run(['verilator','--cc','--build','-j','2','--top-module','fes_de25_ddr_probe',
                '-Wall','--Mdir',str(probe),'-I'+str(fpga/'boards/de25nano/hps'),
                str(fpga/'boards/de25nano/hps/fes_de25_ddr_probe.v')],capture_output=True,text=True,timeout=60)
            self.assertEqual(compiled.returncode,0,compiled.stdout+'\n'+compiled.stderr)
            window=base/'window-reader'
            command=['verilator','--cc','--exe','--build','-j','2','--top-module','fes_de25_embedded',
                '-Wall','-Wno-UNUSEDSIGNAL','--Mdir',str(window),'-I'+str(fpga/'cores/fes-common/generated'),
                "-GBUILD_ID=128'h778899aabbccddee0011223344556677",'-GMEMORY_BACKEND=2',
                "-GDDR_BASE=32'h95000000",'-GDDR_BYTES=4194304',
                '-CFLAGS','-std=c++17 -O1 -UNDEBUG -fsanitize=undefined -fno-sanitize-recover=all -I'+str(FW)+
                    ' -I'+str(fpga/'boards/de25nano/hps')+' -I'+str(probe),
                '-LDFLAGS','-fsanitize=undefined '+' '.join(str(p) for p in window_objects)+
                    ' '+str(probe/'Vfes_de25_ddr_probe__ALL.a'),
                str(fpga/'cores/fes-common/rtl/fes_embedded_control.sv'),
                *(str(fpga/'prototypes/embedded_control'/name) for name in ('fes_embedded_window_reader.sv','fes_embedded_window_control.sv')),
                str(fpga/'prototypes/de25_board_control/fes_de25_embedded.sv'),
                str(fpga/'prototypes/de25_board_control/fes_de25_read_width.sv'),str(FW/'tests/nano_window_runtime_test.cpp')]
            compiled=subprocess.run(command,capture_output=True,text=True,timeout=90)
            self.assertEqual(compiled.returncode,0,compiled.stdout+'\n'+compiled.stderr)
            if evidence:
                shutil.copytree(base,target/'window-build')
                (target/'window-build/contract.json').write_text(json.dumps({'profile':window_profile,'command':command},indent=2)+'\n')
            for mutation in range(160,169):
                with self.subTest(strong_window_runtime=mutation):
                    result=subprocess.run([window/'Vfes_de25_embedded',str(mutation)],capture_output=True,text=True,timeout=15)
                    if evidence:(target/('window-'+str(mutation)+'.log')).write_text(result.stdout+result.stderr)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-4000:])
                    self.assertIn('PASS strong window configuration/read/drain/return',result.stdout)
                    if mutation in (160,167):
                        self.assertIn('FES_TEST_WINDOW_FRAME bytes=3686400 crc32=9597d203',result.stdout)
                    if mutation in (160,164,167):
                        self.assertRegex(result.stdout,r'FES_NANO_MASTER_STOP status=0 .* disable=1 .* cancel=1 complete=1 data=active')
                        self.assertNotIn('FES_NANO_READER_STOP ',result.stdout)
                        self.assertRegex(result.stdout,r'FES_NANO_WINDOW_READER_DRAIN owner=[0-9a-f]+ generation=6 reset=5 reader_generation=1 request_generation=1 cookie=7 ack_generation=1 ack_cookie=7 status=4 pending=0')
                    else:self.assertNotIn('FES_NANO_WINDOW_READER_DRAIN ',result.stdout)
                    if mutation==165:
                        self.assertRegex(result.stdout,r'FES_NANO_READER_STOP samples=[1-9][0-9]* last_poll=128 first_status=1 first_pending=[0-9a-f]+ last_status=2 last_pending=[0-9a-f]+ generation=1 request_generation=1 cookie=7 ack_generation=0 ack_cookie=0 receipt=absent')
                        self.assertRegex(result.stdout,r'FES_NANO_MASTER_STOP status=-110 .* reader_polls=128 disable=1 .* complete=0 data=active')
                    if mutation in (160,164):
                        self.assertRegex(result.stdout,r'FES_NANO_WINDOW_CLOSURE owner=[0-9a-f]+ generation=7 reset=f hardware_drained=8 data_remained_contained=0 producers_quiescent=1 fabric_acknowledged=0')
                    elif mutation==168:
                        self.assertRegex(result.stdout,r'FES_NANO_WINDOW_CLOSURE owner=[0-9a-f]+ generation=6 reset=f hardware_drained=0 data_remained_contained=1 producers_quiescent=1 fabric_acknowledged=0')
                    else:self.assertNotIn('FES_NANO_WINDOW_CLOSURE ',result.stdout)
            print('PASS 9 joined secure SD/configuration/window/all-master-drain/return cases',flush=True)
            # The same actual dispatcher must reject SD return without opt-in.
            result=subprocess.run([window/'Vfes_de25_embedded','170'],capture_output=True,text=True,timeout=15)
            self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-4000:])
            self.assertIn('window SD return absent without matched opt-in',result.stdout)
            # Recompile the C provider for the explicit capability. Retain the
            # original executable and reuse only byte-identical compiled RTL.
            (window/'Vfes_de25_embedded').rename(base/'window-without-storage-return')
            (inc/'fes_nano_cold_profile.h').write_text(window_profile+'\n#define FES_NANO_WINDOW_STORAGE_RETURN 1\n')
            for name in reader_sources:
                obj=base/('window-'+Path(name).name+'.o')
                extra=['-DFES_TEST_READER_RTL','-DFES_TEST_WINDOW_RTL','-Dmain=fes_strong_window_main'] if name.startswith('tests/') else []
                compiled=subprocess.run(['cc',*flags,'-Wextra','-DFES_TEST_HPS_CONTEXT',*extra,
                                         '-c',str(FW/name),'-o',str(obj)],capture_output=True,text=True,timeout=30)
                self.assertEqual(compiled.returncode,0,compiled.stderr)
            compiled=subprocess.run(command,capture_output=True,text=True,timeout=90)
            self.assertEqual(compiled.returncode,0,compiled.stdout+'\n'+compiled.stderr)
            for mutation in range(170,189):
                with self.subTest(strong_window_storage_return=mutation):
                    result=subprocess.run([window/'Vfes_de25_embedded',str(mutation)],capture_output=True,text=True,timeout=15)
                    if evidence:(target/('window-storage-'+str(mutation)+'.log')).write_text(result.stdout+result.stderr)
                    self.assertEqual(result.returncode,0,result.stderr+'\n'+result.stdout[-5000:])
                    self.assertIn('PASS strong window SD return',result.stdout)
            if evidence:
                shutil.copytree(base,target/'window-storage-build')
                (target/'window-storage-build/contract.json').write_text(json.dumps({'profile':window_profile+'\n#define FES_NANO_WINDOW_STORAGE_RETURN 1\n','command':command},indent=2)+'\n')
            print('PASS explicit window SD return, stale/provenance/live-scope failures and unchanged default refusal',flush=True)
