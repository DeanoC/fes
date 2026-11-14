"""Explicit repeated-load contract, bound to the compiled CPU release path."""
import copy
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
CPU_FILES = tuple('platform/embedded/tamago/' + name for name in (
    'nativefabric/source_reservation.go', 'nativefabric/configuration_arena.go',
    'nativefabric/stored_configuration.go', 'nativefabric/menu_storage.go',
    'nativefabric/computer.go', 'embeddedcontrol/generated/fes_embedded_bridges.go'))
CPU_CONTRACT = dict(format=1, scope='owned-native-source-release-before-copy-v1',
                    bridge_major=1, bridge_minor=11, release_operation=14,
                    arena='original-private-reservation-under-held-mutex',
                    failure='retain-no-copy-no-retry', stale_completion='reject')


def require(value, message):
    if not value:
        raise ValueError(message)


def expected_cpu_binding():
    return dict(contract=copy.deepcopy(CPU_CONTRACT), sources={
        name: hashlib.sha256((ROOT/name).read_bytes()).hexdigest() for name in CPU_FILES})


def native_binding(record):
    require(type(record) is dict and record.get('native_program') == 'fpgaload',
            'repeated loads require a matched native fpgaload entry')
    require(all(record.get(name) is True for name in (
        'configuration_retained', 'selected_programs', 'tenfoot_serial',
        'computer_reload', 'computer_storage_return')) and bool(record.get('hps_menu_program')),
        'repeated loads require retained selected programs and both owned storage returns')
    expected = expected_cpu_binding()
    sources = record.get('native_build_sources')
    require(type(sources) is dict and all(sources.get(name) == digest
            for name, digest in expected['sources'].items()),
            'compiled native source-release path is absent or changed')
    return expected


def checked_cpu_binding(profile):
    require(type(profile) is dict and same(profile.get('sdm_cpu_rearm'),expected_cpu_binding()),
            'complete source-bound CPU rearm contract required')
    require(all(profile.get(name) is True for name in (
        'configuration_retained', 'selected_programs', 'tenfoot_serial',
        'computer_reload', 'computer_storage_return')) and bool(profile.get('hps_menu_program')),
        'CPU rearm lost its retained program/storage selection')
    return copy.deepcopy(profile['sdm_cpu_rearm'])


def contract(base):
    result = copy.deepcopy(base)
    result.update(format=15, scope='nano-owned-source-release-rearm-v15',
                  mode='input-transfer-retire-native-return-release-rearm',
                  attempt_scope='one-per-fresh-configuration-generation-no-retry',
                  buffers='retained-until-owned-return-SD-stop-and-consumed-source-release',
                  source_release=copy.deepcopy(CPU_CONTRACT),
                  source_release_bound_us=1000000,
                  warm_preparation='actual-previous-private-controller-and-retirement',
                  warm_controls=[0, 8, 12, 13], warm_abort=0x100000,
                  warm_arm_writes=4, warm_queue_clear=False,
                  queue_cursor='owned-private-engine-plus-six-modulo-sixteen',
                  generation_advance=dict(menu=6, computer=5),
                  warm_clock='fresh-before-source-SHA384-never-reset-a-failed-attempt',
                  warm_remap='retain-observed-80000000',
                  warm_mailbox='empty-parser-FIFOs-and-retired-vendor-descriptors-at-every-guard',
                  cpu_source_binding='sdm_cpu_rearm', failure_retry=False)
    result['cold_retirement_queue_pointers'] = result.pop('retirement_queue_pointers')
    return result


def same(left, right):
    return json.dumps(left, sort_keys=True) == json.dumps(right, sort_keys=True)
