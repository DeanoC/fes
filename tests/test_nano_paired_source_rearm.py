"""Actual paired controller over synthetic source/SDM wire and injected boot observation.

Native package/ROM/fabric hardware is not emulated here. The independently
verified boot observation is injected; existing cold boot tests cover its codec.
"""
import copy
import re
import unittest
from unittest.mock import patch

from test_nano_paired_tenfoot_trace import Model, paired, PROGRAMS, SPEC, OWNER
from test_nano_sdm_rearm_trace import source_wire, BUILD
from test_nano_sdm_rearm_profile import warm_profile, mode


class RearmModel(Model):
    def __init__(self):
        self.diag=None
        super().__init__()
        for name in ('configuration_retained','selected_programs','tenfoot_serial','computer_reload',
                     'computer_storage_return'):
            self.profile[name]=True
        self.profile['window_storage_return']=False
        self.profile['source_sha256']=mode.rearm.expected_cpu_binding()['sources']
        self.profile['sdm_reader_route']=copy.deepcopy(self.route)
        self.cold=warm_profile()
        for key in ('hps_context','hps_menu_program','hps_program_platform','cpu_mapping'):
            self.cold[key]=copy.deepcopy(self.profile[key])
        self.cold['chain_sha256']=BUILD+'1'*48
        self.cold['sdm_reader_route']=copy.deepcopy(self.route)
        self.cold['sdm_cpu_rearm']=mode.rearm.expected_cpu_binding()
        mode.checked(self.cold)

    def source_lines(self,data,source,*,original=4):
        raw=super().source_lines(data,source,original=original)
        raw=raw.replace(b' gbpa=0 agbpa=0',b' gbpa=100000 agbpa=0')
        raw=raw.replace(b'bypass_controls_clear=1',b'bypass_controls_clear=0')
        if b'FES_NANO_WINDOW_GATE ' in raw:
            self.route=paired.reader_route._expected(self.profile)
            marker=(f'NOTICE:  FES_NANO_READER_ROUTE status=0 owner={OWNER} stream=1 secure=1 aruser=3e0 '
                    f'arprot=3 bank=secure ns_abort=1 source_retained=1 source_generation={source} '
                    f'generation={source+1} binding={paired.reader_route.digest(self.route)}\n').encode()
            raw=b''.join(marker+line if b'FES_NANO_WINDOW_GATE ' in line else line for line in raw.splitlines(keepends=True))
        return raw

    def configure(self,source):
        role='menu' if self.configurations%2==0 else 'computer'
        cfg=self.companion['configuration'] if role=='menu' else PROGRAMS[self.selected]['configuration']
        previous=copy.deepcopy(self.diag)
        raw=super().configure(source)
        new=source_wire(source,previous,cfg,role).decode().replace('owner=11 ',f'owner={OWNER} ')
        lines=new.splitlines(keepends=True)
        transition=b''.join(line.encode() for line in lines if line.startswith('FES_NANO_SOURCE_RELEASE '))
        start=new.index('FES_NANO_START phase=0 ')
        engine=new.index('FES_NANO_CORE_ENGINE ')
        start_wire=new[start:engine].encode()
        remap=b''.join(line.encode() for line in lines if line.startswith(
            ('FES_NANO_REMAP kind=2 ','FES_NANO_SDM_SUBMIT ','FES_NANO_SDM_RESULT ')))
        result=b''.join(line.encode() for line in lines if line.startswith(
            ('FES_NANO_INPUT_RESULT ','FES_NANO_RETIRE_RESULT ','FES_NANO_RETIRE_TIMING ',
             'FES_NANO_RETIRE_QUEUE ','FES_NANO_INPUT_CONTINUE ')))
        out=[];first_write=True;count=0
        for line in raw.splitlines(keepends=True):
            if b'FES_NANO_CORE_TRANSFER ' in line:
                phase=re.search(rb'phase=([a-z]+)',line)[1].decode();count+=1
                relative={'arm':-100000,'start':-50000,'status':900000,'complete':1015000}.get(phase,count*50000)
                line=re.sub(rb'counter=[0-9a-f]+',f'counter={(source*10000000+relative)*400:x}'.encode(),line)
                line=re.sub(rb'hash_ticks=[0-9a-f]+',f'hash_ticks={count*400:x}'.encode(),line)
            if b'FES_NANO_CORE_ENGINE ' in line:
                out.append(start_wire)
                line=re.sub(rb'next_job=[0-9]+',f'next_job={previous["next_job"] if previous else 0}'.encode(),line)
            if b'FES_NANO_FABRIC_ACTIVATION ' in line:out.append(result)
            out.append(line)
            if first_write and b'FES_NANO_CORE_TRANSFER phase=write ' in line:
                out.append(remap);first_write=False
        posts=(cfg['core_size']+cfg['chunk_bytes']-1)//cfg['chunk_bytes']
        self.diag=dict(generation=source,role=role,cursor=((previous['cursor'] if previous else 0)+12)&15,
                       next_job=((previous['next_job'] if previous else 0)+posts)%14)
        return transition+b''.join(out)

    def client(self):
        original=paired.decode_native_boot
        def boot(*args,**kwargs):
            kwargs['capability_observe']=False
            result=original(*args,**kwargs)
            if result['loader_ready'] and not result['errors']:
                result['capability_observation']=dict(status=0,reads=26,owner=int(OWNER,16),epoch=7)
            return result
        with patch.object(paired,'decode_native_boot',side_effect=boot):
            return paired.PairedTenfootClient(self.initial,build_record=self.profile,
                build_id=SPEC['build_id'],owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],
                programs=PROGRAMS,exchange=self.exchange,owns=lambda:self.service.owned,now=lambda:self.service.clock,
                capability_observe=True,remap_observe=True,retained_reader_route=self.route,source_rearm_profile=self.cold)


