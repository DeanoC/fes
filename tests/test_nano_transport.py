"""Exercise the actual pinned configuration engine's transport outcomes."""
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
import prepare_tfa
spec=importlib.util.spec_from_file_location('nano_transport_verifier',FW/'verify.py')
verifier=importlib.util.module_from_spec(spec);spec.loader.exec_module(verifier)
function=verifier.function
sys.path.remove(str(FW))


class NanoTransportTests(unittest.TestCase):
    def test_transport_failure_never_becomes_busy_or_success(self):
        reference=Path(os.environ.get('FES_NANO_SIP_REFERENCE',str(ROOT/'out/terasic-git-restore-tfa'/prepare_tfa.SOURCE)))
        if not reference.is_file():self.skipTest('exact Terasic service required for engine integration')
        original=reference.read_bytes()
        self.assertEqual(prepare_tfa.source_identity(original)[0],prepare_tfa.TERASIC_BLOB)
        source=prepare_tfa.contained_source(original).decode()
        names=('intel_fpga_sdm_write_buffer','intel_fpga_sdm_write_all','mark_last_buffer_xfer_completed',
               'intel_fpga_config_completed_write','intel_fpga_config_start','is_fpga_config_buffer_full',
               'is_address_in_ddr_range','intel_fpga_config_write','intel_smmu_hps_remapper_init')
        helpers=''
        # The fix adds its fail-closed transport helper ahead of the engine.
        if 'static int fes_contained_transport_failure(' in source:
            helpers='static int fes_contained_transport_status;\nstatic unsigned fes_contained_transport_phase;\n'+function(source,'fes_contained_transport_failure')
        body='''#include "tfa_stubs.h"
#include <assert.h>
#include <stdlib.h>
#include <stdio.h>
#define FPGA_CONFIG_BUFFER_SIZE 4
typedef enum {NO_REQUEST,RECONFIGURATION,BITSTREAM_AUTH} config_type;
struct fpga_config_info {uint32_t addr;int size,size_written;uint32_t write_requested;int subblocks_sent,block_number;};
static config_type request_type;
static int current_block,current_buffer,read_block,max_blocks;
static uint32_t send_id,rcv_id,bytes_per_block,blocks_submitted,g_remapper_bypass;
static bool bridge_disable;
struct fpga_config_info fpga_config_buffers[4];
'''+helpers+'\n'.join(function(source,name) for name in names)+'''
static unsigned mode,submissions,queued,reads,clears;
int socfpga_bridges_disable(uint32_t mask) { assert(mask==~0U);return 0; }
uint32_t mmio_read_32(uintptr_t address) { (void)address;return 0; }
void mmio_write_32(uintptr_t address,uint32_t value) { assert((address==2 || address==3) && value==DRAM_BASE); }
void mailbox_clear_response(void) { clears++; }
int mailbox_send_fpga_config_comp(void) { return 0; }
int mailbox_send_cmd(int job,int command,const uint32_t *a,unsigned count,int flag,uint32_t *response,unsigned *n) {
 (void)job;(void)a;(void)count;(void)flag;
 if(command==MBOX_CMD_CANCEL)return 0;
 assert(command==MBOX_RECONFIG && response && n);response[0]=4;response[1]=65536;*n=2;return 0;
}
int mailbox_send_cmd_async(uint32_t *job,int command,const uint32_t *a,unsigned n,int flag) {
 (void)job;(void)flag;assert(command==MBOX_RECONFIG_DATA && n==3 && a[2]==65536);submissions++;
 if(mode==1)return -17;
 queued++;return 0;
}
int mailbox_read_response(uint32_t *job,uint32_t *response,unsigned *n) {
 (void)response;reads++;*job=0;*n=0;
 if(mode==5 && reads==3)return -17; /* valid ACK followed by empty error */
 if(!queued)return MBOX_NO_RESPONSE;
 if(mode==2) { queued--;return -17; }
 if(mode==3) { queued--;return MBOX_TIMEOUT; }
 if(mode==4 && reads<4)return MBOX_NO_RESPONSE;
 if(mode==6) { queued--;return 7; }
 queued--;return 0;
}
int main(int argc,char **argv) {
 assert(argc==2);mode=(unsigned)strtoul(argv[1],0,10);
 assert(intel_fpga_config_start(0)==0);
 unsigned before=clears;
 assert(intel_fpga_config_write(0x94800000,65536)==0);
 uint32_t a[3]={0},count=0,id=0;
 int status=intel_fpga_config_completed_write(a,&count,&id);
 assert(status==(mode==1 ? 4 : 1));
 if(mode!=1) {
  status=intel_fpga_config_completed_write(a,&count,&id);
  if(mode==4) {
   assert(status==1 && !count && request_type==RECONFIGURATION);
   status=intel_fpga_config_completed_write(a,&count,&id);assert(status==1 && !count);
   status=intel_fpga_config_completed_write(a,&count,&id);
  }
 }
 if(mode==0 || mode==4) {
  assert(status==0 && count==1 && a[0]==0x14800000 && request_type==RECONFIGURATION);
 } else {
  assert(status==4 && count==0 && request_type==NO_REQUEST);
  assert(clears==before); /* No mailbox reset, response flush or replay. */
  unsigned calls=submissions,polled=reads;
  assert(intel_fpga_config_start(0)==4);
  assert(intel_fpga_config_completed_write(a,&count,&id)==4 && !count);
  assert(submissions==calls && reads==polled && clears==before);
 }
 assert(submissions==1);
 puts("PASS actual transport error and completion path");return 0;
}
'''
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);path=base/'transport.c';binary=base/'transport'
            path.write_text(body)
            compiled=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-Wno-unused-variable','-Wno-sign-compare',
                                     '-fsanitize=undefined','-fno-sanitize-recover=all','-DFES_TEST_TERASIC_TFA',
                                     '-I'+str(FW/'tests'),str(path),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mode in range(7):
                with self.subTest(mode=mode):
                    result=subprocess.run([binary,str(mode)],capture_output=True,text=True,timeout=10)
                    self.assertEqual(result.returncode,0,result.stderr)
                    self.assertIn('PASS actual transport',result.stdout)
