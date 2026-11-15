import copy
import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET

ROOT=Path(__file__).resolve().parents[1]
HERE=ROOT/'prototypes/de25_board_control'
spec=importlib.util.spec_from_file_location('de25_rom_seal',HERE/'seal.py')
seal=importlib.util.module_from_spec(spec);spec.loader.exec_module(seal)
rom=seal.rom_transport


class ROMProducerTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.project=Path(self.temp.name);self.local=self.project/'fes_mailbox'
        (self.local/'generated').mkdir(parents=True)
        self.inputs={'board_fes_revision':rom.BOARD_REVISION,'computer_model':'zx81',
            'computer':{'hps_initialization':'HPS FIRST','source_sha256':{}},
            'reader_memory':seal.reader_memory.selection('hps-ddr',0x95000000,8192),
            'rom_transport':rom.selection(True,'zx81')}
        self.sources={'sources/misteross/prototypes/de25_board_control/'+name:(HERE/name).read_bytes() for name in rom.FILES}
        self.sources[rom.VIDEO_SOURCE]=(HERE/'video.v').read_bytes()
        self.sources[rom.COMPONENT]=subprocess.check_output(['git','-C',str(ROOT.parents[1]),'show',rom.BOARD_REVISION+':'+rom.COMPONENT])
        (self.local/'fes_de25_computer_hw.tcl').write_bytes(self.sources[rom.COMPONENT])
        (self.project/'golden_top.qsf').write_text('set_global_assignment -name SEED 1\n')
        script=rom.install(self.project,self.inputs,self.sources,'load_system periph\nsave_system $peripheral_system\n# Refresh\n')
        (self.project/'integrate-mailbox.tcl').write_text(script)

    def test_real_sources_and_selection_are_checked(self):
        self.assertEqual(rom.verify(self.project,self.inputs,self.sources),self.inputs['rom_transport'])
        for p in self.project.rglob('*'):
            if not p.is_file():continue
            original=p.read_bytes();p.write_bytes(original+b'changed\n')
            # An extra unrelated QSF assignment does not change the selected
            # ROM macro, but edits to every installed source are rejected.
            if p.name not in ('golden_top.qsf','integrate-mailbox.tcl'):
                with self.subTest(file=p.name),self.assertRaises(ValueError):rom.verify(self.project,self.inputs,self.sources)
            p.write_bytes(original)
        for mutation in ('size','address','span','model','boot','memory','board','macro','route'):
            with self.subTest(mutation=mutation):
                changed=copy.deepcopy(self.inputs)
                if mutation=='size':changed['rom_transport']['bytes']=4096
                if mutation=='address':changed['rom_transport']['peripheral_offset']+=128
                if mutation=='span':changed['rom_transport']['span']=True
                if mutation=='model':changed['computer_model']='zx80'
                if mutation=='boot':changed['computer']['hps_initialization']='AFTER INIT_DONE'
                if mutation=='memory':changed['reader_memory']=seal.reader_memory.selection('onchip',0,0)
                if mutation=='board':changed['board_fes_revision']='a'*40
                path=self.project/('golden_top.qsf' if mutation=='macro' else 'integrate-mailbox.tcl')
                original=path.read_text()
                if mutation=='macro':path.write_text(original.replace('FES_DE25_EXTERNAL_ROM=1','FES_DE25_EXTERNAL_ROM=0'))
                if mutation=='route':path.write_text(original.replace('0x10500','0x10600'))
                with self.assertRaises(ValueError):rom.verify(self.project,changed,self.sources)
                path.write_text(original)
        self.assertEqual(seal.profile_for(self.inputs),seal.HPS_ROM_PROFILE)
        self.assertEqual(rom.selection(True,'zx80')['bytes'],4096)

    def generated(self):
        tree=ET.Element('EnsembleReport')
        avs={'address':('Input',7),'read':('Input',1),'write':('Input',1),'byteenable':('Input',4),
            'writedata':('Input',32),'readdata':('Output',32),'readdatavalid':('Output',1),
            'writeresponsevalid':('Output',1),'response':('Output',2),'waitrequest':('Output',1)}
        conduit={'address':('Input',13),'data':('Output',8),'held':('Input',1),'loaded':('Output',1),'failed':('Output',1)}
        props={'associatedClock':'clk','associatedReset':'reset','addressUnits':'SYMBOLS','bitsPerSymbol':'8',
            'explicitAddressSpan':'128','maximumPendingReadTransactions':'1','maximumPendingWriteTransactions':'1',
            'minimumResponseLatency':'1','waitrequestAllowance':'0'}
        for name,kind,points in [('fes_rom','fes_de25_rom',{'clk':{},'reset':{},'avs':avs,'rom':conduit}),
            ('fes_computer','fes_de25_computer',{'rom':{k:('Output' if d=='Input' else 'Input',w) for k,(d,w) in conduit.items()}})]:
            node=ET.SubElement(tree,'module',path='subsys_periph.'+name,kind=kind,version='1.0')
            definition=ET.Element('component');interfaces=ET.SubElement(ET.SubElement(definition,'boundary'),'interfaces')
            for key,roles in points.items():
                point=ET.SubElement(interfaces,'interface');ET.SubElement(point,'name').text=key
                ports=ET.SubElement(point,'ports')
                for role,(direction,width) in roles.items():
                    p=ET.SubElement(ports,'port')
                    for field,value in [('role',role),('direction',direction),('width',str(width))]:ET.SubElement(p,field).text=value
                if key=='avs':
                    entries=ET.SubElement(ET.SubElement(point,'parameters'),'parameterValueMap')
                    for k,v in props.items():
                        entry=ET.SubElement(entries,'entry');ET.SubElement(entry,'key').text=k;ET.SubElement(entry,'value').text=v
            ET.SubElement(ET.SubElement(node,'parameter',name='componentDefinition'),'value').text=ET.tostring(definition,encoding='unicode')
        def connection(kind,start,end,parameters={}):
            c=ET.SubElement(tree,'connection',kind=kind,start='subsys_periph_'+start,end='subsys_periph_'+end)
            for k,v in parameters.items():ET.SubElement(ET.SubElement(c,'parameter',name=k),'value').text=v
        connection('avalon','pb_cpu_0.m0','fes_rom.avs',{'baseAddress':'0x10500'})
        for endpoint in ('fes_rom','fes_computer'):
            connection('clock','periph_clk.out_clk',endpoint+'.clk',{'clockRateSysInfo':'100000000'})
            connection('reset','periph_rst_in.out_reset',endpoint+'.reset')
        connection('conduit','fes_computer.rom','fes_rom.rom')
        metadata=self.project/'qsys_top/qsys_top.sopcinfo';metadata.parent.mkdir();ET.ElementTree(tree).write(metadata)
        material=self.project/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_rom'
        for name in rom.FILES:
            if name.endswith('_hw.tcl'):continue
            target=material/'fes_de25_rom_10/synth'/name;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(self.local/name,target)
        wrapper=material/'synth/peripheral_subsys_fes_rom.v';wrapper.parent.mkdir()
        wrapper.write_text('module peripheral_subsys_fes_rom #(parameter ROM_BYTES=8192) ();\nfes_de25_rom #(.ROM_BYTES(ROM_BYTES)) fes_rom();\nendmodule\n')
        parent=self.project/'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v';parent.parent.mkdir(parents=True)
        parent.write_text('peripheral_subsys_fes_rom fes_rom();\n')
        return tree,metadata,wrapper,parent

    def test_generated_routes_and_real_sources_cannot_drift(self):
        tree,metadata,wrapper,parent=self.generated()
        self.assertFalse(rom.verify_generated(self.project,self.inputs)['new_dma_master'])
        for mutation in ('address','clock','reset','conduit','duplicate','extra-master','port','property','size','build','override','source'):
            with self.subTest(mutation=mutation):
                changed=copy.deepcopy(tree);original_wrapper=wrapper.read_text();original_parent=parent.read_text()
                if mutation=='address':changed.find("connection[@kind='avalon']/parameter/value").text='0x10600'
                if mutation=='clock':changed.find("connection[@kind='clock']/parameter/value").text='50000000'
                if mutation in ('reset','conduit'):changed.remove(changed.find("connection[@kind='"+mutation+"']"))
                if mutation=='duplicate':changed.append(copy.deepcopy(changed.find("connection[@kind='avalon']")))
                if mutation in ('extra-master','port','property'):
                    text=changed.find("module/parameter/value");definition=ET.fromstring(text.text)
                    if mutation=='extra-master':ET.SubElement(ET.SubElement(definition.find('boundary/interfaces'),'interface'),'name').text='dma'
                    if mutation=='port':definition.find("boundary/interfaces/interface[name='rom']/ports/port/width").text='12'
                    if mutation=='property':definition.find("boundary/interfaces/interface[name='avs']/parameters/parameterValueMap/entry[key='addressUnits']/value").text='WORDS'
                    text.text=ET.tostring(definition,encoding='unicode')
                if mutation=='size':wrapper.write_text(original_wrapper.replace('8192','4096'))
                if mutation=='build':wrapper.write_text(original_wrapper.replace('.ROM_BYTES(ROM_BYTES)',".ROM_BYTES(ROM_BYTES),.BUILD_ID(128'd0)"))
                if mutation=='override':parent.write_text('peripheral_subsys_fes_rom #(.ROM_BYTES(4096)) fes_rom();\n')
                source=wrapper.parent.parent/'fes_de25_rom_10/synth/fes_de25_rom.sv';original_source=source.read_bytes()
                if mutation=='source':source.write_bytes(original_source+b'changed\n')
                ET.ElementTree(changed).write(metadata)
                with self.assertRaises(ValueError):rom.verify_generated(self.project,self.inputs)
                wrapper.write_text(original_wrapper);parent.write_text(original_parent);source.write_bytes(original_source)
        ET.ElementTree(tree).write(metadata)
        self.assertEqual(rom.verify_generated(self.project,self.inputs)['bytes'],8192)
