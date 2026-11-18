"""build_pc386_firmware._run names the failing line of a parallel build (#1982)."""

from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from scripts import build_pc386_firmware as firmware  # noqa: E402


class FailureReportTests(unittest.TestCase):
    def run_script(self, script, earlier=''):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / 'build.log'
            log.write_text(earlier)
            with self.assertRaises(firmware.FirmwareError) as raised:
                firmware._run([sys.executable, '-c', script], directory, log)
            return str(raised.exception)

    def test_an_error_before_the_tail_is_reported_with_the_tail(self):
        message = self.run_script(
            'import sys\n'
            'print("cc -c first.c")\n'
            'print("bad.c:7: error: unknown type name", file=sys.stderr)\n'
            'sys.stderr.flush()\n'
            'for index in range(200): print(f"cc -c other{index}.c")\n'
            'sys.exit(2)\n')
        self.assertIn('bad.c:7: error: unknown type name', message)
        self.assertIn('cc -c first.c', message)
        self.assertIn('cc -c other199.c', message)
        self.assertNotIn('cc -c other100.c', message)
        self.assertIn('lines omitted', message)

    def test_make_error_lines_are_reported(self):
        message = self.run_script(
            'print("make[2]: *** [Makefile:12: scan.o] Error 1")\n'
            'for index in range(200): print(f"cp {index}")\n'
            'raise SystemExit(2)\n')
        self.assertIn('make[2]: *** [Makefile:12: scan.o] Error 1', message)

    def test_a_short_output_is_reported_whole(self):
        message = self.run_script('print("only line"); raise SystemExit(1)')
        self.assertIn('only line', message)
        self.assertNotIn('lines omitted', message)

    def test_an_earlier_command_in_the_same_log_is_not_reported(self):
        message = self.run_script(
            'for index in range(200): print(f"cp {index}")\n'
            'raise SystemExit(2)\n',
            earlier='$ make earlier\nold.c:1: error: from a command that passed\n')
        self.assertNotIn('from a command that passed', message)


if __name__ == '__main__':
    unittest.main()
