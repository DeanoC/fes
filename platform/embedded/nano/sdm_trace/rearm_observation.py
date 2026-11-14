"""Ordered per-load source evidence, alongside independent native lifecycle checks.

This observes firmware records. It grants no source reuse, DMA, native activation
or hardware acceptance; the paired controller still verifies each real return,
configuration, identity and reader opening through its existing codecs.
"""
import copy
import importlib.util
from pathlib import Path
import re

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location('rearm_source_profile', HERE.parents[1]/'firmware/nano_sdm_input_profile.py')
mode = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mode)
_spec = importlib.util.spec_from_file_location('rearm_source_continuation', HERE/'continuation_observation.py')
continuation = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(continuation)

RESULTS = ('FES_NANO_INPUT_RESULT', 'FES_NANO_RETIRE_RESULT', 'FES_NANO_RETIRE_TIMING',
           'FES_NANO_RETIRE_QUEUE', 'FES_NANO_INPUT_CONTINUE')
RELEASE = 'FES_NANO_SOURCE_RELEASE'
REARM = 'FES_NANO_SOURCE_REARM'
RELEASE_FIELDS = 'status owner source_generation generation reuse_authorized elapsed_us'.split()
REARM_FIELDS = 'status owner epoch previous generation baseline producer abort bytes sha384 start_us'.split()


def require(value, message):
    if not value:
        raise ValueError(message)


def record(line, prefix, fields):
    require(len(line) <= 2048 and line.startswith(prefix+' '), 'bounded source transition record required')
    pairs = [value.split('=', 1) for value in line.split()[1:]]
    require(all(len(pair) == 2 for pair in pairs) and [pair[0] for pair in pairs] == fields,
            'complete ordered source transition fields required')
    result = {}
    for key, value in pairs:
        if key == 'sha384':
            require(re.fullmatch('[0-9a-f]{96}', value), 'complete rearm source digest required')
            result[key] = value
            continue
        hexadecimal = key in ('owner', 'epoch', 'abort')
        pattern = '[0-9a-f]{1,16}' if hexadecimal else '-?[0-9]{1,11}' if key == 'status' else '[0-9]{1,20}'
        require(re.fullmatch(pattern, value), 'bounded source transition integer required')
        result[key] = int(value, 16 if hexadecimal else 10)
        require(-(1 << 31) <= result[key] < (1 << 31) if key == 'status' else 0 <= result[key] < (1 << 64),
                'source transition integer overflow')
    return result


