import copy
import json
import unittest
from unittest.mock import patch

from scripts.affected import CORES, LANES
from scripts.ci_gate import JOB_LANES, require_success, validate_plan
from scripts.ci_simulations import simulation_matrix


def results_for(selected=(), cores=(), modes=None, tier='full'):
    lanes = {lane: lane in selected for lane in LANES}
    if modes is None:
        modes = {'video': False, 'media': False, 'full_race': False}
    results = {'plan': {'result': 'success', 'outputs': {
        'lanes': json.dumps(lanes), 'cores': json.dumps(list(cores)),
        'test_modes': json.dumps(modes),
        'sim_tier': tier,
        'simulations': json.dumps(simulation_matrix(list(cores), tier))}}}
    results.update({job: {'result': 'success' if lanes[lane] else 'skipped'}
                    for job, lane in JOB_LANES.items()})
    results['simulation-tools'] = {'result': 'success' if cores else 'skipped'}
    results['fpga-simulation'] = {'result': 'success' if cores else 'skipped'}
    return results


class TierTests(unittest.TestCase):
    def test_smoke_tier_swaps_only_slow_families(self):
        from scripts.ci_simulations import SMOKE_TARGETS
        for core in CORES:
            smoke = [r['target'] for r in simulation_matrix([core], 'smoke')['include']]
            full = [r['target'] for r in simulation_matrix([core], 'full')['include']]
            if core in SMOKE_TARGETS:
                self.assertEqual(smoke, list(SMOKE_TARGETS[core]))
                self.assertNotEqual(smoke, full)
            else:
                self.assertEqual(smoke, full)
        self.assertEqual(simulation_matrix(['c64']), simulation_matrix(['c64'], 'full'))
        with self.assertRaises(ValueError):
            simulation_matrix(['c64'], 'quick')

    def test_gate_ties_the_tier_to_the_event(self):
        results = results_for(('parent', 'fpga'), ('c64',))
        require_success(results, 'schedule')
        require_success(results, 'workflow_dispatch')
        for event in ('pull_request', 'merge_group', 'push'):
            with self.subTest(event=event), self.assertRaises(ValueError):
                require_success(results, event)
        outputs = results['plan']['outputs']
        outputs['sim_tier'] = 'smoke'
        outputs['simulations'] = json.dumps(simulation_matrix(['c64'], 'smoke'))
        require_success(results, 'pull_request')
        require_success(results, 'merge_group')
        require_success(results, 'push')
        for event in ('schedule', 'workflow_dispatch'):
            with self.subTest(event=event), self.assertRaises(ValueError):
                require_success(results, event)


