"""Execute the actual pinned mailbox-v3 parser, FIFO transport and contained engine."""
import importlib.util
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
sys.path.insert(0,str(FW))
import prepare_tfa
spec=importlib.util.spec_from_file_location('nano_mailbox_subject',FW/'nano_mailbox.py')
mailbox=importlib.util.module_from_spec(spec);spec.loader.exec_module(mailbox)
sys.path.remove(str(FW))

def function(source,name):
 match=re.search(r'(?m)^(?:static )?(?:inline )?\w+[ \t*]+'+name+r'\([^;{}]*?\)\n\{',source)
 assert match,name
 start=source.index('{',match.start());i=start+1;depth=1
 while depth:
  depth+=(source[i]=='{')-(source[i]=='}');i+=1
 return source[match.start():i]+'\n'

PREFIX=r"""#include <assert.h>
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#define SIP_SVC_V3 1
#define PLATFORM_MODEL 5
#define PLAT_SOCFPGA_AGILEX5 5
#include "socfpga_mailbox.h"
#define VERBOSE(...) ((void)0)
#define INFO(...) ((void)0)
#define ERROR(...) ((void)0)
#define DRAM_BASE 0x80000000U
#define DRAM_SIZE 0x80000000U
#define BL31_LIMIT 0x82000000U
#define PARTIAL_CONFIG 1
#define AUTHENTICATION 2
#define CONFIG_TEST_FLAG(flag,name) ((flag)&(name))
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define INTEL_SIP_SMC_STATUS_REJECTED 2
#define INTEL_SIP_SMC_STATUS_ERROR 4
#define BOOT_SCRATCH_POR_1 1
#define SDM_BE_ARADDR_REMAP 2
#define SDM_BE_AWADDR_REMAP 3
#define SOCFPGA_SYSMGR(r) (r)
typedef unsigned __int128 uint128_t;
typedef unsigned spinlock_t;
static void spin_lock(spinlock_t *p) { assert(!*p);*p=1; }
static void spin_unlock(spinlock_t *p) { assert(*p);*p=0; }
static spinlock_t mbox_db_lock,mbox_read_lock,mbox_write_lock;
static mailbox_service_t mbox_svc;
static uint8_t async_v1_job_id;
static mailbox_payload_t mailbox_resp_payload;
static mailbox_container_t mailbox_resp_ctr={0,0,&mailbox_resp_payload};
static int fes_contained_transport_status;
static unsigned fes_contained_transport_phase;
#define FPGA_CONFIG_BUFFER_SIZE 4
typedef enum { NO_REQUEST,RECONFIGURATION,BITSTREAM_AUTH } config_type;
struct fpga_config_info { uint32_t addr;int size,size_written;uint32_t write_requested;int subblocks_sent,block_number; };
static config_type request_type;
static int current_block,current_buffer,read_block,max_blocks;
static uint32_t send_id,bytes_per_block,blocks_submitted,g_remapper_bypass;
static bool bridge_disable;
static struct fpga_config_info fpga_config_buffers[4];
uint32_t mmio_read_32(uintptr_t);
void mmio_write_32(uintptr_t,uint32_t);
void udelay(unsigned);
void mdelay(unsigned);
int socfpga_bridges_disable(uint32_t);
static void test_notice(const char *,...);
#define NOTICE(...) test_notice(__VA_ARGS__)
"""

DRIVER_FUNCTIONS=('is_mailbox_cmdbuf_full', 'is_mailbox_cmdbuf_empty', 'wait_for_mailbox_cmdbuf_empty', 'write_mailbox_cmd_buffer', 'fill_mailbox_circular_buffer', 'mailbox_send_cmd_async', 'mailbox_send_cmd', 'mailbox_poll_response', 'mailbox_clear_response', 'mailbox_get_free_cmd_desc', 'mailbox_free_cmd_desc', 'mailbox_get_free_resp_desc', 'mailbox_free_resp_desc', 'mailbox_get_cmd_desc', 'mailbox_fill_cmd_desc', 'mailbox_get_resp_desc', 'mailbox_get_resp_desc_cid', 'mailbox_response_parser', 'mailbox_response_handler_fsm', 'mailbox_read_response_v3', 'mailbox_poll_response_v3', 'mailbox_read_response_async', 'mailbox_init_v3', 'mailbox_send_fpga_config_comp')
ENGINE_FUNCTIONS=('fes_contained_transport_failure', 'intel_fpga_sdm_write_buffer', 'intel_fpga_sdm_write_all', 'mark_last_buffer_xfer_completed', 'intel_fpga_config_completed_write', 'intel_fpga_config_start', 'is_fpga_config_buffer_full', 'is_address_in_ddr_range', 'intel_fpga_config_write', 'intel_smmu_hps_remapper_init')

