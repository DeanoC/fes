"""Synthetic repeated-source wire; no hardware or native boot acceptance."""
import copy
import re
import unittest

from test_nano_sdm_rearm_profile import warm_profile, mode
from test_nano_sdm_continuation import WIRE, BOOT, BUILD, CONFIG
from sdm_trace.rearm_observation import SourceSequence


def model():
    p = warm_profile()
    p['chain_sha256'] = BUILD+'1'*48
    native = dict(p, program='fpgaload', sources=copy.deepcopy(p['sdm_cpu_rearm']['sources']))
    return SourceSequence(p, native, p['sdm_reader_route'], BOOT)


def replace_fields(line, edits):
    parts = line.split()
    for n, word in enumerate(parts[1:], 1):
        key, value = word.split('=', 1)
        if key in edits:
            parts[n] = key+'='+str(edits[key])
    return ' '.join(parts)+'\n'


def source_wire(source=4, previous=None, descriptor=None, role='menu'):
    descriptor = CONFIG if descriptor is None else descriptor
    total, chunk = descriptor['core_size'], descriptor['chunk_bytes']
    done = 0
    first_job = previous['next_job'] if previous else 0
    baseline = previous['cursor'] if previous else 0
    prepared, retired = (baseline+6)&15, (baseline+12)&15
    start = source*10000000
    # Preserve the real diagnostic field grammars; only hardware observations
    # and source metadata are supplied by this synthetic fixture.
    records = []
    for line in WIRE.decode().splitlines():
        if line.startswith(('FES_NANO_INPUT_RESULT ', 'FES_NANO_RETIRE_RESULT ', 'FES_NANO_RETIRE_TIMING ',
                            'FES_NANO_RETIRE_QUEUE ', 'FES_NANO_INPUT_CONTINUE ')):
            records.append(line)
    prefix = ''
    if previous:
        prefix += (f'FES_NANO_SOURCE_RELEASE status=0 owner=11 source_generation={previous["generation"]} '
                   f'generation={source} reuse_authorized=1 elapsed_us=100\n')
    def transfer(phase, completed, pending):
        relative={'arm':-100000,'start':-50000,'write':10000+completed//4,'poll':20000+completed//4,'status':900000}[phase]
        return (f'FES_NANO_CORE_TRANSFER phase={phase} owner=11 generation={source} completed={completed} '
                f'pending={pending} total={total} base=94800000 counter={(start+relative)*400:x} hash_ticks=1\n')
    prefix += transfer('arm', 0, 0)+transfer('start', 0, 0)
    phases = (0,1,2,3,4,12,13,14,5,6,7,8,9,10,11)
    for phase in phases:
        prefix += f'FES_NANO_START phase={phase} status={2 if phase==7 else 0}\n'
        if phase==3 and previous:
            prefix += (f'FES_NANO_SOURCE_REARM status=0 owner=11 epoch=7 previous={previous["generation"]} '
                       f'generation={source} baseline={baseline} producer={prepared} abort=100000 '
                       f'bytes={total} sha384={descriptor["core_sha384"]} start_us={start}\n')
        if phase==12 and not previous:
            prefix += next(line+'\n' for line in WIRE.decode().splitlines() if line.startswith('FES_NANO_REMAP kind=1 phase=0 '))
            prefix += next(line+'\n' for line in WIRE.decode().splitlines() if line.startswith('FES_NANO_REMAP kind=1 phase=1 '))
    prefix += f'FES_NANO_CORE_ENGINE credits=4 block_bytes=1048560 next_job={first_job} owner=11 generation={source}\n'
    while done<total:
        amount=min(chunk,total-done)
        prefix+=transfer('write',done,amount)
        if not previous and not done:
            prefix+='\n'.join(line for line in WIRE.decode().splitlines() if line.startswith(
                ('FES_NANO_REMAP kind=2 ', 'FES_NANO_SDM_SUBMIT ', 'FES_NANO_SDM_RESULT ')))+'\n'
        prefix+=transfer('poll',done,amount)
        done+=amount
    prefix+=transfer('status',total,0)
    posts=(total+chunk-1)//chunk
    for line in records:
        changes=dict(generation=source,bytes=total,sha384=descriptor['core_sha384'])
        if line.startswith('FES_NANO_INPUT_RESULT '):
            changes.update(accepted=total,posted=total,acknowledged=total,completed=total,posts=posts,acks=posts,
                           buffers=posts,arm_writes=4 if previous else 12)
        if line.startswith('FES_NANO_RETIRE_RESULT '):changes.update(producer=retired,consumer=retired)
        if line.startswith('FES_NANO_RETIRE_QUEUE '):
            changes.update(consumer=format(retired,'x'),last_value=format(retired,'x'))
        if line.startswith(('FES_NANO_RETIRE_TIMING ','FES_NANO_RETIRE_QUEUE ')):
            # The transfer has elapsed one second before retirement begins.
            offset=start+1000000+100-1000
            for word in line.split()[1:]:
                key,value=word.split('=',1)
                if key in ('start_us','last_us','observed_us','completion_us'):
                    changes[key]=int(value)+offset
                elif key in ('first_us','latest_us'):
                    changes[key]=','.join(format(int(v,16)+offset,'x') for v in value.split(','))
        prefix+=replace_fields(line,changes)
    return prefix.encode()


class SourceSequenceTests(unittest.TestCase):
    def test_twelve_changing_sources_wrap_queues_and_jobs(self):
        sequence=model()
        source,previous=4,0
        for n in range(13):
            role='computer' if n%3==1 else 'menu'
            descriptor=dict(CONFIG,core_size=2101248 if n%2 else CONFIG['core_size'],
                            core_sha384=format(n+1,'x')*96)
            raw=source_wire(source,sequence.last,descriptor,role)
            clean,evidence=sequence.observe(raw,descriptor,source,previous,role)
            self.assertNotIn(b'FES_NANO_INPUT_RESULT',clean)
            self.assertNotIn(b'FES_NANO_SOURCE_RELEASE',clean)
            self.assertTrue(evidence['proof']['passed'])
            self.assertFalse(evidence['proof']['memory_reuse_authorized'])
            previous,source=source,source+(6 if role=='menu' else 5)
        self.assertFalse(sequence.failed)

    def test_old_release_or_changed_rearm_and_callback_state_fail_closed(self):
        seed=model()
        seed.observe(source_wire(),CONFIG,4,0,'menu')
        raw=source_wire(10,seed.last)
        release=next(line+b'\n' for line in raw.splitlines() if line.startswith(b'FES_NANO_SOURCE_RELEASE '))
        arm=next(line+b'\n' for line in raw.splitlines() if line.startswith(b'FES_NANO_SOURCE_REARM '))
        changes=[raw.replace(release,b''),raw.replace(release,release+release),raw.replace(arm,b''),
                 raw.replace(arm,arm+arm),raw+release,raw.replace(arm,b'')+arm]
        for before,after in ((b'source_generation=4',b'source_generation=3'),(b'reuse_authorized=1',b'reuse_authorized=0'),
            (b'baseline=12',b'baseline=0'),(b'producer=2',b'producer=6'),(b'abort=100000',b'abort=0'),
            (b'epoch=7',b'epoch=8'),(b'arm_writes=4',b'arm_writes=12'),(b'next_job=2',b'next_job=0'),
            (b'consumer=8',b'consumer=c'),(b'elapsed_us=100\n',b'elapsed_us=1000000\n')):
            changed=raw.replace(before,after)
            self.assertNotEqual(changed,raw,(before,after))
            changes.append(changed)
        for index,bad in enumerate(changes):
            sequence=model();sequence.observe(source_wire(),CONFIG,4,0,'menu')
            with self.subTest(index=index),self.assertRaises(ValueError):sequence.observe(bad,CONFIG,10,4,'computer')
            self.assertTrue(sequence.failed)
            self.assertEqual(sequence.last['generation'],4)
            with self.assertRaisesRegex(ValueError,'cannot resume'):sequence.observe(raw,CONFIG,10,4,'computer')

    def test_wrong_role_generation_or_unselected_profile_is_rejected(self):
        for source,previous in ((9,4),(10,3),(15,4)):
            sequence=model();sequence.observe(source_wire(),CONFIG,4,0,'menu')
            with self.subTest(source=source),self.assertRaises(ValueError):
                sequence.observe(source_wire(10,sequence.last),CONFIG,source,previous,'computer')
        p=warm_profile();native=dict(p,program='fpgaload',sources=p['sdm_cpu_rearm']['sources'])
        for key in ('configuration_retained','selected_programs','hps_menu_program'):
            with self.subTest(missing=key),self.assertRaises(ValueError):
                SourceSequence(p,dict(native,**{key:False}),p['sdm_reader_route'],BOOT)