class GateTests(unittest.TestCase):
    def test_menu_has_a_required_simulation_job(self):
        self.assertEqual(simulation_matrix(['menu']),
                         {'include': [{'core': 'menu', 'target': 'sim-fes-menu'}]})
        require_success(results_for(('parent', 'fpga'), ('menu',)))

    def test_z80_has_a_required_standalone_simulation_job(self):
        self.assertEqual(simulation_matrix(['z80']),
                         {'include': [{'core': 'z80', 'target': 'sim-fes-z80'}]})
        baseline = results_for(('parent', 'fpga'), ('z80',))
        require_success(baseline)
        for job in ('simulation-tools', 'fpga-simulation'):
            results = copy.deepcopy(baseline)
            results[job]['result'] = 'skipped'
            with self.subTest(job=job), self.assertRaises(ValueError):
                require_success(results)

    def test_documentation_producer_and_full_plans(self):
        all_modes = {'video': True, 'media': True, 'full_race': True}
        for selected, cores, modes in [((), (), None), (('parent', 'fpga'), (), None),
                                       (('host', 'parent'), (), None),
                                       (LANES, CORES, all_modes)]:
            with self.subTest(selected=selected):
                require_success(results_for(selected, cores, modes))

    def test_selected_modes_must_be_valid_and_lane_consistent(self):
        baseline = results_for(LANES, CORES,
                               {'video': True, 'media': True, 'full_race': True})
        for bad in (None, [], {}, {'video': True}, 'true',
                    {'video': 1, 'media': False, 'full_race': False},
                    {'video': True, 'media': False, 'full_race': False, 'extra': False}):
            results = copy.deepcopy(baseline)
            results['plan']['outputs']['test_modes'] = json.dumps(bad)
            with self.subTest(modes=bad), self.assertRaises(ValueError):
                require_success(results)
        results = copy.deepcopy(baseline)
        del results['plan']['outputs']['test_modes']
        with self.assertRaises(ValueError):
            require_success(results)
        for modes, selected in [
                ({'video': True, 'media': False, 'full_race': False}, ('host', 'fpga')),
                ({'video': False, 'media': True, 'full_race': False}, ('fpga',)),
                ({'video': False, 'media': False, 'full_race': True}, ('parent', 'fpga'))]:
            with self.subTest(modes=modes, selected=selected), self.assertRaises(ValueError):
                require_success(results_for(selected, (), modes))
        for modes in ({'video': True, 'media': True, 'full_race': False},
                      {'video': False, 'media': False, 'full_race': True}):
            require_success(results_for(('parent', 'host'), (), modes))

    def test_every_planned_job_must_succeed(self):
        baseline = results_for(LANES, CORES)
        for job in baseline:
            for result in ('skipped', 'cancelled', 'failure', None):
                results = copy.deepcopy(baseline)
                results[job]['result'] = result
                with self.subTest(job=job, result=result), self.assertRaises(ValueError):
                    require_success(results)

    def test_missing_jobs_and_failed_unselected_jobs_are_rejected(self):
        baseline = results_for()
        for job in baseline:
            results = copy.deepcopy(baseline)
            del results[job]
            with self.subTest(job=job), self.assertRaises(ValueError):
                require_success(results)
        for result in ('failure', 'cancelled', None):
            results = results_for()
            results['host']['result'] = result
            with self.assertRaises(ValueError):
                require_success(results)

    def test_missing_or_invalid_plan_cannot_authorize_skips(self):
        for outputs in ({}, {'lanes': '{}', 'cores': '[]'},
                        {'lanes': 'null', 'cores': '[]'}):
            results = results_for()
            results['plan']['outputs'] = outputs
            with self.assertRaises(ValueError):
                require_success(results)

    def test_matrix_cannot_silently_omit_planned_coverage(self):
        for matrix in (None, {'include': []}, simulation_matrix(['pong'])):
            results = results_for(('fpga', 'parent'), ('coleco',))
            results['plan']['outputs']['simulations'] = json.dumps(matrix)
            with self.assertRaises(ValueError):
                require_success(results)

    def test_core_and_lane_contract(self):
        lanes = {lane: True for lane in LANES}
        for cores in (None, {}, ['unknown'], ['coleco', 'coleco'], [1]):
            with self.subTest(cores=cores), self.assertRaises(ValueError):
                validate_plan(lanes, cores)
        for bad in ({**lanes, 'fpga': 'true'}, {**lanes, 'extra': True},
                    {**lanes, 'fpga': False}):
            with self.assertRaises(ValueError):
                validate_plan(bad, ['coleco'])


