"""Source-inverse and executable return observations; never a hardware claim."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
def module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT/'platform/embedded/nano'/f'{name}.py')
    result = importlib.util.module_from_spec(spec); spec.loader.exec_module(result)
    return result
trace, recovery = module('recovery_return_trace'), module('recovery')


def fixture(path):
    shutil.copytree(ROOT/'tests/fixtures/nano-recovery-phy', path)
    shutil.copytree(ROOT/'tests/fixtures/nano-recovery-return', path, dirs_exist_ok=True)
    return recovery.prepare_phy_sources(path)


class RecoveryReturnTests(unittest.TestCase):
    def test_console_alias_resolves_to_UART1_and_matches_independent_firmware_contract(self):
        fixture=json.loads((ROOT/'tests/fixtures/nano-recovery-return/console.json').read_text())
        for fault in ('pass','UART0','disabled','byte-width','shift','baud','foreign-node','helper-address'):
            data=copy.deepcopy(fixture)
            if fault=='UART0':data['reg']='10c02000 100'
            if fault=='disabled':data['status']='disabled'
            if fault=='byte-width':data['reg-io-width']='1'
            if fault=='shift':data['reg-shift']='0'
            if fault=='baud':data['stdout']='serial0:9600n8'
            if fault=='foreign-node':data['node']='../../outside'
            def query(argv,**_):
                node,name=argv[-2:]
                if node=='/chosen':return data['stdout']+'\n'
                if node=='/aliases':return data['node']+'\n'
                self.assertEqual(node,data['node']);return data[name]+'\n'
            with self.subTest(fault=fault),patch('subprocess.check_output',query),patch.object(trace,'UART_DATA',0x10c02000 if fault=='helper-address' else 0x10c02100):
                if fault=='pass':
                    actual=trace.check_console('fdtget','board.dtb');self.assertEqual(actual['data_address'],0x10c02100)
                    self.assertEqual(actual['resolved_node'],'/soc/serial1@10c02100')
                else:
                    with self.assertRaises(ValueError):trace.check_console('fdtget','board.dtb')
        header=(ROOT/'platform/embedded/firmware/nano_firmware_ram.h').read_text()
        for key,value in [('DATA',trace.UART_DATA),('STATUS',trace.UART_STATUS)]:
            match=re.search(r'#define FES_NANO_FW_RAM_UART_'+key+r' (0x[0-9a-f]+)ULL',header)
            self.assertIsNotNone(match);self.assertEqual(int(match[1],16),value)
            self.assertIn(hex(value)+'UL',trace.HELPER)

    def test_inverse_preserves_exact_original_phy_and_network_operations(self):
        with tempfile.TemporaryDirectory() as d:
            source = Path(d)/'source'; phy = fixture(source)
            original = {n: (source/n).read_text() for n in trace.PINNED}
            record = trace.prepare(source)
            self.assertEqual(trace.reconstruct(source, record), original)
            trace.check_phy_sources(source, record, phy, recovery)
            with self.assertRaises(ValueError): trace.prepare(source)

    @unittest.skipUnless(shutil.which('cc'), 'host C compiler required')
    def test_actual_header_drives_millisecond_busy_drain_regression(self):
        # The existing operation-equivalence fixture deliberately models the
        # intended 100-ms limit. This regression also injects the actual header
        # value, which exposed the vendor's 100000-ms mismatch on hardware.
        with tempfile.TemporaryDirectory() as d:
            directory=Path(d); source=directory/'source'; fixture(source)
            before={n:(source/n).read_text() for n in trace.PINNED}
            trace.prepare(source)
            after={n:(source/n).read_text() for n in trace.PINNED}
            self.assertEqual(before['drivers/net/dwc_eth_xgmac.c'].count('XGMAC_TIMEOUT_100MS'),4)
            self.assertEqual(before['drivers/net/dwc_eth_xgmac.c'].count('get_timer(start_time) < XGMAC_TIMEOUT_100MS'),3)
            self.assertIn('XGMAC_TIMEOUT_100MS, true',before['drivers/net/dwc_eth_xgmac.c'])
            results=[]
            for index,files in enumerate((before,after)):
                driver=files['drivers/net/dwc_eth_xgmac.c']; start=driver.index('static void xgmac_stop(')
                (directory/'stop.inc').write_text(driver[start:driver.index('\nstatic int xgmac_send(',start)])
                net=files['net/net.c']; start=net.index('\t\tcase NETLOOP_SUCCESS:'); end=net.index('\t\tcase NETLOOP_FAIL:',start)
                done=net.index('done:\n',end); tail=net[done:net.index('\n}',done)+2]
                (directory/'success.inc').write_text('static int net_loop(enum proto_t protocol)\n{\nint ret=0;\nswitch (NETLOOP_SUCCESS) {\n'+net[start:end]+'}\n'+tail+'\n')
                command=files['cmd/net.c']; start=command.index('\tsize = net_loop(proto);'); end=command.index('\n}',start)+2
                (directory/'command.inc').write_text('static int command_under_test(enum proto_t proto)\n{\nint size, rcode=0; struct cmd_tbl table={0}; struct cmd_tbl *cmdtp=&table; char *argv[]={"tftpboot"};\n'+command[start:end]+'\n')
                definition=re.search(r'^#define XGMAC_TIMEOUT_100MS\s+(\d+)$',files[trace.TIMEOUT_HEADER],re.M)
                self.assertIsNotNone(definition)
                harness=(ROOT/'platform/embedded/firmware/tests/nano_recovery_return_test.c').read_text()
                literal='#define XGMAC_TIMEOUT_100MS 100\n'
                self.assertEqual(harness.count(literal),1)
                harness=harness.replace(literal,'#define XGMAC_TIMEOUT_100MS '+definition[1]+'\n')
                c=directory/'actual-header.c'; c.write_text(harness); executable=directory/('actual-header-'+str(index))
                subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                    '-I',str(directory),str(c),'-o',str(executable)],check=True,capture_output=True,timeout=30)
                runs={}
                for mode in (0,1):
                    runs[mode]=subprocess.run([str(executable),str(mode),'0'],check=True,capture_output=True,text=True,timeout=5).stdout
                results.append(runs)
            # get_timer advances one modeled millisecond per call. Permanently
            # busy TX and RX queues must stop waiting within two 100-ms budgets.
            elapsed=lambda text:int(re.search(r'ticks=(\d+)',text)[1])
            self.assertGreater(elapsed(results[0][1]),90000)
            self.assertLessEqual(elapsed(results[1][1]),202)
            self.assertEqual(elapsed(results[0][0]),elapsed(results[1][0]))
            for runs in results:
                for output in runs.values():self.assertIn('dma=0/0 mac=0/0',output)
            # Expiry still follows the original disable sequence; it is not
            # evidence that the modeled nonempty queues became idle.
            self.assertTrue(trace.decode(results[1][1])['complete'])

    def test_foreign_originals_reject_before_any_write(self):
        for name in trace.PINNED:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as d:
                source = Path(d)/'source'; fixture(source)
                (source/name).write_text((source/name).read_text()+'/* foreign */\n')
                before = {str(p.relative_to(source)): p.read_bytes() for p in source.rglob('*') if p.is_file()}
                with self.assertRaises(ValueError): trace.prepare(source)
                self.assertEqual(before, {str(p.relative_to(source)): p.read_bytes() for p in source.rglob('*') if p.is_file()})

    def test_self_consistent_tampering_cannot_change_calls_helper_or_limits(self):
        for name in [*trace.PINNED, trace.HEADER_PATH, trace.HELPER_PATH]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as d:
                source = Path(d)/'source'; fixture(source); record = trace.prepare(source)
                p=source/name; p.write_text(p.read_text()+'/* changed */\n')
                record['prepared'][name]=hashlib.sha256(p.read_bytes()).hexdigest()
                with self.assertRaises(ValueError): trace.reconstruct(source, record)
        for record in ({}, None, dict(scope='forged', originals={}, prepared={})):
            with self.subTest(record=record), self.assertRaises(ValueError): trace.reconstruct('/nonexistent', record)

    def test_decoder_retains_partial_progress_and_rejects_bad_order(self):
        lines = [f'FES_RECOVERY_RETURN phase={p}\r\n' for p in trace.PHASES]
        for n in range(len(lines)+1):
            result=trace.decode('unrelated console\n'+''.join(lines[:n]))
            self.assertTrue(result['valid']); self.assertEqual(result['complete'], n==len(lines))
            self.assertEqual(result['phases'], list(trace.PHASES[:n]))
        for n in range(len(lines)):
            for bad in (lines[n] + lines[n], 'FES_RECOVERY_RETURN phase=foreign\n'):
                self.assertFalse(trace.decode(''.join(lines[:n])+bad)['complete'])
        self.assertFalse(trace.decode(''.join(reversed(lines)))['valid'])
        self.assertFalse(trace.decode(''.join(lines)+lines[0])['valid'])
        result=trace.decode(''.join(lines[:3])+'FES_RECOVERY_RETURN phase=filesize-')
        self.assertTrue(result['partial']); self.assertFalse(result['complete'])
        with self.assertRaises(ValueError): trace.decode('x'*(1+(1<<20)))

    def test_explicit_builder_selection_is_required(self):
        r=subprocess.run(['python3', str(ROOT/'platform/embedded/firmware/build_terasic_uboot.py'),
            '--reference-repo','/nonexistent','--bl31-build','/nonexistent','--cross-prefix','/nonexistent-',
            '--output','/nonexistent','--network-return-trace'],capture_output=True,text=True,timeout=5)
        self.assertEqual(r.returncode,2); self.assertIn('require the separate network recovery profile',r.stderr)

    @unittest.skipUnless(shutil.which('cc'), 'host C compiler required')
    def test_actual_uart_helper_has_bounded_polls_strings_and_inactive_scope(self):
        with tempfile.TemporaryDirectory() as d:
            directory=Path(d)
            for name,content in {'linux/types.h':'#include <stdbool.h>\n#include <stdint.h>\n',
                                 'asm/io.h':'uint32_t readl(const void *);\nvoid writel(uint32_t,void *);\n',
                                 'fes_recovery_return_trace.h':trace.HEADER,'helper.inc':trace.HELPER}.items():
                p=directory/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(content)
            executable=directory/'uart'
            subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                '-I',str(directory),str(ROOT/'platform/embedded/firmware/tests/nano_recovery_return_uart_test.c'),'-o',str(executable)],check=True,capture_output=True,timeout=30)
            for mode in range(6):
                with self.subTest(mode=mode):
                    subprocess.run([str(executable),str(mode)],check=True,capture_output=True,text=True,timeout=5)

    @unittest.skipUnless(shutil.which('cc'), 'host C compiler required')
    def test_actual_vendor_success_return_and_stop_calls_match_with_faults(self):
        with tempfile.TemporaryDirectory() as d:
            directory=Path(d); source=directory/'source'; fixture(source)
            original={n:(source/n).read_text() for n in trace.PINNED}; trace.prepare(source)
            outputs=[]
            for instrumented in (False,True):
                files={n:(source/n).read_text() for n in trace.PINNED} if instrumented else original
                driver=files['drivers/net/dwc_eth_xgmac.c']; start=driver.index('static void xgmac_stop(')
                (directory/'stop.inc').write_text(driver[start:driver.index('\nstatic int xgmac_send(',start)])
                net=files['net/net.c']; start=net.index('\t\tcase NETLOOP_SUCCESS:'); end=net.index('\t\tcase NETLOOP_FAIL:',start)
                done=net.index('done:\n',end); tail=net[done:net.index('\n}',done)+2]
                (directory/'success.inc').write_text('static int net_loop(enum proto_t protocol)\n{\nint ret=0;\nswitch (NETLOOP_SUCCESS) {\n'+net[start:end]+'}\n'+tail+'\n')
                command=files['cmd/net.c']; start=command.index('\tsize = net_loop(proto);');end=command.index('\n}',start)+2
                (directory/'command.inc').write_text('static int command_under_test(enum proto_t proto)\n{\nint size, rcode=0; struct cmd_tbl table={0}; struct cmd_tbl *cmdtp=&table; char *argv[]={"tftpboot"};\n'+command[start:end]+'\n')
                executable=directory/str(instrumented)
                subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                    '-I',str(directory),str(ROOT/'platform/embedded/firmware/tests/nano_recovery_return_test.c'),'-o',str(executable)],check=True,capture_output=True,timeout=30)
                runs={}
                for mode in range(6):
                    r=subprocess.run([str(executable),str(mode),'0'],check=True,capture_output=True,text=True,timeout=5)
                    runs[(mode,0)]=r.stdout
                count=sum(line.startswith('OP ') for line in runs[(0,0)].splitlines())
                for fault in range(1,count+1):
                    r=subprocess.run([str(executable),'0',str(fault)],check=True,capture_output=True,text=True,timeout=5)
                    runs[(0,fault)]=r.stdout
                outputs.append(runs)
            self.assertEqual(outputs[0].keys(),outputs[1].keys())
            for key in outputs[0]:
                # Observe every modeled register/call operation and final state.
                remove=lambda s:'\n'.join(l for l in s.splitlines() if not l.startswith('FES_RECOVERY_RETURN'))
                self.assertEqual(remove(outputs[0][key]),remove(outputs[1][key]),key)
            self.assertTrue(trace.decode(outputs[1][(0,0)])['complete'])
            self.assertTrue(trace.decode(outputs[1][(1,0)])['complete'])  # loop return is not drain proof
            self.assertTrue(trace.decode(outputs[1][(4,0)])['complete'])  # nonzero command still rejects
            self.assertIn('RESULT rc=1',outputs[1][(4,0)])
            for key,text in outputs[1].items():
                if key[1] or key[0] in (2,3,5): self.assertFalse(trace.decode(text)['complete'],key)


if __name__ == '__main__': unittest.main()
