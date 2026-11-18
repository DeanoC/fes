import unittest

from scripts.de25_hilq import (de25_producer, kit_target, recovery_write_allowed,
                               selected_kits)


class De25HilqTests(unittest.TestCase):
    def test_deployed_de25_selects_only_d25(self):
        paths = ["sources/misteross/boards/de25nano/zx81/build.py"]
        target = kit_target(paths)
        self.assertEqual(target, "D25")
        self.assertEqual(selected_kits(target), ("D25",))

    def test_core_label_selects_d25(self):
        self.assertEqual(kit_target(["README.md"], ["core:de25-zx81"]), "D25")

    def test_non_deployed_de25_diagnostics_do_not_select_kit(self):
        paths = ["sources/misteross/boards/de25nano/test_build.py",
                 "docs/de25-kit-hil.md"]
        self.assertIsNone(kit_target(paths))

    def test_existing_de10_selection_is_unchanged(self):
        self.assertIsNone(kit_target(["sources/misteross/cores/zx81/rtl/cpu.v"]))
        self.assertEqual(selected_kits(None), ("B", "A"))

    def test_quartus_producer_mapping_and_ambiguity(self):
        self.assertEqual(de25_producer([
            "sources/misteross/boards/de25nano/producers/build_menu_package.py"]),
            "build_menu_package.py")
        self.assertEqual(de25_producer([
            "sources/misteross/boards/de25nano/producers/build_zx81_package.py"]),
            "build_zx81_package.py")
        self.assertIsNone(de25_producer([
            "sources/misteross/boards/other/producers/build_menu_package.py"]))
        self.assertIsNone(de25_producer(["scripts/build_zx81_package.py"]))
        self.assertIsNone(de25_producer([
            "sources/misteross/boards/de25nano/producers/build_menu_package.py",
            "sources/misteross/boards/de25nano/producers/build_zx81_package.py"]))

    def test_target_labels_select_the_matching_quartus_producer(self):
        self.assertEqual(de25_producer([], ["core:de25-menu"]), "build_menu_package.py")
        self.assertEqual(de25_producer([], ["core:de25-zx81"]), "build_zx81_package.py")
        self.assertIsNone(de25_producer([], ["core:de25-menu", "core:de25-zx81"]))

    def test_recovery_requires_matching_board_and_card_identity(self):
        good = recovery_write_allowed(
            "TRWU8J8G", "TRWU8J8G", "de25-j8g", "de25-j8g",
            "/dev/disk/by-id/usb-TRWU8J8G-SD", "TRWU8J8G", "USB TRWU8J8G card")
        self.assertTrue(good)
        self.assertFalse(recovery_write_allowed(
            "TRWU8J8G", "TRWU8CBL", "de25-j8g", "de25-j8g",
            "/dev/disk/by-id/usb-TRWU8J8G-SD", "TRWU8J8G", "USB TRWU8J8G card"))
        self.assertFalse(recovery_write_allowed(
            "TRWU8J8G", "TRWU8J8G", "de25-j8g", "de25-j8g",
            "/dev/sdb", "TRWU8J8G", "USB TRWU8J8G card"))
        self.assertFalse(recovery_write_allowed(
            "TRWU8J8G", "TRWU8J8G", "de25-j8g", "de25-j8g",
            "/dev/disk/by-id/usb-unrelated-SD", "TRWU8J8G", "USB TRWU8J8G card"))
        self.assertFalse(recovery_write_allowed(
            "TRWU8J8G", "TRWU8J8G", "de25-j8g", "de25-j8g",
            "/dev/disk/by-id/usb-TRWU8J8G-SD", "TRWU8J8G", "USB unrelated card"))


if __name__ == "__main__":
    unittest.main()
