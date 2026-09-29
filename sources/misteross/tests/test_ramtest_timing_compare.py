"""Timing comparisons preserve delay definitions and logical bit names."""
import unittest

from scripts.ramtest_timing_compare import nextpnr_paths, quartus_paths


class TimingComparisonTest(unittest.TestCase):
    def test_nextpnr_preserves_negative_terms_and_resolves_register_bit(self):
        report = {"critical_paths": [{"from": "posedge mem", "to": "posedge mem",
            "max_delay": 7.692, "path": [
                {"type": "clk-skew", "delay": -0.1},
                {"type": "clk-to-q", "delay": 0.7,
                 "from": {"cell": "skid_ff", "port": "Q"}},
                {"type": "routing", "delay": 8.2},
                {"type": "logic", "delay": 1.2},
                {"type": "setup", "delay": -0.2,
                 "to": {"cell": "address_ff_22", "port": "ENA"}}]},
            {"from": "posedge mem", "to": "posedge pixel", "path": []}]}
        module = {"cells": {
            "skid_ff": {"connections": {"Q": [42]}},
            "address_ff_22": {"connections": {"Q": [123]}}},
            "netnames": {"ddr2_address": {"bits": list(range(100, 129))},
                         "skid": {"bits": [42]}}}
        result, = nextpnr_paths(report, module)
        self.assertAlmostEqual(result["effective_setup_ns"], 9.8)
        self.assertAlmostEqual(result["slack_ns"], -2.108)
        self.assertEqual(result["to_signals"], ["ddr2_address[23]"])
        self.assertEqual(result["logic_arcs"], 1)
        self.assertEqual(result["delays_ns"]["setup"], -0.2)

    def test_quartus_uses_relationship_minus_slack_not_data_delay(self):
        report = '''Path #1: Setup slack is 3.458
; From Node ; hps_ddr|port2|skid ;
; To Node ; ddr2_test|address[23] ;
; Setup Relationship ; 7.692 ; ; ; ; ; ;
; Data Delay ; 3.994 ; ; ; ; ; ;
; Number of Logic Levels ; ; 2 ; ; ; ; ;
'''
        result, = quartus_paths(report)
        self.assertEqual(result["logic_levels"], 2)
        self.assertAlmostEqual(result["effective_setup_ns"], 4.234)
        self.assertEqual(result["data_delay_ns"], 3.994)

    def test_missing_quartus_paths_fails(self):
        with self.assertRaises(ValueError):
            quartus_paths("Report Timing: Found 0 setup paths")


if __name__ == "__main__":
    unittest.main()