class SimulationMatrixTests(unittest.TestCase):
    def test_coleco_includes_every_default_and_oss_scenario(self):
        from scripts.ci_simulations import simulation_matrix
        rows = simulation_matrix(['coleco'])['include']
        self.assertEqual(len(rows), 24)
        expected = {'sim-fes-coleco-' + scenario + suffix
                    for scenario in ('unit', 'board-graphics', 'board-stream',
                                     'board-interactive', 'board-controllers',
                                     'board-vdp-io', 'board-sprites')
                    for suffix in ('', '-oss')}
        expected.update({'sim-fes-coleco-expansion', 'sim-fes-coleco-diagnostic',
                         'sim-fes-video-parts'})
        expected.update({'sim-fes-coleco-sgm-' + name for name in
                         ('socket', 'shell-ram', 'ay', 'module', 'audio',
                          'integrated', 'probe')})
        self.assertEqual({row['target'] for row in rows}, expected)
        self.assertTrue(all(row['core'] == 'coleco' for row in rows))

    def test_pcxt_bios_is_an_independent_required_scenario(self):
        rows = simulation_matrix(['pcxt'])['include']
        self.assertEqual(rows, [{'core': 'pcxt', 'target': 'sim-fes-pcxt'},
                                {'core': 'pcxt', 'target': 'sim-fes-pcxt-bios'},
                                {'core': 'pcxt', 'target': 'sim-fes-pcxt-fdc'},
                                {'core': 'pcxt', 'target': 'sim-fes-pcxt-dos'}])

    def test_coleco_smoke_is_five_jobs_and_oss_board_twins_are_full_only(self):
        from scripts.ci_simulations import simulation_matrix
        smoke = [row['target'] for row in simulation_matrix(['coleco'], 'smoke')['include']]
        self.assertEqual(smoke, ['sim-fes-coleco-unit', 'sim-fes-coleco-unit-oss',
                                 'sim-fes-coleco-board-graphics', 'sim-fes-coleco-diagnostic',
                                 'sim-fes-coleco-sgm-integrated'])
        full = {row['target'] for row in simulation_matrix(['coleco'], 'full')['include']}
        self.assertTrue(set(smoke) <= full)
        self.assertFalse(any(t.startswith('sim-fes-coleco-board-') and t.endswith('-oss') for t in smoke))

    def test_selected_families_only_and_no_empty_placeholder(self):
        from scripts.ci_simulations import simulation_matrix
        self.assertEqual(simulation_matrix([]), {'include': []})
        rows = simulation_matrix(['sms', 'sg1000'])['include']
        self.assertEqual({row['core'] for row in rows}, {'sms', 'sg1000'})
        self.assertEqual({row['target'] for row in rows},
                         {'sim-fes-sms sim-fes-sms-oss',
                          'sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link sim-fes-sg1000-gp-audio'})
        self.assertEqual({row['core'] for row in simulation_matrix(list(CORES))['include']}, set(CORES))
        for cores in (['unknown'], ['coleco', 'coleco']):
            with self.assertRaises(ValueError):
                simulation_matrix(cores)


class ObservationTests(unittest.TestCase):
    def test_only_validated_intentional_skips_are_omitted(self):
        from scripts.ci_gate import observation_checks
        with patch.dict('os.environ', {'GITHUB_EVENT_NAME': 'pull_request'}):
            results = results_for(('parent', 'host'), tier='smoke')
            checks = observation_checks(results)
            self.assertEqual({check['name'] for check in checks},
                             {'plan', 'parent-tests', 'consistency', 'host'})
            self.assertTrue(all(check['result'] == 'success' for check in checks))
            results['host']['result'] = 'skipped'
            checks = observation_checks(results)
            self.assertIn({'name': 'host', 'result': 'skipped'}, checks)
            self.assertEqual(len(checks), len(results) + 1)


    def test_invalid_plan_with_successful_jobs_records_gate_failure(self):
        from scripts.ci_gate import observation_checks
        for invalid in ('{}', 'null'):
            results = results_for(LANES, CORES)
            results['plan']['outputs']['simulations'] = invalid
            self.assertIn({'name': 'integration-gate', 'result': 'failure'},
                          observation_checks(results))
        results = results_for(LANES, CORES)
        del results['plan']['outputs']['simulations']
        self.assertIn({'name': 'integration-gate', 'result': 'failure'},
                      observation_checks(results))
        results['host']['result'] = None
        self.assertIn({'name': 'host', 'result': 'unknown'}, observation_checks(results))