class PairedSourceRearmTests(unittest.TestCase):
    def test_full_three_core_launch_return_cycle_and_final_park(self):
        model=RearmModel();client=model.client()
        self.assertTrue(client.source_sequence.last['proof']['passed'])
        result=paired.exercise(client)
        self.assertTrue(result['passed'])
        self.assertEqual(client.loads,3)
        self.assertTrue(client.retired)
        self.assertEqual(client.source_sequence.last['generation'],37)
        self.assertFalse(client.source_sequence.failed)
        self.assertEqual(model.configurations,7)

    def test_missing_wrong_or_replayed_upload_proofs_retire_before_another_exchange(self):
        mutations=(lambda raw:raw.replace(b'FES_NANO_SOURCE_RELEASE status=0 ',b'FES_NANO_SOURCE_RELEASE status=1 '),
                   lambda raw:raw.replace(b'baseline=12 ',b'baseline=0 '),
                   lambda raw:raw.replace(b'arm_writes=4 ',b'arm_writes=12 '),
                   lambda raw:raw.replace(b'source_generation=4 generation=10',b'source_generation=3 generation=10'),
                   lambda raw:raw.replace(b'gbpa=100000 agbpa=0',b'gbpa=0 agbpa=0'),
                   lambda raw:raw.replace(b'FES_NANO_INPUT_CONTINUE ',b'FES_NANO_UNKNOWN_CONTINUE '))
        for change in mutations:
            model=RearmModel();client=model.client();exchange=model.exchange
            def bad_exchange(payload,bound,done):
                raw=exchange(payload,bound,lambda _:False)
                changed=change(raw);self.assertNotEqual(changed,raw)
                done(changed);return changed
            client.exchange=bad_exchange
            with self.subTest(change=change),self.assertRaises(ValueError):client.key('launch',0)
            self.assertTrue(client.retired)
            keys=list(model.keys)
            with self.assertRaises(ValueError):client.key('launch',0)
            self.assertEqual(model.keys,keys)
