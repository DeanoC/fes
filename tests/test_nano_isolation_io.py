"""Compare original and observed real mailbox send/fill/poll control paths."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from test_nano_start_trace import FW,ROOT,trace
from test_nano_mailbox import function

PREFIX=r'''#include <assert.h>
#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define SIP_SVC_V3 1
#define PLATFORM_MODEL 5
#define PLAT_SOCFPGA_AGILEX5 5
#include "socfpga_mailbox.h"
#define INFO(...) ((void)0)
#define VERBOSE(...) ((void)0)
#define ERROR(...) ((void)0)
#define NOTICE(...) printf(__VA_ARGS__)
typedef unsigned spinlock_t;
static spinlock_t mbox_write_lock;
static void spin_lock(spinlock_t *p) {assert(!*p);*p=1;}
static void spin_unlock(spinlock_t *p) {assert(*p);*p=0;}
static unsigned mode,reads,writes,delays,restarts,fills,parses,gets,frees;
static uint64_t events=1469598103934665603ULL;
static uint32_t registers[0x500/4];
static sdm_command_t command;
static sdm_response_t simulated_response;
static void event(unsigned kind,uint64_t a,uint64_t b) {events^=kind;events*=1099511628211ULL;events^=a;events*=1099511628211ULL;events^=b;events*=1099511628211ULL;}
uint32_t mmio_read_32(uintptr_t addr) {
 assert(addr>=MBOX_OFFSET && addr<MBOX_OFFSET+sizeof(registers) && !(addr&3));
 uint32_t value=registers[(addr-MBOX_OFFSET)/4];reads++;event(1,addr,value);return value;
}
void mmio_write_32(uintptr_t addr,uint32_t value) {
 assert(addr>=MBOX_OFFSET && addr<MBOX_OFFSET+sizeof(registers) && !(addr&3));
 writes++;event(2,addr,value);unsigned off=addr-MBOX_OFFSET;
 registers[off/4]=value;
 if(off==MBOX_DOORBELL_TO_SDM && mode!=3 && mode!=9) {
  registers[MBOX_COUT/4]=registers[MBOX_CIN/4];
  registers[MBOX_DOORBELL_FROM_SDM/4]=mode==1 ? 0:mode==8 ? 2:1;
 }
}
void mdelay(unsigned n) {delays++;event(3,n,0);}
void udelay(unsigned n) {delays++;event(4,n,0);}
int mailbox_init(void) {restarts++;event(5,0,0);return mode==9 ? mailbox_send_cmd(0U,MBOX_CMD_RESTART,NULL,0U,CMD_URGENT,NULL,NULL):MBOX_TIMEOUT;}
static int mailbox_fill_cmd_desc(uint8_t client,uint8_t job,uint32_t *resp) {
 assert(client==MBOX_ATF_CLIENT_ID && job==MBOX_JOB_ID && !resp);fills++;event(6,client,job);return mode==4 ? MBOX_BUFFER_FULL:0;
}
static sdm_command_t *mailbox_get_cmd_desc(uint8_t client,uint8_t job) {
 assert(client==MBOX_ATF_CLIENT_ID && job==MBOX_JOB_ID);gets++;event(7,client,job);return &command;
}
static int mailbox_response_handler_fsm(void) {
 parses++;event(8,0,0);if(mode==6)registers[MBOX_DOORBELL_FROM_SDM/4]=1;return 0;
}
static sdm_response_t *mailbox_get_resp_desc(uint8_t client,uint8_t job,uint8_t *di) {
 assert(client==MBOX_ATF_CLIENT_ID && job==MBOX_JOB_ID && di);event(9,client,job);*di=0;
 return mode==6 || mode==7 ? NULL:&simulated_response;
}
static void mailbox_free_resp_desc(uint8_t di) {assert(!di);frees++;event(10,di,0);}
static void mailbox_free_cmd_desc(sdm_command_t *cmd) {assert(cmd==&command);frees++;event(11,0,0);}
'''
SUFFIX=r'''
int main(int argc,char **argv) {
 assert(argc==2);mode=(unsigned)strtoul(argv[1],NULL,10);assert(mode<10);
 registers[MBOX_CIN/4]=3;registers[MBOX_COUT/4]=(mode==3 || mode==9) ? 4:3;
 simulated_response.header=mode==2 ? 0x7ff:mode==5 ? 17:0;
 int result=mailbox_send_fpga_config_comp();
#ifdef FES_TEST_OBSERVED
 assert(!fes_nano_isolation_io.active && !fes_nano_isolation_io.polling);
 struct fes_nano_isolation_observation saved=fes_nano_isolation_io;
#endif
 int outside=mailbox_send_cmd(MBOX_JOB_ID,MBOX_CMD_FPGA_CONFIG_COMP,NULL,0U,CMD_CASUAL,NULL,NULL);
#ifdef FES_TEST_OBSERVED
 assert(!memcmp(&saved,&fes_nano_isolation_io,sizeof(saved)));
#endif
 printf("RESULT result=%d outside=%d reads=%u writes=%u delays=%u restarts=%u fills=%u parses=%u gets=%u frees=%u events=%llx cin=%u cout=%u bell=%u lock=%u\n",
 result,outside,reads,writes,delays,restarts,fills,parses,gets,frees,(unsigned long long)events,
 registers[MBOX_CIN/4],registers[MBOX_COUT/4],registers[MBOX_DOORBELL_FROM_SDM/4],mbox_write_lock);
 assert(!mbox_write_lock);return 0;
}
'''
NAMES=('is_mailbox_cmdbuf_full','is_mailbox_cmdbuf_empty','wait_for_mailbox_cmdbuf_empty',
       'write_mailbox_cmd_buffer','fill_mailbox_circular_buffer','mailbox_poll_response_v3',
       'mailbox_poll_response','mailbox_send_cmd','mailbox_send_fpga_config_comp')

class Tests(unittest.TestCase):
    def test_actual_transport_preserves_every_mmio_delay_call_result_and_retirement(self):
        ref=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        source=(ref/trace.MAILBOX).read_bytes()+b'\n'+(FW/'nano_mailbox_observe.inc').read_bytes()+b'\n'+(FW/'nano_mailbox_status.inc').read_bytes()
        observed=trace.observed_mailbox(source)
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);inc=base/'include';(inc/'lib').mkdir(parents=True)
            (inc/'lib/utils_def.h').write_text('#define BIT(n) (1U << (n))\n#define U(n) n##U\n#define ARRAY_SIZE(a) (sizeof(a)/sizeof((a)[0]))\n')
            (inc/'socfpga_mailbox.h').write_bytes((ref/'plat/intel/soc/common/include/socfpga_mailbox.h').read_bytes())
            binaries=[]
            for name,data in [('original',source),('observed',observed)]:
                p=base/(name+'.c');binary=p.with_suffix('')
                p.write_text(PREFIX+('#define FES_TEST_OBSERVED 1\n'+trace.ISOLATION_STATE if name=='observed' else '')+
                             '\n'.join(function(data.decode(),n) for n in NAMES)+SUFFIX)
                r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-Wshadow',
                    '-Wno-sign-compare','-Wno-unused-parameter','-fsanitize=undefined',
                    '-fno-sanitize-recover=all','-I'+str(inc),str(p),'-o',str(binary)],
                    capture_output=True,text=True,timeout=30)
                self.assertEqual(r.returncode,0,r.stderr);binaries.append(binary)
            expected={0:(0,3,1),1:(-2047,1,40),2:(-2047,3,1),3:(-2047,0,0),
                      4:(-4,2,1),5:(-17,3,1),6:(-2047,4,255),7:(-2047,1,41),
                      8:(-2047,1,40),9:(-2047,0,0)}
            for mode,(status,branch,polls) in expected.items():
                with self.subTest(mode=mode):
                    old,new=[subprocess.run([str(b),str(mode)],capture_output=True,text=True,timeout=5) for b in binaries]
                    self.assertEqual(old.returncode,0,old.stderr);self.assertEqual(new.returncode,0,new.stderr)
                    rows=new.stdout.splitlines(keepends=True);self.assertEqual(len(rows),3)
                    self.assertEqual(rows[1],f'FES_NANO_ISOLATION status={status}\n')
                    self.assertIn(f'branch={branch} ',rows[0]);self.assertIn(f'polls={polls} ',rows[0])
                    self.assertEqual(''.join(rows[2:]),old.stdout)
                    if mode in (3,9):self.assertIn('restarts=2 ',rows[2])
                    else:self.assertIn('restarts=0 ',rows[2])

if __name__=='__main__':unittest.main()