class NanoMailboxTests(unittest.TestCase):
    def test_observer_is_explicit_and_requires_both_reviewed_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for disabled in (False,None,0,1,'enabled'):
                self.assertIsNone(mailbox.install(root,disabled))
            self.assertEqual(list(root.iterdir()),[])
        for source,header in ((b'',b''),(b'modified transport',b'header')):
            with self.assertRaises(ValueError):mailbox.checked(source,header)

    def test_actual_parser_full_stream_wrap_delay_and_missing_response(self):
        reference=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        if not (reference/mailbox.SOURCE).is_file():
            self.skipTest('exact Terasic mailbox source required for parser integration')
        source=(reference/mailbox.SOURCE).read_bytes();header=(reference/mailbox.HEADER).read_bytes()
        mailbox.checked(source,header)
        for changed_source,changed_header in ((source+b'\n',header),(source,header+b'\n')):
            with self.assertRaises(ValueError):mailbox.checked(changed_source,changed_header)
        engine=prepare_tfa.contained_source((reference/prepare_tfa.SOURCE).read_bytes()).decode()
        parts=[function(source.decode(),name) for name in DRIVER_FUNCTIONS]
        parts += [function(engine,name) for name in ENGINE_FUNCTIONS]
        prototypes=''.join(part[:part.index('{')].rstrip()+';\n' for part in parts)
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);inc=base/'include';(inc/'lib').mkdir(parents=True)
            (inc/'lib/utils_def.h').write_text('#define BIT(n) (1U << (n))\n#define U(n) n##U\n#define ARRAY_SIZE(a) (sizeof(a)/sizeof((a)[0]))\n')
            (inc/'socfpga_mailbox.h').write_bytes(header)
            # The real installer appends the observer to the reviewed transport;
            # source mutation is rejected before any output is written.
            exported=base/'export';target=exported/mailbox.SOURCE;target.parent.mkdir(parents=True)
            target.write_bytes(source);h=exported/mailbox.HEADER;h.parent.mkdir(parents=True,exist_ok=True);h.write_bytes(header)
            h.write_bytes(header+b'\n')
            with self.assertRaises(ValueError):mailbox.install(exported,True)
            self.assertEqual(target.read_bytes(),source)
            self.assertFalse(h.with_name('nano_mailbox_observe.h').exists())
            h.write_bytes(header)
            policy=mailbox.install(exported,True)
            observer=(FW/'nano_mailbox_observe.inc').read_text()
            self.assertEqual(target.read_bytes(),source+b'\n'+observer.encode())
            self.assertFalse(policy['mailbox_commands_added']);self.assertFalse(policy['mailbox_register_writes_added'])
            model=base/'model.c';binary=base/'model'
            model.write_text(PREFIX+prototypes+'\n'.join(parts)+observer+(FW/'tests/nano_mailbox_test.c').read_text())
            command=['cc','-std=c11','-Wall','-Wextra','-Werror','-Wno-sign-compare','-Wno-unused-parameter',
                     '-fsanitize=address,undefined','-fno-sanitize-recover=all','-I'+str(inc),'-I'+str(FW),str(model),'-o',str(binary)]
            compiled=subprocess.run(command,capture_output=True,text=True,timeout=30)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            for mode in range(5):
                for initial in range(16):
                    with self.subTest(mode=mode,initial=initial):
                        result=subprocess.run([str(binary),str(mode),str(initial)],capture_output=True,text=True,timeout=10)
                        self.assertEqual(result.returncode,0,result.stderr)
                        expected=2027520 if mode<2 else 786432
                        self.assertIn('completed='+str(expected),result.stdout)
