"""Owned attachment, polling and detach failure paths without a device."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'platform/embedded/nano'))
import jtag_server_trace as trace


class Child:
    pid = 5678
    def __init__(self):
        self.code = None
        self.signals = []
        self.waits = []
    def poll(self):return self.code
    @property
    def returncode(self):return self.code
    def send_signal(self, value):self.signals.append(value)
    def wait(self, timeout):
        self.waits.append(timeout);self.code = 0;return 0
    def kill(self):self.signals.append('kill');self.code = -9


class TraceTests(unittest.TestCase):
    def setup_case(self, directory):
        root = Path(directory);record = root/'transport.json'
        args = [trace.LAUNCHER, '--foreground', '--no-config', '--auto-detect-filter',
                'TRWU8CBL', '--port', '0', '--port-file', str(root/'private-port'),
                '--pid-file', str(root/'private-pid')]
        record.write_text(json.dumps(dict(owned_pid=1234, process_start_ticks='100',
            serial='TRWU8CBL', status='adapter-ready', server_args=args)))
        identity = dict(pid=1234, start_ticks='100', uid=1000, state='S',
                        executable=str(trace.SERVER), argv=[str(trace.SERVER), *args[1:]],
                        tracer_pid=0)
        child = Child();calls = [];owner = [True];live = [False]
        def snapshot(pid):
            calls.append(pid)
            if pid == child.pid:return dict(start_ticks='200')
            self.assertEqual(pid, 1234)
            return {**identity, 'tracer_pid':child.pid if live[0] and child.poll() is None else identity['tracer_pid']}
        def launch(argv, **options):
            calls.append(argv)
            self.assertNotIn('--kill-on-exit', argv)
            self.assertNotIn('start_new_session', options)
            self.assertEqual(argv[-2:], ['-p', '1234'])
            live[0] = True
            return child
        item = trace.JtagServerTrace(root/'trace', server_record=record,
                                     server_sha256='a'*64, owns=lambda:owner[0])
        return item, child, identity, owner, calls, snapshot, launch

    def mocks(self, snapshot, launch):
        from contextlib import ExitStack
        stack = ExitStack()
        stack.enter_context(patch.object(trace.os, 'geteuid', return_value=0))
        stack.enter_context(patch.object(trace, '_sha', return_value='a'*64))
        stack.enter_context(patch.object(trace, '_snapshot', side_effect=snapshot))
        stack.enter_context(patch.object(trace.subprocess, 'Popen', side_effect=launch))
        return stack

    def test_attach_poll_detach_exact_owned_pid_and_no_authority(self):
        with tempfile.TemporaryDirectory() as directory:
            item, child, _, _, calls, snapshot, launch = self.setup_case(directory)
            with self.mocks(snapshot, launch):
                self.assertEqual(item.start()['pid'], 1234)
                self.assertEqual(item.check()['start_ticks'], '100')
                item.close();item.close();receipt = item.receipt()
                self.assertTrue(receipt['attached'] and receipt['tracer_stopped'] and receipt['target_detached'])
                self.assertTrue(receipt['ownership_verified'])
                self.assertFalse(receipt['tracer_exited_before_close'])
                self.assertTrue(all(receipt[k] is False for k in ('hardware_acceptance','programming_authority','reset_completed','recovery_authority')))
                self.assertEqual(child.signals, [trace.signal.SIGINT])
                self.assertEqual(child.waits, [5])
                with self.assertRaises(ValueError):item.start()
            self.assertEqual(sum(isinstance(call,list) for call in calls), 1)
            saved = json.loads((item.directory/'owned-worker.json').read_text())
            self.assertEqual((saved['pid'],saved['start_ticks']), (5678,'200'))

    def test_bad_epoch_uid_executable_arguments_or_existing_tracer_never_attach(self):
        changes = [('start_ticks','101'),('uid',0),('executable','/bin/other'),
                   ('argv',['jtagd','--auto-detect-filter','OTHER']),('tracer_pid',42)]
        for field,value in changes:
            with self.subTest(field=field),tempfile.TemporaryDirectory() as directory:
                item, child, identity, _, calls, snapshot, launch = self.setup_case(directory)
                identity[field] = value
                with self.mocks(snapshot, launch),self.assertRaises(ValueError):item.start()
                self.assertFalse(any(isinstance(call,list) for call in calls))
                self.assertEqual(child.signals, [])

    def test_vendor_launcher_basename_and_absolute_argv_zero_only(self):
        for argv_zero in ('jtagd', str(trace.SERVER), '/other/jtagd'):
            with self.subTest(argv_zero=argv_zero),tempfile.TemporaryDirectory() as directory:
                item, child, identity, _, calls, snapshot, launch = self.setup_case(directory)
                identity['argv'][0]=argv_zero
                with self.mocks(snapshot,launch):
                    if argv_zero=='/other/jtagd':
                        with self.assertRaises(ValueError):item.start()
                        self.assertEqual(child.signals, [])
                    else:item.start();item.close()

    def test_wrong_record_or_changed_executable_never_attach(self):
        for field,value in [('serial','OTHER'),('status','stopped'),('server_args',[]),('owned_pid',True)]:
            with self.subTest(field=field),tempfile.TemporaryDirectory() as directory:
                item, _, _, _, calls, snapshot, launch = self.setup_case(directory)
                r=json.loads(item.record_path.read_text());r[field]=value;item.record_path.write_text(json.dumps(r))
                with self.mocks(snapshot, launch),self.assertRaises(ValueError):item.start()
                self.assertFalse(any(isinstance(call,list) for call in calls))
        with tempfile.TemporaryDirectory() as directory:
            item, _, _, _, calls, snapshot, launch = self.setup_case(directory)
            with self.mocks(snapshot, launch),patch.object(trace,'_sha',return_value='b'*64),self.assertRaises(ValueError):item.start()
            self.assertFalse(any(isinstance(call,list) for call in calls))

    def test_lost_owner_is_sticky_but_cleanup_still_detaches(self):
        with tempfile.TemporaryDirectory() as directory:
            item, child, _, owner, _, snapshot, launch = self.setup_case(directory)
            with self.mocks(snapshot, launch):
                item.start();owner[0]=False
                with self.assertRaises(ValueError):item.check()
                owner[0]=True
                with self.assertRaises(ValueError):item.check()
                item.close()
                self.assertEqual(child.signals,[trace.signal.SIGINT])
                self.assertFalse(item.receipt()['ownership_verified'])

    def test_nonroot_or_failed_owner_never_attach(self):
        for mode in ('uid', 'owner', 'exception'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                item, _, _, owner, calls, snapshot, launch = self.setup_case(directory)
                if mode=='owner':owner[0]=False
                if mode=='exception':item.owns=lambda:(_ for _ in ()).throw(RuntimeError('lost owner'))
                with self.mocks(snapshot, launch),patch.object(trace.os,'geteuid',return_value=1000 if mode=='uid' else 0),self.assertRaises((ValueError,RuntimeError)):
                    item.start()
                self.assertFalse(any(isinstance(call,list) for call in calls))

    def test_attachment_timeout_detaches_owned_tracer(self):
        with tempfile.TemporaryDirectory() as directory:
            item, child, _, _, _, snapshot, launch = self.setup_case(directory)
            def unattached(pid):
                value=snapshot(pid)
                if pid==1234:value['tracer_pid']=0
                return value
            counter=[0]
            def now():counter[0]+=2;return counter[0]
            with self.mocks(unattached,launch),patch.object(trace.time,'monotonic',side_effect=now),self.assertRaises(TimeoutError):
                item.start()
            self.assertTrue(item.closed);self.assertEqual(child.signals,[trace.signal.SIGINT])

    def test_early_tracer_exit_and_budget_or_output_exhaustion_stop_polling(self):
        for mode in ('exit','time','size'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                item, child, _, _, _, snapshot, launch = self.setup_case(directory)
                with self.mocks(snapshot, launch):
                    item.start()
                    if mode=='exit':child.code=1
                    if mode=='time':item.started_at-=trace.MAX_SECONDS
                    if mode=='size':
                        with (item.directory/'usb-ioctl.strace').open('wb') as f:f.truncate(trace.MAX_BYTES)
                    with self.assertRaises((ValueError,TimeoutError)):item.check()
                    item.close()
                    self.assertEqual(item.receipt()['tracer_exited_before_close'],mode=='exit')

    def test_attachment_failure_and_record_mutation_retain_and_detach(self):
        for mode in ('attach-exit','record-change','target-change'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                item, child, identity, _, _, snapshot, launch = self.setup_case(directory)
                with self.mocks(snapshot, launch):
                    if mode=='attach-exit':
                        def exited(*args,**kwargs):child.code=1;return child
                        with patch.object(trace.subprocess,'Popen',side_effect=exited),self.assertRaises(ValueError):item.start()
                        self.assertTrue(item.closed)
                    else:
                        item.start()
                        if mode=='record-change':item.record_path.write_text(item.record_path.read_text()+'\n')
                        else:identity['start_ticks']='999'
                        with self.assertRaises(ValueError):item.check()
                        with self.assertRaises(ValueError):item.close()
                        self.assertEqual(child.signals,[trace.signal.SIGINT])
                        with self.assertRaises(ValueError):item.receipt()

    def test_stubborn_tracer_kill_does_not_signal_target(self):
        with tempfile.TemporaryDirectory() as directory:
            item, child, _, _, _, snapshot, launch = self.setup_case(directory)
            with self.mocks(snapshot, launch):
                item.start();real=child.wait;count=[0]
                def wait(timeout):
                    count[0]+=1
                    if count[0]==1:raise subprocess.TimeoutExpired('strace', timeout)
                    return real(timeout)
                child.wait=wait
                item.close();self.assertEqual(child.signals,[trace.signal.SIGINT,'kill'])

    def test_record_symlink_and_existing_directory_cannot_reuse_evidence(self):
        for mode in ('symlink','directory'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                item, _, _, _, calls, snapshot, launch = self.setup_case(directory)
                if mode=='symlink':
                    p=item.record_path.with_suffix('.saved');item.record_path.rename(p);item.record_path.symlink_to(p)
                else:item.directory.mkdir()
                with self.mocks(snapshot, launch),self.assertRaises((ValueError,FileExistsError)):item.start()
                self.assertFalse(any(isinstance(call,list) for call in calls))


if __name__=='__main__':unittest.main()
