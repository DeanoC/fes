"""Source-bound ROM resources and cold firmware selection; no hardware."""
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch
import xml.etree.ElementTree as ET

ROOT=Path(__file__).resolve().parents[1]
NANO=ROOT/'platform/embedded/nano'


def module(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result)
    return result


hps=module('rom_context_under_test',NANO/'hps_context.py')
cold=module('rom_cold_under_test',NANO/'cold_profile.py')


class ROMContextTests(unittest.TestCase):
    def setUp(self):
        data=ROOT/'platform/embedded/tamago/nativefabric/testdata'
        self.context=json.loads((data/'hps-context.json').read_bytes())
        self.context.update(programming_profile=hps.ROM_PROFILE,control_abi=0x10203041)
        self.control=json.loads((data/'hps-control.json').read_bytes())
        self.control['fabric_stop']={'scope':'sealed-ddr-master-stop-v1','probe_cpu_base':0x20010100,
            'probe_span':128,'maintenance':'held-reset-from-configuration'}
        self.computer=json.loads((data/'hps-computer.json').read_bytes())
        self.computer['control_map_sha256']=hps.digest(self.control)
        self.rom={'format':1,'scope':'sealed-cold-held-rom-v1',
            'package_id':self.context['package_id'],'build_id':self.context['build_id'],
            'control_map_sha256':hps.digest(self.control),'computer_map_sha256':hps.digest(self.computer),
            'handoff_sha256':self.control['handoff_sha256'],
            'cpu_base':0x20010500,'span':128,'bytes':8192,'clock_hz':100000000,
            'transport':'cpu-csr-v1','role':'firmware','held_conduit':'fes_computer.rom/fes_rom.rom',
            'hps_master':False,'source_sha256':{n:'a'*64 for n in ('fes_de25_rom.sv',
                'fes_de25_rom_hw.tcl','fes_embedded_rom.sv','fes_embedded_rom_csr.sv','generated/fes_embedded_rom.vh')}}

    def test_exact_resource_and_image_sizes(self):
        for size in (4096,8192):
            rom=dict(self.rom,bytes=size)
            self.assertEqual(hps.validate_rom(self.context,self.control,self.computer,rom),rom)

    def test_selected_metadata_requires_an_explicit_content_receipt(self):
        with patch.object(hps,'_derive',return_value=self.context), \
             patch.object(hps,'_program_platform',return_value={'platform':{'sealed':'fixture'}}), \
             patch.object(hps,'_control',return_value=self.control), \
             patch.object(hps,'_computer',return_value=self.computer), \
             patch.object(hps,'_rom',return_value=self.rom):
            for receipt in (None, '', '0'*64, 'g'*64, 'a'*63, True):
                with self.subTest(receipt=receipt),self.assertRaises(ValueError):
                    hps._selected_program('fixture',receipt)
            record=hps._selected_program('fixture','b'*64)
            self.assertEqual(record['scope'],'nano-selected-rom-program-v1')
            self.assertEqual(record['rom'],self.rom)
            self.assertEqual(record['rom_receipt_sha256'],'b'*64)
            with patch.object(hps,'_derive',return_value=dict(self.context,programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1')):
                with self.assertRaises(ValueError):hps._selected_program('fixture','b'*64)

    def test_cpu_route_requires_unique_nonoverlapping_region(self):
        # The producer seal and generated conduit checks are separate. Exercise
        # the final CPU address-map decoder with malformed handoffs here.
        for fault in ('pass','missing','duplicate-route','duplicate-map','duplicate-region',
                'wrong-start','wrong-end','wrong-width','overlap'):
            tree=ET.Element('root')
            route=ET.SubElement(tree,'connection',kind='avalon',
                start='subsys_hps_agilex_hps.lwhps2fpga',end='subsys_periph_pb_cpu_0.s0')
            parameter=ET.SubElement(route,'parameter',name='addressMapSysInfo')
            value=ET.SubElement(parameter,'value');regions=ET.Element('address-map')
            row=ET.SubElement(regions,'slave',name='subsys_periph.fes_rom.avs',
                start='0x10500',end='0x10580',datawidth='32')
            ET.SubElement(regions,'slave',name='other',start='0x10400',end='0x10500',datawidth='32')
            if fault=='missing':regions.remove(row)
            elif fault=='duplicate-region':regions.append(ET.fromstring(ET.tostring(row)))
            elif fault=='wrong-start':row.set('start','0x10400')
            elif fault=='wrong-end':row.set('end','0x10600')
            elif fault=='wrong-width':row.set('datawidth','64')
            elif fault=='overlap':ET.SubElement(regions,'slave',name='overlap',start='0x1057c',end='0x10600')
            value.text=ET.tostring(regions,encoding='unicode')
            if fault=='duplicate-map':route.append(ET.fromstring(ET.tostring(parameter)))
            if fault=='duplicate-route':tree.append(ET.fromstring(ET.tostring(route)))
            with self.subTest(fault=fault):
                if fault=='pass':hps._rom_cpu_region(ET.tostring(tree),0x10500,128)
                else:
                    with self.assertRaises(ValueError):hps._rom_cpu_region(ET.tostring(tree),0x10500,128)

    def test_mutated_or_incomplete_rom_contract_rejects(self):
        changes={'format':True,'scope':'other','package_id':'b'*64,'build_id':'b'*32,
            'control_map_sha256':'b'*64,'computer_map_sha256':'b'*64,'handoff_sha256':'b'*64,
            'cpu_base':0x20010400,'span':256,'bytes':16384,'clock_hz':74250000,
            'transport':'dma','role':'game','held_conduit':'other','hps_master':0,
            'source_sha256':{}}
        for k,v in changes.items():
            with self.subTest(field=k),self.assertRaises(ValueError):
                hps.validate_rom(self.context,self.control,self.computer,dict(self.rom,**{k:v}))
        for k in self.rom:
            rom=dict(self.rom);del rom[k]
            with self.subTest(missing=k),self.assertRaises(ValueError):
                hps.validate_rom(self.context,self.control,self.computer,rom)
        for rom in [dict(self.rom,unknown=1),dict(self.rom,bytes=True),
                dict(self.rom,source_sha256=dict(self.rom['source_sha256'],**{'fes_de25_rom.sv':'0'*64}))]:
            with self.assertRaises(ValueError):hps.validate_rom(self.context,self.control,self.computer,rom)

    def test_foreign_computer_or_missing_master_stop_rejects(self):
        for k,v in [('package_id','b'*64),('build_id','b'*32),('hps_master',True),
                ('cpu_base',0x20010500),('control_map_sha256','b'*64),('handoff_sha256','b'*64)]:
            with self.subTest(field=k),self.assertRaises(ValueError):
                hps.validate_rom(self.context,self.control,dict(self.computer,**{k:v}),self.rom)
        for stop in (None,{},dict(self.control['fabric_stop'],maintenance='unknown')):
            with self.assertRaises(ValueError):
                hps.validate_rom(self.context,dict(self.control,fabric_stop=stop),self.computer,self.rom)
        with self.assertRaises(ValueError):
            hps.validate_rom(dict(self.context,programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1'),
                self.control,self.computer,self.rom)

    def test_receipt_requires_fresh_source_equality(self):
        record={'program':'fpgaload','hps_project':'fixture','hps_context':self.context,
            'hps_rom':self.rom,'hps_rom_sha256':hps.digest(self.rom)}
        with patch.object(hps,'checked_computer',return_value=self.computer),patch.object(hps,'rom',return_value=self.rom):
            self.assertEqual(hps.checked_rom(record),self.rom)
            for bad in [dict(record,program='probe'),dict(record,hps_rom=None),dict(record,hps_rom_sha256='b'*64),
                    dict(record,hps_rom=dict(self.rom,bytes=4096),hps_rom_sha256=hps.digest(dict(self.rom,bytes=4096)))]:
                with self.assertRaises(ValueError):hps.checked_rom(bad)
            with patch.object(hps,'checked_computer',return_value=None),self.assertRaises(ValueError):hps.checked_rom(record)
        for record in ({},{'hps_context':None}):self.assertIsNone(hps.checked_rom(record))
        with self.assertRaises(ValueError):hps.checked_rom({'hps_rom':self.rom})

    def profile(self):
        config={'base':0x94800000,'size':8<<20,'core_size':128,'chunk_bytes':64,
            'core_sha384':'3'*96,'chunk_sha384':['4'*96,'5'*96]}
        memory=hps.reader_memory_contract(self.context,{'reader_memory_backend':'hps-ddr',
            'reader_memory_base':0x95000000,'reader_memory_bytes':8192})
        cpu=hps.cpu_mapping(self.control,config,memory)
        p={'sd_pio':True,'chain_sha256':'1'*64,'owner_seed':17,'entry_image_sha256':'2'*64,'entry_image_size':128}
        for name,value in [('hps_context',self.context),('hps_control',self.control),('hps_configuration',config),
                ('hps_reader_memory',memory),('cpu_mapping',cpu),('hps_computer',self.computer),('hps_rom',self.rom)]:
            p[name]=value;p[name+'_sha256']=hps.digest(value)
        return p

    def test_cold_header_uses_the_verified_resource(self):
        p=self.profile();text=cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(p)} )
        for line in ['#define FES_NANO_COLD_ROM 1','#define FES_NANO_ROM_CPU_BASE 0x20010500UL',
                '#define FES_NANO_ROM_BYTES 8192U','#define FES_NANO_ROM_MAP_SHA {']:
            self.assertIn(line,text)
        # Even a recomputed metadata digest cannot move the aperture or change
        # the held execution contract selected by firmware.
        for k,v in [('cpu_base',0x20010400),('hps_master',True),('bytes',True),('role','game')]:
            rom=dict(self.rom,**{k:v})
            with self.subTest(field=k),self.assertRaises(ValueError):
                cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(dict(p,hps_rom=rom,hps_rom_sha256=hps.digest(rom)))} )
        for bad in [dict(p,hps_rom=None),dict(p,hps_rom_sha256=None),dict(p,hps_computer=None),
                dict(p,hps_reader_memory=None),dict(p,hps_context=dict(self.context,programming_profile='other'))]:
            with self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(bad)} )

    def test_rom_header_does_not_enable_an_ordinary_cold_image(self):
        p=self.profile();p['hps_context']=dict(self.context,programming_profile='de25-embedded-hps-first-ddr-diagnostic-v1')
        p['hps_context_sha256']=hps.digest(p['hps_context'])
        with self.assertRaises(ValueError):cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(p)} )
        p['hps_rom']=p['hps_rom_sha256']=None
        self.assertIn('#define FES_NANO_COLD_ROM 0',cold.header({'boot_fit': dict(cold.fit_layout.CONTRACT), **(p)} ))


class NativeComputerContextTests(ROMContextTests):
    def setUp(self):
        super().setUp()
        self.context.update(programming_profile=hps.COMPUTER_PROFILE,
                            core_id='fes.zx81',abi='fes.computer',abi_major=1,abi_minor=0)

    def test_native_source_requires_its_own_exact_boot_contract(self):
        self.assertTrue(hps.uses_held_rom(self.context))
        for key,value in [('abi','fes.de25-embedded-diagnostic'),('abi_major',True),
                          ('abi_minor',1),('control_abi',0x10203040),('core_id','fes.other')]:
            with self.subTest(field=key),self.assertRaises(ValueError):
                hps.validate_rom(dict(self.context,**{key:value}),self.control,self.computer,self.rom)
        self.assertTrue(hps.uses_held_rom(dict(self.context,core_id='fes.zx80')))


if __name__=='__main__':unittest.main()
