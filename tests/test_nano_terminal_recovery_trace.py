"""A reset request, returning transport or partial record never grants recovery."""
from pathlib import Path
import sys
import unittest

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'platform/embedded/nano'))
from terminal_recovery_trace import observe,PARK

BUILD='0123456789abcdef'
ATTEMPT=('FES_NANO_RECOVERY_ATTEMPT build='+BUILD+' retained=1 reuse=0 activated=0\n').encode()
RETURN=('FES_NANO_RECOVERY_RETURN build='+BUILD+' error=-5 retained=1 resume=0 reuse=0 activated=0\n').encode()
WIRE=PARK+b'\n'+ATTEMPT


class TerminalRecoveryTraceTests(unittest.TestCase):
    def check_no_authority(self,result):
        for name in ('request_publication_verified','reset_completed','fresh_boot_admitted','recovery_safe',
                     'source_reuse_authorized','ordinary_resume','hardware_acceptance'):
            self.assertIs(result[name],False,name)

    def test_request_return_and_new_banner_remain_failed_telemetry(self):
        for raw in (WIRE,WIRE+RETURN,WIRE+b'U-Boot SPL 2026.01 another boot\n',
                    b'old boot output\n'+WIRE,WIRE.replace(b'FES_',b'NOTICE:  FES_')):
            with self.subTest(raw=raw):
                result=observe(raw,build_id=BUILD)
                self.assertTrue(result['attempt_observed'])
                self.check_no_authority(result)
        self.assertEqual(observe(WIRE+RETURN,build_id=BUILD)['return_error'],-5)

    def test_partial_record_never_becomes_a_completed_request(self):
        for end in range(len(b'FES_NANO_RECOVERY_'),len(ATTEMPT)):
            result=observe(PARK+b'\n'+ATTEMPT[:end],build_id=BUILD)
            self.assertFalse(result['attempt_observed'])
            self.assertTrue(result['incomplete_marker'])
            self.check_no_authority(result)

    def test_malformed_foreign_repeated_and_reordered_records_reject(self):
        bad=(ATTEMPT,WIRE+ATTEMPT,RETURN+WIRE,WIRE+RETURN+RETURN,
             WIRE.replace(BUILD.encode(),b'ffffffffffffffff'),
             WIRE.replace(b'reuse=0',b'reuse=1'),
             PARK+b'\nnoise\n'+ATTEMPT,WIRE+RETURN.replace(b'error=-5',b'error=0'),
             WIRE+RETURN.replace(b'error=-5',b'error=-2147483649'),
             PARK+b'\n'+WIRE,b'prefix '+WIRE,b'\0'+WIRE,
             WIRE.replace(b'RECOVERY_ATTEMPT',b'RECOVERY_OTHER'))
        for raw in bad:
            with self.subTest(raw=raw),self.assertRaises(ValueError):observe(raw,build_id=BUILD)

    def test_missing_request_preserves_failure_without_granting_anything(self):
        result=observe(PARK+b'\n',build_id=BUILD)
        self.assertFalse(result['attempt_observed'])
        self.check_no_authority(result)


if __name__=='__main__':unittest.main()
