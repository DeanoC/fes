import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest

HERE = Path(__file__).resolve().parents[1]/'prototypes/de25_board_control'
spec = importlib.util.spec_from_file_location('de25_timing_sources', HERE/'timing_sources.py')
timing = importlib.util.module_from_spec(spec)
spec.loader.exec_module(timing)


class TimingSourceTests(unittest.TestCase):
    def test_exact_generated_exception_removal_is_complete_and_reversible(self):
        with tempfile.TemporaryDirectory() as directory:
            project = Path(directory)
            original = ('# Altera generated source; retain its header.\n\n'+timing.COMMAND+'\n').encode()
            paths = [project/name/'altera_avalon_st_jtag_interface.sdc' for name in ('debug0','debug1','emif')]
            for path in paths:
                path.parent.mkdir()
                path.write_bytes(original)
            record = timing.repair(project)
            self.assertEqual(timing.verify(project, record), record)
            for path in paths:
                self.assertEqual(path.read_bytes().replace(timing.REPLACEMENT.encode(),timing.COMMAND.encode()), original)
            for change in ('hash','path','missing','extra-command','restored'):
                row = copy.deepcopy(record)
                path = paths[0]
                after = path.read_bytes()
                if change=='hash':row['files'][0]['before_sha256']='1'*64
                elif change=='path':row['files'][0]['path']='../other.sdc'
                elif change=='missing':row['files'].pop()
                elif change=='extra-command':path.write_bytes(after+b'set_false_path -from * -to *\n')
                else:path.write_bytes(original)
                with self.subTest(change=change),self.assertRaises(ValueError):timing.verify(project,row)
                path.write_bytes(after)

    def test_other_active_constraints_and_empty_inventory_are_not_silently_changed(self):
        for text in (b'', b'create_clock -period 10 clk\n',
                     (timing.COMMAND+'\nset_false_path -from real_source -to real_sink\n').encode()):
            with self.subTest(text=text),self.assertRaises(ValueError):timing.replacement(text)
        with tempfile.TemporaryDirectory() as directory,self.assertRaises(ValueError):timing.repair(Path(directory))


if __name__=='__main__':unittest.main()
