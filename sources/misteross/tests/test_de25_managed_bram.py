"""Unqualified graph fixtures test the closed BRAM fabric boundary."""
import copy
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest
import xml.etree.ElementTree as ET

ROOT=Path(__file__).resolve().parents[1]
SPEC=importlib.util.spec_from_file_location('bram_graph_test',ROOT/'boards/de25nano/zx81-hps/managed_bram.py')
B=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(B)


class BramGraphTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='de25-bram-graph-test-');self.addCleanup(self.temp.cleanup)
        self.project=Path(self.temp.name)
        (self.project/'qsys_top/synth').mkdir(parents=True)
        self.tree=ET.Element('Ensemble')
        self.native=ET.SubElement(self.tree,'module',{'path':'subsys_hps.agilex_hps','kind':'intel_agilex_hps'})
        self.interfaces=['h2f_warm_reset_handshake','hps2fpga','lwhps2fpga','fpga2hps_interrupt_irq0']
        for start,end,base in (('subsys_hps_agilex_hps.lwhps2fpga','subsys_periph_pb_cpu_0.s0',0),
                               ('subsys_hps_agilex_hps.hps2fpga','ocm.axi_s1',0),
                               ('subsys_periph_pb_cpu_0.m0','subsys_periph_fes_computer.avs',0x10300)):
            self.route(start,end,base)
        self.parameters={name:values['after'] for name,values in B.PARAMETERS.items()}
        self.parent='\thps_subsys subsys_hps (\n .hps2fpga_arvalid (wire0)\n\t);\n'
        path=self.project/'fes_mailbox/generated/fes_de25_computer.vh';path.parent.mkdir(parents=True)
        path.write_bytes((ROOT/'boards/de25nano/zx81-hps/generated/fes_de25_computer.vh').read_bytes())
        path=self.project/B.HPS_IP;path.parent.mkdir(parents=True);path.write_text('unqualified IP marker fixture')

    def route(self,start,end,base):
        row=ET.SubElement(self.tree,'connection',{'kind':'avalon','start':start,'end':end})
        ET.SubElement(ET.SubElement(row,'parameter',{'name':'baseAddress'}),'value').text=hex(base)

    def verify(self):
        definition=ET.Element('component');interfaces=ET.SubElement(ET.SubElement(definition,'boundary'),'interfaces')
        for name in self.interfaces:ET.SubElement(ET.SubElement(interfaces,'interface'),'name').text=name
        for row in list(self.native):self.native.remove(row)
        ET.SubElement(ET.SubElement(self.native,'parameter',{'name':'componentDefinition'}),'value').text=ET.tostring(definition,encoding='unicode')
        ET.ElementTree(self.tree).write(self.project/'qsys_top/qsys_top.sopcinfo')
        (self.project/'qsys_top/synth/qsys_top.v').write_text(self.parent)
        return B.verify(self.project,lambda _:self.parameters,lambda p:hashlib.sha256(p.read_bytes()).hexdigest())

    def test_cpu_only_graph_keeps_control_and_has_no_fabric_hps_ingress(self):
        result=self.verify();self.assertEqual(set(result['initiator_endpoints']),B.MASTERS)
        self.assertEqual(result['fabric_to_hps'],[]);self.assertFalse(result['native_acceptance'])

    def test_hidden_master_and_route_into_hps_are_both_rejected(self):
        for start,end in (('hidden.master','ocm.axi_s1'),('subsys_periph_pb_cpu_0.m0','subsys_hps_agilex_hps.f2sdram')):
            self.route(start,end,0)
            with self.assertRaisesRegex(ValueError,'initiator/ingress'):self.verify()
            self.tree.remove(self.tree.findall('connection')[-1])

    def test_removed_instance_kind_cannot_be_renamed_to_escape_inventory(self):
        row=ET.SubElement(self.tree,'module',{'path':'unrecognized_debug','kind':'altera_jtag_avalon_master'})
        with self.assertRaisesRegex(ValueError,'memory/maintenance'):self.verify()
        self.tree.remove(row)
        row=ET.SubElement(self.tree,'module',{'path':'subsys_hps.f2sdram_adapter','kind':'custom_adapter'})
        with self.assertRaisesRegex(ValueError,'memory/maintenance'):self.verify()

    def test_native_or_parent_memory_port_and_lost_warm_conduit_are_rejected(self):
        self.interfaces.append('f2sdram')
        with self.assertRaisesRegex(ValueError,'native ports'):self.verify()
        self.interfaces.remove('f2sdram');self.interfaces.remove('h2f_warm_reset_handshake')
        with self.assertRaisesRegex(ValueError,'warm-reset'):self.verify()
        self.interfaces.append('h2f_warm_reset_handshake');self.parent=self.parent.replace('.hps2fpga_arvalid','.f2sdram_arvalid')
        with self.assertRaisesRegex(ValueError,'parent retains'):self.verify()

    def test_enabled_bridge_or_changed_control_window_is_rejected(self):
        self.parameters['f2sdram_data_width']='256'
        with self.assertRaisesRegex(ValueError,'HPS parameters'):self.verify()
        self.parameters['f2sdram_data_width']='0'
        self.tree.findall('connection')[-1].find('parameter/value').text='0x10400'
        with self.assertRaisesRegex(ValueError,'control/OCM'):self.verify()


if __name__=='__main__':unittest.main()
