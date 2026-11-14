"""Exercise the actual trace console and UploadSession without a UART."""
from pathlib import Path
import contextlib
import io
import sys
import types
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import upload
import recovery_return_trace as trace
from recovery_trace_console import TraceConsole
from upload_transport import Console
from test_nano_upload import Model


class TraceConsoleTests(unittest.TestCase):
    def receiver(self):
        c=object.__new__(TraceConsole);c.retired=False;c.return_traces=[];c.parked=True
        return c

    def test_complete_trace_still_requires_base_nonce_and_prompt_success(self):
        lines=''.join(f'FES_RECOVERY_RETURN phase={p}\n' for p in trace.PHASES)
        for failure in (ValueError('nonce failed'),TimeoutError('no prompt')):
            c=self.receiver()
            with patch.object(Console,'command',side_effect=failure) as call:
                with self.assertRaises(type(failure)):c.command('tftpboot 82000000 U123ABC.BIN',90)
                self.assertTrue(c.retired);before=call.call_count
                with self.assertRaises(ValueError):c.command('fatwrite mmc 0:1 82000000 FES/UPLOAD.BIN 100')
                with self.assertRaises(ValueError):c.park()
                self.assertEqual(call.call_count,before)
        c=self.receiver()
        with patch.object(Console,'command',return_value=lines):
            self.assertEqual(c.command('tftpboot 82000000 U123ABC.BIN',90),lines)
        self.assertFalse(c.retired);self.assertTrue(c.return_traces[0]['trace']['complete'])

    def test_foreign_commands_or_bounds_reject_before_transmission(self):
        for command,timeout in [('tftpboot 83000000 U123ABC.BIN',90),('tftpboot 82000000 other',90),
                                ('tftpboot 82000000 U123ABC.BIN',91)]:
            with self.subTest(command=command,timeout=timeout),patch.object(Console,'command') as call:
                c=self.receiver()
                with self.assertRaises(ValueError):c.command(command,timeout)
                call.assert_not_called();self.assertTrue(c.retired)

    def test_park_failure_and_close_retire_before_descriptor_reuse(self):
        c=self.receiver()
        with patch.object(Console,'park',side_effect=TimeoutError('no prompt')):
            with self.assertRaises(TimeoutError):c.park()
        with patch.object(Console,'command') as call:
            with self.assertRaises(ValueError):c.command('version')
            call.assert_not_called()
        with patch.object(Console,'close') as close:
            c.close();c.close();self.assertEqual(close.call_count,1)
        self.assertTrue(c.retired)

    def test_missing_or_damaged_return_markers_prevent_the_actual_SD_write(self):
        faults=['pass','missing','duplicate','reordered','foreign','partial','outside']
        faults += ['stop-after-'+p for p in trace.PHASES[:-1]]
        for fault in faults:
            with self.subTest(fault=fault):
                kit=dict(manifest=b'card metadata',fit=b'qualified root FIT',fit_sha256=upload.sha(b'qualified root FIT'),prefix_sha256=upload.sha(b'known prefix'))
                payload=b'new diagnostic payload'*20;model=Model(kit,payload);model.parked=True
                c=self.receiver();original=model.command
                def command(_self,value,timeout):
                    output=original(value,timeout)
                    if value.startswith('tftpboot'):
                        phases=list(trace.PHASES)
                        if fault=='missing':phases=[]
                        if fault=='duplicate':phases.append(phases[-1])
                        if fault=='reordered':phases[2],phases[3]=phases[3],phases[2]
                        if fault=='foreign':phases[3]='foreign'
                        if fault.startswith('stop-after-'):phases=phases[:phases.index(fault[len('stop-after-'):])+1]
                        output+='\n'+''.join('FES_RECOVERY_RETURN phase='+p+'\n' for p in phases)
                        if fault=='partial':output=output.rstrip('\n')
                    elif fault=='outside':output+='FES_RECOVERY_RETURN phase=begin\n'
                    return output
                model.command=lambda value,timeout=15: c.command(value,timeout)
                class Server:
                    complete=True
                    thread=types.SimpleNamespace(join=lambda timeout:None)
                    def __init__(self,*args):pass
                    def start(self):pass
                    def close(self):pass
                receipt=dict(commands=[])
                with patch.object(Console,'command',command),patch.object(upload.time,'sleep'),contextlib.redirect_stdout(io.StringIO()):
                    session=upload.UploadSession(model,kit,'192.168.10.202','02:fe:25:08:cb:01',owns=lambda:True,serve=Server)
                    try:
                        if fault=='pass':session.upload(payload,receipt);self.assertEqual(receipt['status'],'verified')
                        else:
                            with self.assertRaises(ValueError):session.upload(payload,receipt)
                    finally:session.close()
                self.assertEqual(model.writes,int(fault=='pass'))
                self.assertEqual(c.retired,fault!='pass')


if __name__=='__main__':unittest.main()
