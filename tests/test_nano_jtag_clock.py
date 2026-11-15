"""Faults must restore both cable properties and never reopen a clock lifetime."""
import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('nano_jtag_clock',Path(__file__).resolve().parents[1]/'platform/embedded/nano/jtag_clock.py')
clock=importlib.util.module_from_spec(spec);spec.loader.exec_module(clock)


class Link:
    def __init__(self, automatic=1):
        self.values=dict(JtagClock='15M',JtagClockAutoAdjust=automatic)
        self.owned=True;self.events=[];self.writes=0;self.fail_write=None;self.after_apply=False
        self.fail_read=None;self.reads=0

    def guard(self):return self.owned

    def read(self,name):
        self.events.append(('read',name));self.reads+=1
        if self.reads==self.fail_read:raise OSError('read failed')
        return self.values[name]

    def write(self,name,value):
        self.events.append(('write',name,value));self.writes+=1
        if self.writes==self.fail_write and not self.after_apply:raise OSError('write failed')
        self.values[name]=value
        if self.writes==self.fail_write:raise OSError('write applied then failed')

    def session(self):return clock.FixedClock(self.guard,self.read,self.write)


class ClockTests(unittest.TestCase):
    def test_success_restores_both_original_auto_modes_and_cannot_restart(self):
        for automatic in (0,1):
            with self.subTest(automatic=automatic):
                link=Link(automatic);session=link.session();session.start()
                self.assertEqual(session.verify(),dict(auto_adjust=0,clock='3M'))
                self.assertEqual(link.values,dict(JtagClock='3M',JtagClockAutoAdjust=0))
                self.assertTrue(session.restore()['restored'])
                self.assertEqual(link.values,dict(JtagClock='15M',JtagClockAutoAdjust=automatic))
                events=list(link.events);session.restore();self.assertEqual(link.events,events)
                with self.assertRaises(RuntimeError):session.start()
                with self.assertRaises(RuntimeError):session.verify()

    def test_every_start_write_failure_before_or_after_apply_is_contained(self):
        for index in (1,2):
            for applied in (False,True):
                with self.subTest(index=index,applied=applied):
                    link=Link();link.fail_write=index;link.after_apply=applied;session=link.session()
                    with self.assertRaises(OSError):session.start()
                    self.assertFalse(session.active)
                    self.assertTrue(session.restore()['restored'])
                    self.assertEqual(link.values,dict(JtagClock='15M',JtagClockAutoAdjust=1))
                    with self.assertRaises(RuntimeError):session.start()

    def test_every_start_read_failure_preserves_or_restores_original_properties(self):
        for index in range(1,6):
            with self.subTest(index=index):
                link=Link();link.fail_read=index;session=link.session()
                with self.assertRaises(OSError):session.start()
                if index<3:self.assertEqual(link.writes,0)
                session.restore()
                self.assertEqual(link.values,dict(JtagClock='15M',JtagClockAutoAdjust=1))

    def test_foreign_claim_blocks_callbacks_and_restoration_never_retries(self):
        link=Link();session=link.session();link.owned=False
        with self.assertRaises(RuntimeError):session.start()
        self.assertEqual(link.events,[])
        link=Link();session=link.session();session.start();link.owned=False
        events=list(link.events)
        with self.assertRaises(RuntimeError):session.verify()
        with self.assertRaises(RuntimeError):session.restore()
        self.assertEqual(link.events,events)
        link.owned=True;session.restore();self.assertEqual(link.events,events)
        self.assertFalse(session.restored)

    def test_restore_setter_failure_still_attempts_other_property_but_stays_uncertain(self):
        for failed in (3,4):
            for applied in (False,True):
                with self.subTest(failed=failed,applied=applied):
                    link=Link();session=link.session();session.start()
                    link.fail_write=failed;link.after_apply=applied
                    with self.assertRaises(RuntimeError):session.restore()
                    self.assertEqual(link.writes,4)
                    self.assertFalse(session.restored)
                    calls=list(link.events);session.restore();self.assertEqual(link.events,calls)

    def test_restore_read_failure_is_uncertain_even_after_both_writes(self):
        for index in (6,7):
            with self.subTest(index=index):
                link=Link();session=link.session();session.start();link.fail_read=index
                with self.assertRaises(RuntimeError):session.restore()
                self.assertEqual(link.writes,4);self.assertFalse(session.restored)
                calls=list(link.events);session.restore();self.assertEqual(link.events,calls)

    def test_parameter_drift_or_unknown_original_never_admits(self):
        for name,value in [('JtagClock','6M'),('JtagClockAutoAdjust',1)]:
            link=Link();session=link.session();session.start();link.values[name]=value
            with self.assertRaises(RuntimeError):session.verify()
            self.assertFalse(session.active)
            link.values.update(JtagClock='3M',JtagClockAutoAdjust=0)
            with self.assertRaises(RuntimeError):session.verify()
            session.restore()
        for name,value in [('JtagClock','6M'),('JtagClockAutoAdjust',2),('JtagClockAutoAdjust',False),('JtagClockAutoAdjust','0')]:
            link=Link();link.values[name]=value;session=link.session()
            with self.assertRaises(RuntimeError):session.start()
            self.assertEqual(link.writes,0)


if __name__=='__main__':unittest.main()
