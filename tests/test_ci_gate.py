import copy
import json
import unittest

from scripts.affected import CORES, LANES
from scripts.ci_gate import JOB_LANES, require_success, validate_plan
from scripts.ci_simulations import simulation_matrix


def results_for(selected=(), cores=()):
    lanes = {lane: lane in selected for lane in LANES}
    results = {'plan': {'result': 'success', 'outputs': {
        'lanes': json.dumps(lanes), 'cores': json.dumps(list(cores)),
        'simulations': json.dumps(simulation_matrix(list(cores)))}}}
    results.update({job: {'result': 'success' if lanes[lane] else 'skipped'}
                    for job, lane in JOB_LANES.items()})
    results['simulation-tools'] = {'result': 'success' if cores else 'skipped'}
    results['fpga-simulation'] = {'result': 'success' if cores else 'skipped'}
    return results


class GateTests(unittest.TestCase):
    def test_documentation_producer_and_full_plans(self):
        for selected, cores in [((), ()), (('parent', 'fpga'), ()),
                                (('host', 'parent'), ()), (LANES, CORES)]:
            with self.subTest(selected=selected):
                require_success(results_for(selected, cores))

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
        self.assertEqual(len(rows), 16)
        expected = {'sim-fes-coleco-' + scenario + suffix
                    for scenario in ('unit', 'board-graphics', 'board-stream',
                                     'board-interactive', 'board-controllers',
                                     'board-vdp-io', 'board-sprites')
                    for suffix in ('', '-oss')}
        expected.update({'sim-fes-coleco-expansion', 'sim-fes-coleco-diagnostic'})
        self.assertEqual({row['target'] for row in rows}, expected)
        self.assertTrue(all(row['core'] == 'coleco' for row in rows))

    def test_selected_families_only_and_no_empty_placeholder(self):
        from scripts.ci_simulations import simulation_matrix
        self.assertEqual(simulation_matrix([]), {'include': []})
        rows = simulation_matrix(['sms', 'sg1000'])['include']
        self.assertEqual({row['core'] for row in rows}, {'sms', 'sg1000'})
        self.assertEqual({row['target'] for row in rows},
                         {'sim-fes-sms sim-fes-sms-oss',
                          'sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link'})
        self.assertEqual({row['core'] for row in simulation_matrix(list(CORES))['include']}, set(CORES))
        for cores in (['unknown'], ['coleco', 'coleco']):
            with self.assertRaises(ValueError):
                simulation_matrix(cores)


class ObservationTests(unittest.TestCase):
    def test_only_validated_intentional_skips_are_omitted(self):
        from scripts.ci_gate import observation_checks
        results = results_for(('parent', 'host'))
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
