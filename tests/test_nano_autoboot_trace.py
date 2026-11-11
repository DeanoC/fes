"""Execute the pinned countdown producer, then verify explicit startup selection."""
import copy
import hashlib
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

from test_nano_native_trace import trace,fixture,BUILD,SEED
from test_nano_paired_tenfoot_trace import Model,PROGRAMS,SPEC

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import paired_tenfoot_trace as paired
import startup


class AutobootTraceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();base=Path(cls.temp.name)
        vendor=ROOT/'tests/fixtures/nano-autoboot/abortboot.c'
        if hashlib.sha256(vendor.read_bytes()).hexdigest()!='cea9669ec56a781fda8ee12f2739c594d2944866e56675dfb59b97ace84ddc7e':raise AssertionError('pinned countdown fixture changed')
        source=base/'countdown.c';binary=base/'countdown'
        source.write_text('''#include <stdio.h>
#include <stdlib.h>
#define putc putchar
#define IS_ENABLED(x) 0
#define CONFIG_AUTOBOOT_USE_MENUKEY 0
static int mode,calls,menukey;
static unsigned long ticks;
static int tstc(void) {calls++;return mode==1 || (mode==2 && calls==3);}
static int getkey(void) {return 27;}
#define getchar getkey
static unsigned long get_timer(unsigned long start) {ticks+=250;return ticks-start;}
static void udelay(unsigned long us) {(void)us;}
'''+vendor.read_text()+'''
int main(int argc,char **argv) {if(argc!=2)return 2;mode=atoi(argv[1]);return abortboot_single_key(2);}
''')
        compiled=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror',str(source),'-o',str(binary)],capture_output=True)
        if compiled.returncode:raise AssertionError(compiled.stderr.decode())
        cls.output=[]
        for mode in range(3):
            r=subprocess.run([str(binary),str(mode)],capture_output=True,check=False)
            if r.returncode!=int(mode!=0):raise AssertionError(r)
            cls.output.append(r.stdout)

    @classmethod
    def tearDownClass(cls):cls.temp.cleanup()

    def decode(self,data,policy=None):
        return trace.decode_native_boot(data,BUILD,SEED,**({'boot_startup':policy} if policy is not None else {}))

    def test_pinned_producer_complete_and_manual_default(self):
        raw=fixture().replace('SOCFPGA_AGILEX5 # fesboot\r\n',self.output[0].decode())
        self.assertTrue(self.decode(raw,startup.policy(True))['complete'])
        self.assertFalse(self.decode(raw)['complete'])
        self.assertTrue(self.decode(fixture())['complete'])
        self.assertFalse(self.decode(fixture(),startup.policy(True))['complete'])

    def test_abort_wrong_delay_duplicate_or_manual_prompt_is_not_autoboot_success(self):
        marker='SOCFPGA_AGILEX5 # fesboot\r\n'
        good=self.output[0].decode()
        values=[x.decode() for x in self.output[1:]]+[good.replace('  2 ','  3 '),good+good,
                good+'SOCFPGA_AGILEX5 # fesboot\r\n',good.replace('\b',''),'',good.replace(' 1 ',' 0 ')]
        for value in values:
            with self.subTest(value=value):
                self.assertFalse(self.decode(fixture().replace(marker,value),startup.policy(True))['complete'])

    def test_split_countdown_cannot_grant_early_loader_readiness(self):
        raw=fixture().replace('SOCFPGA_AGILEX5 # fesboot\r\n',self.output[0].decode())
        end=raw.index(self.output[0].decode())+len(self.output[0])
        start=raw.index('Hit any key')
        for split in range(start,end):
            r=self.decode(raw[:split],startup.policy(True))
            self.assertFalse(r['complete']);self.assertFalse(r['loader_ready'])
        self.assertTrue(self.decode(raw[:end],startup.policy(True))['loader_ready'])

    def test_invalid_policy_rejects_before_decoding(self):
        for value in ({},dict(startup.policy(True),bootdelay=-2),dict(startup.policy(True),extra=1)):
            with self.assertRaises(ValueError):self.decode(fixture(),value)

    def test_paired_core_launch_and_return_use_the_selected_autoboot_prefix(self):
        m=Model(True)
        initial=re.sub(rb'(?m)^SOCFPGA_AGILEX5 # fesboot\r?\n',lambda _:self.output[0],m.initial)
        self.assertNotEqual(initial,m.initial)
        options=dict(build_record=m.profile,build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],
                     atf=SPEC['atf'],package=SPEC['package'],programs=copy.deepcopy(PROGRAMS),
                     exchange=m.exchange,owns=lambda:m.service.owned,now=lambda:m.service.clock,shared_usb=m.policy)
        with self.assertRaises(ValueError):paired.PairedTenfootClient(initial,**options)
        client=paired.PairedTenfootClient(initial,boot_startup=startup.policy(True),**options)
        self.assertEqual(client.boot['startup_policy'],startup.policy(True))
        client.key('launch',0);client.key('stop');client.key('focus',1);client.key('launch',1);client.key('quit')
        self.assertTrue(client.retired);self.assertEqual(client.loads,2)
        self.assertNotIn(b'fesboot',m.keys)


if __name__=='__main__':unittest.main()
