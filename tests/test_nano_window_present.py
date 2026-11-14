"""Source-bound repeated-frame capture fixtures; no hardware acceptance."""
import unittest

import test_nano_window_trace as fixture

trace,BUILD,SEED,OWNER=fixture.trace,fixture.BUILD,fixture.SEED,fixture.OWNER


class WindowPresentationTraceTests(unittest.TestCase):
    def setUp(self):
        f=fixture.WindowTraceTests();f.setUp();self.base=f
        close=f'NOTICE:  FES_NANO_WINDOW_SCOPE phase=close owner={OWNER:x} generation=6 reset=5\n'
        self.prefix=f.prefix+f.suffix.split(close)[0]
        frames=[];blocks=[];self.steps=[]
        def step(index,phase):
            row=f'FES_NATIVE_FPGA_WINDOW_PRESENT STEP index={index} phase={phase} elapsed_us={len(self.steps)}\n'
            self.steps.append(row);return row
        for index in range(1,13):
            frames.append(f'FES_NATIVE_FPGA_WINDOW_PRESENT FRAME index={index} package_id={f.identity["package_id"]} '
                f'build_id={f.identity["build_id"]} owner={OWNER:x} source_generation=4 reader_generation={index+1} '
                f'cookie=1 bytes=3686400 published_crc32={0x12340000+index:08x} read_crc32={0x12340000+index:08x} '
                f'frame_sha256={index:064x} drain_generation={index+1} drain_cookie={index+1} memory_retained=1\n')
            blocks.append(step(index,'render_begin')+step(index,'render_end')+step(index,'present_begin')+frames[-1]+step(index,'present_end'))
        self.frames=frames
        self.blocks=blocks;self.frame_body=''.join(blocks)
        self.tail=step(13,'return_begin')+close+(f'NOTICE:  FES_NANO_MASTER_STOP status=0 owner={OWNER:x} generation=6 reset=5 '
            'reader_reads=88 reader_polls=1 disable=0 probe_polls=2 cancel=0 complete=1 data=active\n')
        self.tail+=(f'NOTICE:  FES_NANO_WINDOW_READER_DRAIN owner={OWNER:x} generation=6 reset=5 reader_generation=13 '
            'request_generation=13 cookie=13 ack_generation=13 ack_cookie=13 status=4 pending=0\n')
        self.tail+=(f'NOTICE:  FES_NANO_WINDOW_CLOSURE owner={OWNER:x} generation=7 reset=f hardware_drained=8 '
            'data_remained_contained=0 producers_quiescent=1 fabric_acknowledged=0\n')
        self.tail+=step(13,'return_end')
        self.tail+=(f'FES_NATIVE_FPGA_WINDOW_PRESENT PASS package_id={f.identity["package_id"]} build_id={f.identity["build_id"]} '
            f'owner={OWNER:x} source_generation=4 closed_generation=7 frames=12 last_reader_generation=13 last_cookie=1 '
            f'bytes=3686400 crc32=1234000c frame_sha256={12:064x} access_map_sha256={trace.context_digest(f.contract)} '
            'data_closed=1 memory_retained=1 runtime_admission=unavailable\n')
        self.raw=self.prefix+self.frame_body+self.tail

    def decode(self,data,**options):
        f=self.base
        args=dict(fpga_identity=f.identity,configuration=f.configuration,from_storage=True,
            reader_memory=f.memory,window_read=f.contract,cpu_mapping=f.mapping,window_present=True)
        args.update(options)
        return trace.decode_native_configuration(data,BUILD,SEED,**args)

    def test_complete_and_explicit_mode(self):
        result=self.decode(self.raw)
        self.assertTrue(result['complete'],result['errors'])
        self.assertEqual(result['last_checkpoint'],'window_present_closed')
        self.assertEqual(len(result['window_read']['presentations']),12)
        self.assertEqual(len(result['window_read']['progress']),50)
        self.assertEqual(result['window_read']['reader_drain']['ack_cookie'],13)
        self.assertFalse(result['window_read']['memory_released'])
        self.assertFalse(result['core_load_acceptance'])
        self.assertFalse(self.decode(self.raw,window_present=False)['complete'])
        self.assertFalse(self.decode(self.base.raw)['complete'])
        for value in (1,None,'yes'):
            with self.subTest(value=value),self.assertRaises(ValueError):self.decode(self.raw,window_present=value)
        with self.assertRaises(ValueError):self.decode(self.raw,window_read=None)

    def test_every_frame_required_and_not_replayed(self):
        for index,line in enumerate(self.frames):
            for changed in (self.raw.replace(line,'',1),self.raw.replace(line,line+line,1),
                            self.raw.replace(line,line.replace(f'index={index+1} ',f'index={index+2} '),1)):
                with self.subTest(index=index):self.assertFalse(self.decode(changed)['complete'])
        for end in range(len(self.prefix),len(self.raw),257):
            self.assertFalse(self.decode(self.raw[:end])['complete'])
        self.assertFalse(self.decode(self.raw[:-1])['complete'])

    def test_frame_publication_identity_and_local_drain(self):
        line=self.frames[0]
        for before,after in (('reader_generation=2','reader_generation=1'),('drain_generation=2','drain_generation=1'),
                ('drain_cookie=2','drain_cookie=1'),('cookie=1 ','cookie=2 '),('bytes=3686400','bytes=32'),
                ('published_crc32=12340001','published_crc32=12340002'),('read_crc32=12340001','read_crc32=12340002'),
                (f'owner={OWNER:x}',f'owner={OWNER+1:x}'),('source_generation=4','source_generation=5'),
                (self.base.identity['build_id'],'0'*32),(self.base.identity['package_id'],'0'*64),
                ('frame_sha256='+f'{1:064x}','frame_sha256='+'0'*64)):
            with self.subTest(before=before):
                self.assertFalse(self.decode(self.raw.replace(line,line.replace(before,after),1))['complete'])
        repeated=self.raw.replace(self.frames[1],self.frames[1].replace('12340002','12340001').replace(f'{2:064x}',f'{1:064x}'))
        self.assertFalse(self.decode(repeated)['complete'])

    def test_final_firmware_proof_and_return(self):
        for before,after in (('reader_reads=88','reader_reads=132'),('reader_polls=1','reader_polls=2'),
                ('disable=0','disable=1'),('probe_polls=2','probe_polls=1'),('cancel=0','cancel=1'),
                ('ack_generation=13','ack_generation=12'),('request_generation=13','request_generation=12'),
                ('ack_cookie=13','ack_cookie=12'),('status=4 pending=0','status=4 pending=1'),
                ('status=4 pending=0','status=1 pending=0'),('hardware_drained=8','hardware_drained=0'),
                ('producers_quiescent=1','producers_quiescent=0'),('frames=12','frames=11'),
                ('last_reader_generation=13','last_reader_generation=12'),('last_cookie=1','last_cookie=2'),
                ('closed_generation=7','closed_generation=8'),('crc32=1234000c','crc32=1234000d'),
                ('access_map_sha256='+trace.context_digest(self.base.contract),'access_map_sha256='+'0'*64)):
            with self.subTest(before=before):self.assertFalse(self.decode(self.prefix+self.frame_body+self.tail.replace(before,after,1))['complete'])
        for marker in ('FES_NANO_MASTER_STOP ','FES_NANO_WINDOW_READER_DRAIN ','FES_NANO_WINDOW_CLOSURE ','FES_NATIVE_FPGA_WINDOW_PRESENT PASS '):
            changed=''.join(line for line in self.raw.splitlines(keepends=True) if marker not in line)
            with self.subTest(marker=marker):self.assertFalse(self.decode(changed)['complete'])

    def test_progress_retains_where_render_or_presentation_stopped(self):
        for phase,end in [('render_begin',1),('present_begin',3)]:
            partial=self.prefix+self.blocks[0]+''.join(self.steps[4:4+end])
            observed=self.decode(partial)
            self.assertFalse(observed['complete']);self.assertEqual(observed['errors'],[])
            self.assertEqual(observed['last_checkpoint'],'window_present_'+phase+'_2')
            self.assertEqual(len(observed['window_read']['presentations']),1)
        for line in self.steps:
            with self.subTest(line=line):self.assertFalse(self.decode(self.raw.replace(line,'',1))['complete'])
        for before,after in [('index=2 ','index=3 '),('phase=render_begin','phase=render_end'),
                             ('elapsed_us=4','elapsed_us=0'),('elapsed_us=4','elapsed_us=240000001'),
                             ('elapsed_us=4','elapsed_us=-1')]:
            line=self.steps[4]
            with self.subTest(before=before):
                self.assertFalse(self.decode(self.raw.replace(line,line.replace(before,after),1))['complete'])
