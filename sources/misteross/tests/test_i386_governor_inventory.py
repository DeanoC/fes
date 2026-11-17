"""Inventory must retain every scoped key and never invent timing evidence."""
from __future__ import annotations

from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts import fes_i386_governor as governor, i386_governor_inventory as inv, test_fes_i386_lockstep as lockstep


class GovernorInventoryTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = inv.inventory()
        cls.rows = {int(row['key'], 16): row for row in cls.data['rows']}

    def test_scope_cross_product_is_complete_and_unique(self):
        keys = {key for stem in lockstep.SCOPE for key in governor.stem_keys(stem)}
        self.assertEqual(set(self.rows), keys)
        self.assertEqual(len(self.rows), self.data['counts']['classifier_keys'])
        self.assertEqual(self.data['counts']['governor_classes'], len(governor.load().classes))

    def test_form_classes_and_286_evidence_boundary(self):
        table = governor.load()
        for key, row in self.rows.items():
            self.assertEqual(row['governor_class'], governor.classify(table, *inv.unpack(key)).name)
            self.assertEqual(row['timing_status'], 'unmeasured')
            self.assertEqual(row['evidence'], [])
        post286 = governor.key(1, 0x84, 0, 1, 0, 0)  # near JZ, 386+
        group7 = governor.key(1, 0x01, 0, 0, 0, 0)  # legality depends on selector and form
        self.assertEqual(self.rows[post286]['reference_286'], 'not_286_opcode')
        self.assertEqual(self.rows[group7]['reference_286'], 'legality_unclassified')




if __name__ == '__main__':
    unittest.main()
