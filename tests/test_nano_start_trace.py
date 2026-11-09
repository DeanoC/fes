"""Compare actual vendor START behaviour with and without result recording."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
sys.path.insert(0,str(FW))
import nano_start_trace as trace
import prepare_nano_owned
import prepare_tfa
spec=importlib.util.spec_from_file_location('start_trace_verifier',FW/'verify.py')
verify=importlib.util.module_from_spec(spec);spec.loader.exec_module(verify)
sys.path.remove(str(FW))


class StartTraceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        p=Path(os.environ.get('FES_NANO_SIP_REFERENCE',str(ROOT/'out/terasic-git-restore-tfa'/prepare_tfa.SOURCE)))
        if not p.is_file():raise unittest.SkipTest('exact Terasic service required')
        cls.original=prepare_nano_owned.service_source(p.read_bytes(),cold_serial=True)
        cls.observed=trace.observed(cls.original)

    def test_disabled_does_not_access_files_and_install_records_exact_generated_bytes(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            for disabled in (False,None,0,1,'true'):
                self.assertIsNone(trace.install(root,disabled))
            self.assertEqual(list(root.iterdir()),[])
            p=root/trace.SOURCE;p.parent.mkdir(parents=True);p.write_bytes(self.original)
            record=trace.install(root,True)
            self.assertEqual(p.read_bytes(),self.observed)
            self.assertEqual(record['maximum_records_per_start'],12)
            self.assertEqual((record['control_reads_added'],record['control_writes_added'],record['retries_added']),(0,0,0))
            self.assertTrue(record['uart_output_added'])
            with self.assertRaises(ValueError):trace.install(root,True)
            self.assertEqual(p.read_bytes(),self.observed)

    def test_noncanonical_context_never_writes_partial_source(self):
        for old,new in [
            (b'if (socfpga_bridges_disable(~0U) != 0)',b'if (socfpga_bridges_disable(0U) != 0)'),
            (b'        status = intel_fpga_config_start(0);',b'        status = intel_fpga_config_start(1);'),
            (b'\tbytes_per_block = response[1];',b'\tbytes_per_block = response[2];'),
        ]:
            self.assertEqual(self.original.count(old),1)
            for source in (self.original.replace(old,new),self.original+b'\n'+old):
                with self.subTest(old=old),tempfile.TemporaryDirectory() as temp:
                    root=Path(temp);p=root/trace.SOURCE;p.parent.mkdir(parents=True);p.write_bytes(source)
                    with self.assertRaises(ValueError):trace.install(root,True)
                    self.assertEqual(p.read_bytes(),source)

    def test_actual_engine_call_order_status_and_retained_state_match(self):
        prefix=r'''#include "tfa_stubs.h"
#include <assert.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>
#define FPGA_CONFIG_BUFFER_SIZE 4
typedef enum {NO_REQUEST,RECONFIGURATION,BITSTREAM_AUTH} config_type;
struct fpga_config_info {uint32_t addr;int size,size_written;uint32_t write_requested;int subblocks_sent,block_number;};
static config_type request_type;
static int current_block,current_buffer,read_block,max_blocks;
static uint32_t send_id,bytes_per_block,blocks_submitted,g_remapper_bypass;
static bool bridge_disable;
static int fes_contained_transport_status;
static struct fpga_config_info fpga_config_buffers[4];
static unsigned mode;
static void note(const char *format,...) { va_list a;va_start(a,format);vprintf(format,a);va_end(a); }
#define NOTICE(...) note(__VA_ARGS__)
'''
        suffix=r'''
int socfpga_bridges_disable(uint32_t mask) { assert(mask==~0U);puts("CALL bridge");return mode==1 ? -101:0; }
int mailbox_send_fpga_config_comp(void) { puts("CALL complete");return mode==2 ? -102:0; }
void intel_smmu_hps_remapper_init(uint64_t *p) { assert(!p);puts("CALL remap"); }
void mailbox_clear_response(void) { puts("CALL clear"); }
int mailbox_send_cmd(int job,int command,const uint32_t *arg,unsigned size,int flag,uint32_t *response,unsigned *length) {
 assert(job==MBOX_JOB_ID && flag==CMD_CASUAL);
 if(command==MBOX_CMD_CANCEL) { assert(!arg && !size && !response && !length);puts("CALL cancel");return mode==3 ? -103:0; }
 assert(command==MBOX_RECONFIG && arg && *arg==1 && !size && response && length && *length==3);
 puts("CALL reconfigure");
 if(mode==4)return -104;
 response[0]=mode==6 ? 0:mode==11 ? (uint32_t)INT_MAX+1U:4;
 response[1]=mode==7 ? 0:65536;response[2]=0;
 *length=mode==5 ? 1:mode==8 ? 4:2;
 return mode==12 ? 5:0;
}
int main(int argc,char **argv) {
 assert(argc==2);mode=(unsigned)strtoul(argv[1],0,10);
 if(mode==9)fes_contained_transport_status=-105;
 int result=intel_fpga_config_start(mode==10 ? 1:0);
 printf("RESULT status=%d request=%u credits=%d block=%u bridge=%u current=%d buffer=%d read=%d submitted=%u send=%u remap=%u\n",
 result,request_type,max_blocks,bytes_per_block,bridge_disable,current_block,current_buffer,read_block,blocks_submitted,send_id,g_remapper_bypass);
 return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);binaries=[]
            for enabled,source in [(False,self.original),(True,self.observed)]:
                text=source.decode();helper=verify.function(text,'fes_nano_start_note') if enabled else ''
                # The original branch compiles the same output helper too,
                # avoiding unused-function differences in the test harness.
                if not enabled:prefix_selected=prefix.replace('static void note(', 'void note(')
                else:prefix_selected=prefix
                path=base/('observed.c' if enabled else 'original.c');binary=path.with_suffix('')
                path.write_text(prefix_selected+helper+verify.function(text,'intel_fpga_config_start')+suffix)
                r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-Wno-unused-variable',
                    '-fsanitize=undefined','-fno-sanitize-recover=all','-DFES_TEST_TERASIC_TFA',
                    '-I'+str(FW/'tests'),str(path),'-o',str(binary)],capture_output=True,text=True,timeout=30)
                self.assertEqual(r.returncode,0,r.stderr);binaries.append(binary)
            expected={0:[2,3,4,5,6,7,8],1:[2,3],2:[2,3,4],3:[2,3,4,5],4:[2,3,4,5,6,7],
                      5:[2,3,4,5,6,7],6:[2,3,4,5,6,7],7:[2,3,4,5,6,7],8:[2,3,4,5,6,7],
                      9:[2],10:[2],11:[2,3,4,5,6,7],12:[2,3,4,5,6,7,8]}
            for mode in range(13):
                with self.subTest(mode=mode):
                    old,new=[subprocess.run([str(b),str(mode)],capture_output=True,text=True,timeout=5) for b in binaries]
                    self.assertEqual(old.returncode,0,old.stderr);self.assertEqual(new.returncode,0,new.stderr)
                    rows=[r for r in new.stdout.splitlines() if r.startswith('FES_NANO_START ')]
                    self.assertEqual([int(r.split('phase=')[1].split()[0]) for r in rows],expected[mode])
                    self.assertEqual('\n'.join(r for r in new.stdout.splitlines() if not r.startswith('FES_NANO_START '))+'\n',old.stdout)
                    if mode in (1,2,3,4,9):self.assertTrue(any('status=-10' in r for r in rows))


if __name__=='__main__':unittest.main()