class SourceSequence:
    def __init__(self, profile, build_record, retained_reader_route, boot):
        require(mode.checked(profile) == mode.REARM_CONTRACT, 'explicit repeated-load boot contract required')
        native = dict(build_record,
                      native_program=build_record.get('native_program', build_record.get('program')),
                      native_build_sources=build_record.get('native_build_sources', build_record.get('source_sha256',build_record.get('sources'))))
        require(mode.rearm.native_binding(native) == profile['sdm_cpu_rearm'],
                'paired CPU source-release implementation differs from firmware selection')
        for key in ('hps_context', 'hps_menu_program', 'hps_program_platform', 'cpu_mapping'):
            require(profile.get(key) == build_record.get(key), 'rearm profile lost its compiled '+key)
        require(retained_reader_route is not None and profile.get('sdm_reader_route') == retained_reader_route,
                'rearm requires the explicitly sealed reader route')
        require(type(boot) is dict and boot.get('loader_ready') is True and boot.get('errors') == [],
                'independently verified boot required for source sequence')
        cap = boot.get('capability_observation') or {}
        require(cap.get('status') == 0 and cap.get('reads') == 26 and all(
            type(cap.get(key)) is int and 0 < cap[key] < 1 << 64 for key in ('owner', 'epoch')),
            'complete observed boot owner and epoch required')
        require(re.fullmatch('[0-9a-f]{64}', profile.get('chain_sha256', '')),
                'source-bound repeated-load chain required')
        self.boot = copy.deepcopy(boot)
        self.build = profile['chain_sha256'][:16]
        self.last = None
        self.failed = False

    def observe(self, data, descriptor, source, previous, role):
        require(not self.failed, 'failed source sequence cannot resume')
        try:
            return self._observe(data, descriptor, source, previous, role)
        except Exception:
            self.failed = True
            raise

    def _observe(self, data, descriptor, source, previous, role):
        require(type(data) is bytes and 0 < len(data) <= 1 << 20 and data.endswith(b'\n') and b'\0' not in data,
                'complete bounded per-load firmware capture required')
        require(role in ('menu', 'computer') and type(source) is int and 0 < source < (1 << 64)-2,
                'explicit source generation and compiled role required')
        warm = self.last is not None
        require(previous == (self.last['generation'] if warm else 0) and
                source == (previous+(6 if self.last['role'] == 'menu' else 5) if warm else 4),
                'source skipped its owned native/SD return')
        parts = data.splitlines(keepends=True)
        lines = [re.sub(r'^NOTICE:[ \t]+', '', row.decode('ascii').rstrip('\r\n')) for row in parts]
        positions = {}
        for prefix in (*RESULTS, RELEASE, REARM):
            positions[prefix] = [n for n, line in enumerate(lines) if prefix in line]
        require(all(len(positions[prefix]) == 1 for prefix in RESULTS),
                'one complete source/retirement/continuation result per load required')
        first = positions[RESULTS[0]][0]
        require([positions[prefix][0] for prefix in RESULTS] == list(range(first, first+5)),
                'source results are missing, interleaved or reordered')
        require(all(len(positions[prefix]) == int(warm) for prefix in (RELEASE, REARM)),
                'release/rearm count differs from configuration lineage')
        for line in lines:
            if any(prefix in line for prefix in ('FES_NANO_SOURCE_', 'FES_NANO_INPUT_', 'FES_NANO_RETIRE_')):
                require(any(line.startswith(prefix+' ') for prefix in positions),
                        'unknown source transition, failure or terminal marker')
        end = positions[RESULTS[-1]][0]
        remove = set(positions[RELEASE]+positions[REARM])
        expected = None
        rearmed = released = None
        if warm:
            arms = [n for n, line in enumerate(lines) if line.startswith('FES_NANO_CORE_TRANSFER phase=arm ')]
            phase3 = [n for n, line in enumerate(lines) if line == 'FES_NANO_START phase=3 status=0']
            phase4 = [n for n, line in enumerate(lines) if line.startswith('FES_NANO_START phase=4 status=')]
            require(len(arms) == len(phase3) == len(phase4) == 1 and
                    positions[RELEASE][0] < arms[0] < phase3[0] < positions[REARM][0] < phase4[0] < first,
                    'source release/rearm escaped its before-copy/START boundary')
            released = record(lines[positions[RELEASE][0]], RELEASE, RELEASE_FIELDS)
            rearmed = record(lines[positions[REARM][0]], REARM, REARM_FIELDS)
            cap = self.boot['capability_observation']
            require(released == dict(status=0, owner=cap['owner'], source_generation=previous,
                                     generation=source, reuse_authorized=1, elapsed_us=released['elapsed_us']) and
                    0 <= released['elapsed_us'] < 1000000, 'source release has no completed owned predecessor')
            target = (self.last['cursor']+6) & 15
            require(rearmed == dict(status=0, owner=cap['owner'], epoch=cap['epoch'], previous=previous,
                    generation=source, baseline=self.last['cursor'], producer=target, abort=0x100000,
                    bytes=descriptor['core_size'], sha384=descriptor['core_sha384'], start_us=rearmed['start_us']) and
                    rearmed['start_us'] > self.last['last_us'], 'warm preparation reused source, cursor, identity or clock')
            expected = dict(generation=source, arm_writes=4, next_job=self.last['next_job'], retire_target=(target+6) & 15)
        prefix = b''.join(row for n, row in enumerate(parts[:end+1]) if n not in remove)
        proof = continuation.decode(prefix, boot=self.boot, build=self.build, configuration=descriptor, _expected=expected)
        require(proof['passed'] and proof['continuation_verified'], 'source transfer/retirement/continuation failed: '+str(proof['errors']))
        queue = proof['retirement']
        retirement = queue['retirement_observation']
        transferred = retirement['input_observation']['result']
        counters = [(match[1], int(match[2],16)//400) for line in lines[:first]
                    if (match := re.fullmatch(r'FES_NANO_CORE_TRANSFER phase=(arm|start|write|poll|status) .* counter=([0-9a-f]+) hash_ticks=[0-9a-f]+',line))]
        starts = [value for phase,value in counters if phase == 'start']
        writes = [value for phase,value in counters if phase == 'write']
        statuses = [value for phase,value in counters if phase == 'status']
        require(len(starts)==1 and writes and statuses and statuses[-1]<=queue['timing']['start_us'],
                'source retirement clock precedes native transfer status')
        if warm:
            require(starts[0]<=rearmed['start_us']<=writes[0], 'warm source clock escaped native START preparation')
            require(rearmed['start_us']+transferred['elapsed_us'] <= queue['timing']['start_us'] < 1 << 64,
                    'warm source clock does not precede its actual retirement')
        next_job = ((self.last['next_job'] if warm else 0)+transferred['posts']) % 14
        # The caller's existing configuration/identity/return parser consumes
        # all other bytes; only the fully verified source records are removed.
        remove.update(positions[prefix][0] for prefix in RESULTS)
        clean = b''.join(row for n, row in enumerate(parts) if n not in remove)
        self.last = dict(generation=source, role=role, cursor=retirement['result']['producer'],
                         next_job=next_job, last_us=queue['timing']['start_us']+proof['continuation']['elapsed_us'], proof=proof,
                         release=released, rearm=rearmed)
        return clean, copy.deepcopy(self.last)
