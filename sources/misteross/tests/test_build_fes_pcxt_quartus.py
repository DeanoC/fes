"""Quartus package gates must preserve timing failures and artifact identity."""
import json
import unittest
from scripts import build_fes_pcxt_quartus as producer
from scripts.export_core_package import build_identity, encode_build_record, _validate_build_record


class PCXTQuartusPackageTests(unittest.TestCase):
    def summary(self):
        return {'status': 'diagnostic-captured', 'seeds': [1], 'video_mailbox_contract': True,
                'profile': {'HALF_RATE_BOARD': 0, 'ENABLE_FES_COMPUTER': 1}, 'runs': [{
                    'seed': 1, 'capture_complete': True, 'reported_endpoint_checks_pass': True,
                    'minimum_pulse_width': {str(c): {'failures': 0} for c in range(4)},
                    'mailbox_interstage': [{'slack_ns': '0.123'} for _ in range(24)],
                    'timing_coverage_issues': {k: 0 for k in
                                             ('loops', 'latches', 'multiple_clock', 'generated_clock')}}]}

    def test_all_timing_classes_and_profile_must_pass(self):
        producer.require_timing(self.summary(), [1])
        mutations = [lambda s: s['runs'][0].update(reported_endpoint_checks_pass=False),
                     lambda s: s['runs'][0]['minimum_pulse_width']['0'].update(failures=1),
                     lambda s: s['runs'][0]['mailbox_interstage'][0].update(slack_ns='-0.000'),
                     lambda s: s['runs'][0]['timing_coverage_issues'].update(generated_clock=1),
                     lambda s: s['profile'].update(HALF_RATE_BOARD=1),
                     lambda s: s.update(video_mailbox_contract=False),
                     lambda s: s['runs'].clear()]
        for mutate in mutations:
            summary = self.summary()
            mutate(summary)
            with self.subTest(mutation=mutate), self.assertRaises(producer.BuildError):
                producer.require_timing(summary, [1])

    def test_quartus_has_a_distinct_functional_identity_without_gpu_inputs(self):
        execution = {'toolchain': 'quartus', 'files': {}}
        record = producer.create_build_record(producer.ROOT, 'https://github.com/DeanoC/fes.git',
            'a'*40, {'quartus': 'pinned quartus', 'yosys': 'pinned yosys'}, execution=execution)
        fields = json.loads(record)
        self.assertEqual(fields['format'], 2)
        self.assertEqual(fields['toolchain'], 'quartus')
        self.assertNotIn('gpu_device', fields['parameters'])
        before = build_identity(record)
        fields.pop('toolchain')
        self.assertNotEqual(build_identity(encode_build_record(fields)), before)
        fields['toolchain'] = 'oss'
        with self.assertRaisesRegex(ValueError, 'toolchain'):
            _validate_build_record(fields)

    def test_package_elaboration_embeds_the_exact_build_id(self):
        program = producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                             producer.ROOT / 'unused-library',
                                             package_profile=True, build_id='1234567890abcdef'*2)
        self.assertIn('-set HALF_RATE_BOARD 0', program)
        self.assertIn('-set ENABLE_FES_COMPUTER 1', program)
        self.assertIn('-set PHASED_HDMI_CLOCK 1', program)
        self.assertIn("-set BUILD_ID 128'h" + '1234567890abcdef'*2, program)
        with self.assertRaisesRegex(ValueError, 'BUILD_ID'):
            producer.board.elaboration(producer.ROOT, producer.ROOT / 'build',
                                       producer.ROOT / 'unused-library', package_profile=True)


if __name__ == '__main__':
    unittest.main()
