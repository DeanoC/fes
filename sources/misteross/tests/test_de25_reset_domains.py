"""Reject generated reset wiring that splits the shared memory/device owner."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
import xml.etree.ElementTree as ET

HERE=Path(__file__).resolve().parents[1]/'prototypes/de25_board_control'
spec=importlib.util.spec_from_file_location('de25_reset_build',HERE/'build.py')
build=importlib.util.module_from_spec(spec);spec.loader.exec_module(build)
domains=build.reset_domains


def call(module,name,ports,parameters=None):
    args=lambda values:','.join('.'+k+'('+v+')' for k,v in values.items())
    return module+(' #('+args(parameters)+')' if parameters else '')+' '+name+' ('+args(ports)+');\n'


class ResetDomainTests(unittest.TestCase):
    def fixture(self,root):
        inputs=dict(board_fes_revision=domains.rom_transport.BOARD_REVISION,computer_model='zx81',
            computer={'hps_initialization':'HPS FIRST'},reader_memory=build.reader_memory.selection('hps-ddr',0x95000000,8192),
            rom_transport=domains.rom_transport.selection(True,'zx81'))
        # Captured memory-controller requirements, including every unused input.
        parameters=dict(NUM_RESET_INPUTS='1',OUTPUT_RESET_SYNC_EDGES='"deassert"',SYNC_DEPTH='2',
            RESET_REQUEST_PRESENT='1',RESET_REQ_WAIT_TIME='1',MIN_RST_ASSERTION_TIME='3',
            RESET_REQ_EARLY_DSRT_TIME='1',ADAPT_RESET_REQUEST='0',
            **{'USE_RESET_REQUEST_IN'+str(i):'0' for i in range(16)})
        ports=dict(clk='clk_100_out_clk_clk',reset_in0='~rst_in_out_reset_reset',reset_out='rst_controller_reset_out_reset',
            reset_req='rst_controller_reset_out_reset_req',**{'reset_in'+str(i):"1'b0" for i in range(1,16)},
            **{'reset_req_in'+str(i):"1'b0" for i in range(16)})
        top=call('altera_reset_controller','rst_controller',ports,parameters)
        top+=call('rst_in','rst_in',dict(clk='clk_100_out_clk_clk',in_reset_n='reset_reset_n',out_reset_n='rst_in_out_reset_reset'))
        top+=call('ocm','ocm',dict(clk='clk_100_out_clk_clk',reset='rst_controller_reset_out_reset',reset_req='rst_controller_reset_out_reset_req'))
        top+=call('peripheral_subsys','subsys_periph',dict(clk_clk='clk_100_out_clk_clk',reset_reset_n='~rst_controller_reset_out_reset',reset_reset_req='rst_controller_reset_out_reset_req'))
        periph=call('peripheral_subsys_periph_rst_in','periph_rst_in',dict(clk='periph_clk_out_clk_clk',in_reset_n='reset_reset_n',in_reset_req='reset_reset_req',out_reset_n='periph_rst_in_out_reset_reset'))
        for name in ('fes_computer','fes_embedded','fes_rom','fes_mailbox','fes_ddr_probe'):
            periph+=call('peripheral_subsys_'+name,name,dict(clk='periph_clk_out_clk_clk',reset_n='periph_rst_in_out_reset_reset'))
        for name,clk,reset in [('button_pio','clk','reset_n'),('dipsw_pio','clk','reset_n'),('led_pio','clk','reset_n'),('sysid','clock','reset_n'),('pb_cpu_0','clk','reset')]:
            periph+=call(name,name,{clk:'periph_clk_out_clk_clk',reset:('~' if reset=='reset' else '')+'periph_rst_in_out_reset_reset'})
        periph+=call('altera_reset_controller','rst_controller',dict(clk='periph_clk_out_clk_clk'),dict(OUTPUT_RESET_SYNC_EDGES='"both"',RESET_REQUEST_PRESENT='0'))
        files={'qsys_top/synth/qsys_top.v':top,'peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v':periph}
        for path in ('ip/qsys_top/rst_in.ip','peripheral_subsys/ip/peripheral_subsys/periph_rst_in.ip'):
            document=ET.Element('component');values=ET.SubElement(ET.SubElement(document,'altera_module_parameters'),'parameters')
            for key,value in dict(ACTIVE_LOW_RESET='1',SYNCHRONOUS_EDGES='deassert',USE_RESET_REQUEST='0',SYNC_RESET='0').items():
                ET.SubElement(ET.SubElement(values,'parameter',parameterId=key),'value').text=value
            files[path]=ET.tostring(document,encoding='unicode')
        for path,text in files.items():
            p=root/path;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(text)
        source=call('fes_de25_top_reset','fes_top_reset_inst',dict(clk='system_clk_100_internal',ninit_done='ninit_done',pll_locked='pll_locked',reset_n='top_reset_n'))
        source+=call('qsys_top','soc_inst',dict(clk_100_clk='system_clk_100_internal',reset_reset_n='top_reset_n'))
        (root/'golden_top.v').write_text(source)
        local=root/'fes_mailbox';local.mkdir()
        for name in domains.SOURCES:(local/name).write_bytes((HERE/name).read_bytes())
        material=root/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_periph_rst_in'
        target=material/'fes_de25_reset_bridge_10/synth/fes_de25_reset_bridge.sv';target.parent.mkdir(parents=True)
        target.write_bytes((HERE/'fes_de25_reset_bridge.sv').read_bytes())
        wrapper=material/'synth/peripheral_subsys_periph_rst_in.v';wrapper.parent.mkdir()
        wrapper.write_text(call('fes_de25_reset_bridge','periph_rst_in',dict(clk='clk',in_reset_n='in_reset_n',in_reset_req='in_reset_req',out_reset_n='out_reset_n')))
        return inputs,files

    def test_actual_script_has_clocked_bridge_contract_and_rejects_bad_top(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);inputs,_=self.fixture(root)
            script='save_system $peripheral_system\nsave_system "'+str(root/'qsys_top.qsys')+'"\n'
            sources={'sources/misteross/prototypes/de25_board_control/'+n:(HERE/n).read_bytes() for n in domains.SOURCES}
            changed=domains.install(root,inputs,script,sources)
            self.assertEqual(changed.count('SYNCHRONOUS_EDGES deassert'),1)
            self.assertIn('add_instance periph_rst_in fes_de25_reset_bridge 1.0',changed)
            (root/'integrate-mailbox.tcl').write_text(changed)
            self.assertTrue(domains.verify(root,inputs))
            with self.assertRaises(ValueError):domains.install(root,inputs,changed,sources)
            for before,after in [('pll_locked','unqualified'),('system_clk_100_internal','wrong_clk'),('top_reset_n','raw_reset')]:
                original=(root/'golden_top.v').read_text();(root/'golden_top.v').write_text(original.replace(before,after,1))
                with self.subTest(before=before),self.assertRaises(ValueError):domains.verify(root,inputs)
                (root/'golden_top.v').write_text(original)

    def test_generated_memory_request_and_common_owner_cannot_drift(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);inputs,files=self.fixture(root)
            r=domains.verify_generated(root,inputs)
            self.assertEqual(r['peripheral_async_release_chains'],0)
            mutations=[('qsys_top/synth/qsys_top.v','.MIN_RST_ASSERTION_TIME(3)','.MIN_RST_ASSERTION_TIME(2)'),
                ('qsys_top/synth/qsys_top.v','.RESET_REQUEST_PRESENT(1)','.RESET_REQUEST_PRESENT(0)'),
                ('qsys_top/synth/qsys_top.v','.reset_req(rst_controller_reset_out_reset_req)','.reset_req()'),
                ('qsys_top/synth/qsys_top.v','.reset_reset_n(~rst_controller_reset_out_reset)','.reset_reset_n(rst_in_out_reset_reset)'),
                ('qsys_top/synth/qsys_top.v','.reset_reset_req(rst_controller_reset_out_reset_req)','.reset_reset_req()'),
                ('qsys_top/synth/qsys_top.v','.clk(clk_100_out_clk_clk)','.clk(other_clock)'),
                ('peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v','.reset_n(periph_rst_in_out_reset_reset)','.reset_n(other_reset)'),
                ('peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v','.OUTPUT_RESET_SYNC_EDGES("both")','.OUTPUT_RESET_SYNC_EDGES("deassert")'),
                ('ip/qsys_top/rst_in.ip','>deassert<','>none<')]
            for path,before,after in mutations:
                self.assertIn(before,files[path]);(root/path).write_text(files[path].replace(before,after,1))
                with self.subTest(path=path,before=before),self.assertRaises(ValueError):domains.verify_generated(root,inputs)
                (root/path).write_text(files[path])
            path='peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v'
            (root/path).write_text(files[path]+call('altera_reset_controller','extra_release',{},dict(OUTPUT_RESET_SYNC_EDGES='"deassert"')))
            with self.assertRaises(ValueError):domains.verify_generated(root,inputs)


if __name__=='__main__':unittest.main()
