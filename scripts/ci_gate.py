"""Validate the affected plan and require every selected CI job to pass."""
import json
import os
from datetime import datetime, timezone
from pathlib import Path

from scripts.affected import CORES, LANES
from scripts.ci_simulations import simulation_matrix

JOB_LANES = {
    'parent-tests': 'parent', 'consistency': 'parent', 'host': 'host',
    'runtime': 'runtime', 'contracts': 'contracts', 'fpga-producer': 'fpga',
}


def validate_plan(lanes, cores):
    if not isinstance(lanes, dict) or set(lanes) != set(LANES):
        raise ValueError('Plan must contain exactly the known lanes')
    if any(type(value) is not bool for value in lanes.values()):
        raise ValueError('Lane selections must be booleans')
    if not isinstance(cores, list) or any(not isinstance(core, str) for core in cores):
        raise ValueError('Core selections must be a list of names')
    if len(cores) != len(set(cores)) or not set(cores) <= set(CORES):
        raise ValueError('Core selections must be unique known cores')
    if cores and not lanes['fpga']:
        raise ValueError('Simulation selections require the FPGA lane')


def require_success(results):
    expected = {'plan', 'simulation-tools', 'fpga-simulation', *JOB_LANES}
    if not isinstance(results, dict) or set(results) != expected:
        raise ValueError('Missing or unexpected CI job results')
    if results['plan'].get('result') != 'success':
        raise ValueError('CI planning did not succeed')
    outputs = results['plan'].get('outputs', {})
    lanes = json.loads(outputs.get('lanes', 'null'))
    cores = json.loads(outputs.get('cores', 'null'))
    validate_plan(lanes, cores)
    if json.loads(outputs.get('simulations', 'null')) != simulation_matrix(cores):
        raise ValueError('Simulation matrix does not cover the selected cores exactly')
    selected = {job: lanes[lane] for job, lane in JOB_LANES.items()}
    selected['simulation-tools'] = bool(cores)
    selected['fpga-simulation'] = bool(cores)
    for job, needed in selected.items():
        result = results[job].get('result')
        allowed = {'success'} if needed else {'success', 'skipped'}
        if result not in allowed:
            raise ValueError(f'{job}: required={needed}, result={result!r}')


def observation_checks(results):
    """Retain gate failures, even when raw jobs succeeded with an invalid plan."""
    known = {'success', 'failure', 'cancelled', 'skipped', 'timed_out', 'unknown'}
    checks = [{'name': name, 'result': job.get('result')
               if job.get('result') in known else 'unknown'}
              for name, job in results.items()]
    try:
        require_success(results)
    except (ValueError, TypeError, KeyError):
        return checks + [{'name': 'integration-gate', 'result': 'failure'}]
    return [check for check in checks if check['result'] != 'skipped']


def main():
    results = json.loads(os.environ['RESULTS'])
    base = os.environ.get('INTEGRATION_BASE')
    observation = {
        'format': 1, 'kind': 'ci-observation',
        'source_revision': os.environ['TESTED_REVISION'],
        'integration_base': base if base and base != '0' * 40 else None,
        'observed_at': datetime.now(timezone.utc).isoformat(),
        'repository': os.environ['REPOSITORY'], 'run_url': os.environ['RUN_URL'],
        'checks': observation_checks(results),
    }
    Path('ci-observation.json').write_text(json.dumps(observation, sort_keys=True, indent=2) + '\n')
    require_success(results)
    print('All planned software checks passed; omitted lanes are reported in the validated plan.')


if __name__ == '__main__':
    main()
