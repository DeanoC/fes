"""START diagnostics never replace configuration/owner/return evidence."""
import re
import sys
from pathlib import Path
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from configuration_trace import extract
from test_nano_paired_tenfoot_trace import Model,paired
from test_nano_program_client import SPEC

NOTE=b''.join(f'NOTICE:  FES_NANO_START phase={i} status={2 if i==7 else 0}\n'.encode() for i in range(12))


class Observed(Model):
    notes=NOTE
    def configure(self,source):
        raw=super().configure(source)
        return re.sub(rb'(?m)^(?:NOTICE:[ \t]+)?FES_NANO_CORE_TRANSFER phase=start [^\n]+\n',
                      lambda m:m[0]+self.notes,raw)


class Isolated(Observed):
    notes=NOTE.replace(b'NOTICE:  FES_NANO_START phase=4 ',
                       b'NOTICE:  FES_NANO_ISOLATION status=0\nNOTICE:  FES_NANO_START phase=4 ')


class Tests(unittest.TestCase):
    def test_complete_model_launch_return_cycle_preserves_required_evidence(self):
        for kind in (Observed,Isolated):
            model=kind(True,'boot-keyboard');client=model.client();result=paired.exercise(client)
            self.assertTrue(result['passed']);self.assertEqual(model.configurations,7)
            self.assertEqual(client.menu.finish()['menu_sessions'],4)

    def test_raw_result_is_bounded_unique_and_inside_the_isolation_call(self):
        line=b'NOTICE:  FES_NANO_ISOLATION status=0\n'
        bad=[Isolated.notes.replace(line,line+line),Isolated.notes.replace(line,line.replace(b'status=0',b'status=-2')),
             Isolated.notes.replace(line,line.replace(b'status=0',b'status=2147483648')),
             Isolated.notes.replace(line,line.replace(b'status=0',b'value=0')),
             line+NOTE,NOTE+line]
        for notes in bad:
            with self.subTest(notes=notes[:80]):
                class Broken(Observed):pass
                Broken.notes=notes
                with self.assertRaises(ValueError):Broken(True,'boot-keyboard').client()

    def test_missing_duplicate_malformed_or_failed_notes_cannot_complete_startup(self):
        lines=NOTE.splitlines(keepends=True)
        bad=[NOTE.replace(line,b'') for line in lines]+[NOTE+lines[-1],NOTE.replace(b'phase=8',b'phase=18'),
             NOTE.replace(b'phase=1 status=0',b'phase=1 status=-1'),NOTE.replace(b'phase=6 status=0',b'phase=6 status=-12'),
             NOTE.replace(b'phase=11 status=0',b'phase=11 status=4'),NOTE.replace(b'phase=3',b'unknown=3'),
             NOTE.replace(b'phase=0 status=0',b'phase=0 status=2147483648'),lines[1]+lines[0]+b''.join(lines[2:])]
        for notes in bad:
            with self.subTest(notes=notes[:90]):
                class Broken(Observed):pass
                Broken.notes=notes;model=Broken(True,'boot-keyboard')
                with self.assertRaises(ValueError):model.client()

    def test_diagnostics_cannot_substitute_a_missing_original_transfer_marker(self):
        model=Observed(True,'boot-keyboard')
        for phase in (b'arm',b'start',b'write',b'poll',b'status',b'complete'):
            raw=re.sub(rb'(?m)^(?:NOTICE:[ \t]+)?FES_NANO_CORE_TRANSFER phase='+phase+rb' [^\n]+\n',b'',model.initial)
            with self.subTest(phase=phase),self.assertRaises(ValueError):model.client(initial=raw)

    def test_diagnostics_outside_start_are_rejected(self):
        model=Observed(True,'boot-keyboard')
        raw=model.initial.replace(NOTE,b'')
        for data in (NOTE+raw,raw+NOTE):
            with self.subTest(tail=data[-30:]),self.assertRaises(ValueError):model.client(initial=data)

    def test_failed_vendor_prefix_stays_failed_with_raw_results_retained(self):
        cfg=SPEC['configuration'];owner=0x17;generation=4
        def core(phase):
            return (f'NOTICE:  FES_NANO_CORE_TRANSFER phase={phase} owner={owner:x} generation={generation} completed=0 pending=0 '
                    f'total={cfg["core_size"]} base={cfg["base"]:x}\n').encode()
        notes=b''.join(f'NOTICE:  FES_NANO_START phase={i} status={-12 if i==4 else 4 if i in (9,10) else 0}\n'.encode() for i in (0,1,2,3,4,9,10))
        r=extract(core('arm')+core('start')+notes+core('FAIL')+b'NOTICE:  FES_NANO_START phase=11 status=4\n',cfg,generation=generation)
        self.assertFalse(r['complete']);self.assertTrue(r['errors']);self.assertFalse(r['start_observation']['complete'])
        self.assertEqual(r['start_observation']['records'][4]['status'],-12)
        self.assertFalse(r['start_observation']['hardware_authority'])

        raw_note=b'NOTICE:  FES_NANO_ISOLATION status=-12\n'
        notes=notes.replace(b'NOTICE:  FES_NANO_START phase=4 status=-12\n',
                            raw_note+b'NOTICE:  FES_NANO_START phase=4 status=-1\n')
        r=extract(core('arm')+core('start')+notes+core('FAIL')+b'NOTICE:  FES_NANO_START phase=11 status=4\n',cfg,generation=generation)
        self.assertEqual(r['start_observation']['isolation']['status'],-12)
        self.assertFalse(r['complete']);self.assertTrue(r['errors']);self.assertFalse(r['start_observation']['hardware_authority'])


if __name__=='__main__':unittest.main()
