"""Evidence schema guards for the actual native-client/RTL test."""
import importlib.util
from pathlib import Path
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_native_client_proof", HERE / "client_proof.py")
PROOF = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PROOF)


class NativeClientProofTests(unittest.TestCase):
    def result(self, **fields):
        return dict(scenario="wrong-build", result="passed", writes=16,
                    image_bytes=0, loaded_bytes=0, complete_frames=0) | fields

    def test_result_requires_complete_exact_scenario_and_counts(self):
        PROOF.validate_result(self.result(), "wrong-build")
        bad = [self.result(result="failed"), self.result(scenario="late-reset"),
               self.result(writes=True), self.result(writes=17), self.result(image_bytes=1)]
        missing = self.result(); missing.pop("complete_frames"); bad.append(missing)
        extra = self.result(); extra["ignored"] = 1; bad.append(extra)
        for row in bad:
            with self.subTest(row=row), self.assertRaises(ValueError):
                PROOF.validate_result(row, "wrong-build")

    def test_session_requires_full_loader_and_visible_frames(self):
        row = self.result(scenario="session", image_bytes=910, loaded_bytes=910,
                          complete_frames=10, writes=1463,
                          roundtrip={'saved_bytes':910,'loads':2,'runs':2,'editor_returns':24,
                                     'saved_cdflag':0x40,'printed_value':7,'reload_removed_edit':True})
        PROOF.validate_result(row, "session")
        for field, value in (("loaded_bytes", 909), ("image_bytes", 0),
                             ("complete_frames", 1), ("writes", 1464)):
            with self.subTest(field=field), self.assertRaises(ValueError):
                PROOF.validate_result(row | {field: value}, "session")

    def test_roundtrip_requires_editor_and_program_execution_evidence(self):
        good={'saved_bytes':910,'loads':2,'runs':2,'editor_returns':24,
              'saved_cdflag':0x40,'printed_value':7,'reload_removed_edit':True}
        PROOF.check_roundtrip(good,910)
        for key,value in [('loads',1),('runs',1),('editor_returns',23),
                          ('saved_cdflag',0xc0),('printed_value',456),('reload_removed_edit',False)]:
            with self.subTest(key=key),self.assertRaises(ValueError):
                PROOF.check_roundtrip(good|{key:value},910)

    def test_source_capture_contains_real_client_and_machine(self):
        receipt, sources = PROOF.capture()
        expected = {"sources/libmister-runtime/src/native/de25_computer.cpp",
                    "sources/misteross/boards/de25nano/zx81-hps/fes_de25_computer.v",
                    "sources/misteross/boards/de25nano/zx81-hps/computer_sim.hpp",
                    "sources/misteross/boards/de25nano/zx81-hps/cosim/client.cpp",
                    "sources/misteross/boards/de25nano/zx81-hps/cosim/server.cpp",
                    "sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v",
                    "sources/misteross/cores/fes-zx81/rtl/zx81_machine.sv",
                    "sources/misteross/cores/fes-zx81/rtl/zx8x.hex"}
        self.assertTrue(expected <= sources.keys())
        for name in expected:
            self.assertEqual(receipt["sha256"][name], PROOF.sha(sources[name]))


if __name__ == "__main__":
    unittest.main()
