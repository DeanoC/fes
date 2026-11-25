"""Host checks for the shared-shell SD export and its FAT namespace."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('test_nano_mixed_catalog_builder',
                                              ROOT/'platform/embedded/tamago/build.py')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


def encode(record):
    return json.dumps(record).encode()


class SharedCatalogueTests(unittest.TestCase):
    def test_reindexes_choices_and_preserves_media(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            entries = []
            expected = {}
            for game, stem, media in (('zx81', 'ZX81', 'ZX81.P'),
                                      ('spectrum48', 'SPEC48', 'SPEC48.PZX'),
                                      ('c64', 'C64', 'C64.D64')):
                stage = root / game
                stage.mkdir()
                files = {}
                for name in (stem+'.FCP', stem+'.JSN', stem+'.ROM', media):
                    path = stage / name
                    path.write_bytes(name.encode())
                    files[name] = path
                    expected[name] = name.encode()
                choice = dict(path='FES/'+stem+'.FCP', metadata_path='FES/'+stem+'.JSN',
                              sources=[dict(path='FES/'+stem+'.ROM')],
                              tape=dict(path='FES/'+media))
                entries.append((game, game.upper(), choice, files))
            # An unrelated file in a producer's staging directory is not exported.
            (stage/'extra.bin').write_bytes(b'extra')
            output = root/'export/FES'
            builder.stage_shared_catalog(encode, output, entries)
            self.assertEqual(json.loads((output/'CORESEL.JSN').read_bytes())['programs'],
                             [entry[2] for entry in entries])
            self.assertEqual(json.loads((output/'UI.JSON').read_bytes())['entries'],
                             [dict(id=entry[0], title=entry[1], program=i)
                              for i, entry in enumerate(entries)])
            self.assertEqual({p.name: p.read_bytes() for p in output.iterdir()
                              if p.name not in ('CORESEL.JSN', 'UI.JSON')}, expected)

    def test_rejects_shared_paths_and_control_file_aliases(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            choice = dict(path='FES/A.FCP', metadata_path='FES/A.JSN')
            entry = ('first', 'First', choice, {'A.FCP': root/'a', 'A.JSN': root/'meta'})
            cases = [[entry, ('second', 'Second', choice, entry[3])]]
            for path in ('../A.FCP', 'A.FCP', 'FES/CORESEL.JSN', 'FES/menusel.jsn'):
                cases.append([('first', 'First', dict(choice, path=path), entry[3])])
            for entries in cases:
                with self.subTest(entries=entries), self.assertRaisesRegex(ValueError, 'invalid or aliased'):
                    builder.stage_shared_catalog(encode, root/'FES', entries)
                self.assertFalse((root/'FES').exists())
