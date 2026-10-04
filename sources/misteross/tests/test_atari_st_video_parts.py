"""ST video physical boundary and archive publication fences; host only."""
import copy
from contextlib import ExitStack
import hashlib
import shutil
import tarfile
import json
import re
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
from scripts import atari_st_video_parts as layout, atari_st_slot
from scripts import build_atari_st_video_part as producer
from tests import test_video_parts_build as shared
from tests.test_atari_st_slot_card import frozen_shell, good_timing
from scripts.cyclonev_rbf import LoadedRbf, SX120F, cram_set

ROOT=Path(__file__).resolve().parents[1]

def fixture(routed=False):
    with patch.object(shared,"video_parts",layout):
        top=shared.video_boundary_fixture(routed=routed)
    if routed:
        shared.add_boundary_route_throughs(top)
        cpu=frozen_shell()["modules"]["top"]
        for name,cell in cpu["cells"].items():
            if name=="machine.keep":continue
            cell=copy.deepcopy(cell)
            if name.startswith("expansion."):
                for port,bits in cell["connections"].items():
                    cell["connections"][port]=[bit+100000 if type(bit)==int and bit>=1000 else bit for bit in bits]
            top["cells"][name]=cell
        top["netnames"].update({name:value for name,value in cpu["netnames"].items() if name!="pixel_clk"})
    return top


def package_fields():
    return {"format":3, "core":{"id":"fes.atari-st"}, "build":{"id":"b"*32},
            "abi":{"id":"fes.computer","major":1,"minor":0}, "rom":{"role":"firmware"},
            "interfaces":[{"id":layout.INTERFACE,"major":1,"minor":0,"required":False},
                          {"id":"fes.expansion.atari-st-bus","major":1,"minor":0,"required":False}]}


class CompilerFixture:
    """Host-only mocked executions; real hashes, scaffolding and CRAM fences."""
    def __init__(self, root):
        self.root, self.shell = root, root / 'shell'
        self.shell.mkdir()
        for relative in producer.INPUTS:
            path=root / relative;path.parent.mkdir(parents=True,exist_ok=True)
            shutil.copyfile(ROOT / relative,path)
        self.package=SimpleNamespace(fields=package_fields(),manifest_bytes=b'manifest',payload_bytes=b'shell',rom_map_bytes=b'rom-map',package_id='a'*64)
        qsf=layout.shell_qsf('FES_RESERVED_RECT "expansion 24 1 28 18"\nFES_RESERVED_RECT "ram_guard 26 19 26 19"\nFES_RESERVED_RECT "video_ram_low 26 40 26 40"\nFES_RESERVED_RECT "video_ram_high 26 59 26 59"\n')
        for name,data in (('manifest.toml',b'manifest'),('core.rbf',b'shell'),('rom-map.json',b'rom-map'),('socket.qsf',qsf.encode()),('routed.json',json.dumps({'modules':{'top':fixture(True)}}).encode())):
            (self.shell/name).write_bytes(data)
        self.tools={name:SimpleNamespace(path=root/name,identity=name+'-authenticated') for name in ('yosys','nextpnr-mistral','mistral')}
        self.invocation=SimpleNamespace(inputs={'version':1,'frozen':'test'},env={},verify=lambda:None,close=lambda:None)
        die=SimpleNamespace(cram_sx=4096,cram_sy=6000,x_to_bx=SX120F.x_to_bx,ecc_columns=SX120F.ecc_columns)
        self.base=LoadedRbf(die,b'same-header',bytearray(die.cram_sx*die.cram_sy//8),True)
        self.placed=LoadedRbf(die,b'same-header',bytearray(self.base.cram),True)
        cram_set(self.placed.cram,die,layout.CRAM[0],layout.CRAM[1],1)
        self.timing=good_timing();self.timing['utilization']={}
        self.clock=9000;self.synthesis={'modules':{'cart':{'cells':{}}}}
        self.commands=[];self.after_route=lambda output:None
    def closure(self,root,roots,**kwargs):
        return {name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in producer.INPUTS}
    def compiler(self,command,root,log,**kwargs):
        self.commands.append(command)
        if Path(command[0]).name=='yosys':
            output=Path(command[-1].split('write_json ',1)[1]);output.write_text(json.dumps(self.synthesis));log.write_text('synthesis complete\n')
        else:
            output=Path(command[command.index('--rbf')+1]).parent
            (output/'cart.rbf').write_bytes(b'placed-video')
            (output/'timing.json').write_text(json.dumps(self.timing))
            top=fixture(True);top['cells']['fes_cart$parity']={'type':'MISTRAL_FF','connections':{'CLK':[self.clock]}}
            (output/'cart-routed.json').write_text(json.dumps({'modules':{'top':top}}))
            log.write_text('Info: backend hip:fixture ready\nInfo: Program finished normally.\n')
            self.after_route(output)
    def build(self,**kwargs):
        with ExitStack() as stack:
            for name,options in (
                ('read_package',{'return_value':self.package}),
                ('_require_clean_source',{'return_value':('repo','d'*40)}),
                ('source_input_closure',{'side_effect':self.closure}),
                ('FunctionalInvocation',{'return_value':self.invocation}),
                ('_run_tool',{'side_effect':self.compiler}),
                ('rbf_load',{'side_effect':[self.base,self.placed]}),
                ('rbf_save',{'return_value':b'linked-video'})):
                stack.enter_context(patch.object(producer,name,**options))
            stack.enter_context(patch.object(producer.shell_recipe,'_authenticate_atari_st_tools',return_value=self.tools))
            # Full shell RAM audit is separately tested by its owner. The actual
            # video/CPU boundary and PLL replay shape remain in this fixture.
            stack.enter_context(patch.object(producer.shell_recipe,'validate_routed_shell'))
            return producer.build(self.root,self.shell,self.root/'package','scanlines',**kwargs)

class Boundary(unittest.TestCase):
    def test_rtl_bels_and_disjoint_reservations(self):
        text=(ROOT/layout.RTL).read_text()
        actual={name:bel for bel,name in re.findall(r'BEL = "([^"]+)" \*\) MISTRAL_FF ([a-z0-9_]+)',text)}
        self.assertEqual(actual,layout.boundary_bels())
        self.assertEqual(len(actual),93)
        base='set_global_assignment -name FES_RESERVED_RECT "expansion 24 1 28 18"\nset_global_assignment -name FES_RESERVED_RECT "ram_guard 26 19 26 19"\nset_global_assignment -name FES_RESERVED_RECT "video_ram_low 26 40 26 40"\nset_global_assignment -name FES_RESERVED_RECT "video_ram_high 26 59 26 59"\n'
        combined=layout.shell_qsf(base)
        self.assertTrue(combined.startswith(base))
        self.assertIn('"video 24 41 28 58"',combined)
        self.assertLess(atari_st_slot.SOCKETS[0].cram[3],layout.CRAM[1])
        for bad in ('',base+base,combined):
            with self.assertRaises(ValueError):layout.shell_qsf(bad)
    def test_scaffold_keeps_cpu_all_anchors_routes_and_pairs(self):
        top=fixture(True)
        layout.validate_boundary(top,routed=True)
        result=json.loads(layout.prepare_scaffold(json.dumps({"modules":{"top":top}}).encode()))["modules"]["top"]
        self.assertEqual(top["netnames"],result["netnames"])
        self.assertEqual(len(top["cells"]),len(result["cells"]))
        for name,cell in top["cells"].items():
            if name.startswith("expansion.") or name.startswith(layout.PREFIX+"clock_coverage_"):
                self.assertEqual(cell,result["cells"][name])
        for kind,target,count in (("request","addr",32),("response","rdata",28)):
            for bit in range(count):
                for suffix in ('','$ROUTETHRU'):
                    self.assertEqual(top["cells"][layout.PREFIX+f'plug_{kind}_ff_{bit}'+suffix],result["cells"][f'plug_{target}_ff_{bit}'+suffix])
        self.assertEqual(result["cells"]["system_clock.pll"]["connections"]["outclk[1]"],[20])
    def test_boundary_rejects_source_clock_response_and_rogue_pair(self):
        layout.validate_boundary(fixture(),routed=False)
        for name,port,value in (("plug_request_ff_0","CLK",[0]),("plug_request_ff_0","DATAIN",[101]),("plug_response_ff_27","DATAIN",[1])):
            top=fixture();top["cells"][layout.PREFIX+name]["connections"][port]=value
            with self.assertRaises(ValueError):layout.validate_boundary(top,routed=False)
        top=fixture(True)
        top["cells"]["rogue"]={"type":"MISTRAL_BUF","attributes":{"NEXTPNR_BEL":"MISTRAL_COMB.25.50.0"},"connections":{}}
        with self.assertRaises(ValueError):layout.validate_boundary(top,routed=True)
        top=fixture(True);top["cells"][layout.PREFIX+"plug_request_ff_0$ROUTETHRU"]["connections"]["Q"]=[50000]
        with self.assertRaises(ValueError):layout.validate_boundary(top,routed=True)
    def test_pll_or_canonical_alias_mutations_fail_closed(self):
        top=fixture(True);top["cells"]["plug_addr_ff_0"]={}
        with self.assertRaises(ValueError):layout.prepare_scaffold(json.dumps({"modules":{"top":top}}).encode())
        top=fixture(True);top["cells"]["system_clock.pll"]["attributes"]["FES_PINMAP_V1"]='00'
        with self.assertRaises((ValueError,UnicodeDecodeError)):layout.prepare_scaffold(json.dumps({"modules":{"top":top}}).encode())

class Publication(unittest.TestCase):
    def test_closed_package_profile(self):
        fields=package_fields()
        self.assertIs(producer.package_profile(SimpleNamespace(fields=fields)),layout)
        for key,value in (("format",2),("core",{"id":"fes.coleco"}),("interfaces",[]),("abi",{"id":"fes.application","major":1,"minor":0}),("rom",{"role":"cartridge"})):
            bad=copy.deepcopy(fields);bad[key]=value
            with self.assertRaises(ValueError):producer.package_profile(SimpleNamespace(fields=bad))
    def test_publication_refuses_outside_changes_and_removes_partial_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            out=Path(tmp)
            with self.assertRaises(ValueError):producer.write_cram_report(out,b'cart',{"bits_outside_slot":1},part_id='a'*64)
            with patch.object(producer,"write_cram_report",side_effect=[RuntimeError('write failed'),None]):
                with self.assertRaises(RuntimeError):producer.publish_archive(out,'a'*64,b'{}',b'cart',{"bits_outside_slot":0},{})
            self.assertFalse(list(out.glob('*.tar*')))
    def test_video_clock_pins_only_and_bounded_seed(self):
        routed={"modules":{"top":{"netnames":{"pixel_clk":{"bits":[1]}},"cells":{"fes_cart$parity":{"type":"MISTRAL_FF","connections":{"CLK":[1]}}}}}}
        self.assertEqual(producer.validate_clocks(routed),1)
        routed["modules"]["top"]["cells"]["fes_cart$parity"]["connections"]["CLK"]=[2]
        with self.assertRaises(ValueError):producer.validate_clocks(routed)
        for seed in (0,11,True):
            with self.assertRaisesRegex(ValueError,'seed'):producer.build(ROOT,ROOT,ROOT,'direct',seed=seed)


class FullProducer(unittest.TestCase):
    def test_frozen_identity_guards_all_clocks_strict_cram_and_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            f=CompilerFixture(Path(tmp));archive=f.build(seed=5)
            summary=json.loads((archive.parent/'build-summary.json').read_bytes())
            self.assertEqual(summary['recipe']['placer_seed'],5)
            self.assertEqual(summary['recipe']['inputs'],f.closure(f.root,[]))
            self.assertEqual(summary['cram_diff']['bits_inside_slot'],1)
            self.assertEqual(summary['cram_diff']['bits_outside_slot'],0)
            self.assertEqual(summary['checked_clock_pins'],1)
            self.assertEqual((archive.parent/'cart.qsf').read_bytes(),(f.shell/'socket.qsf').read_bytes())
            for guard in ('ram_guard 26 19','video_ram_low 26 40','video_ram_high 26 59'):
                self.assertIn(guard,(archive.parent/'cart.qsf').read_text())
            self.assertIn('--no-pack',f.commands[1]);self.assertEqual(f.commands[1][f.commands[1].index('--seed')+1],'5')
            with tarfile.open(archive) as packed:
                self.assertEqual(packed.getnames(),['manifest.json','cart.rbf'])
                manifest=json.load(packed.extractfile('manifest.json'))
                self.assertEqual(manifest['shell_package_id'],f.package.package_id)
                self.assertEqual(manifest['map'],layout.MAP)
    def test_failures_never_publish_a_part(self):
        for name in ('pixel timing','system timing','audio timing','wrong clock','RAM','header','outside strict column','source mutation','generated mutation','map mismatch'):
            with self.subTest(name=name),tempfile.TemporaryDirectory() as tmp:
                f=CompilerFixture(Path(tmp))
                if name.endswith('timing'):
                    clock={'pixel timing':'pixel_clk','system timing':'system_clock.clocks[0]','audio timing':'system_clock.clocks[1]'}[name]
                    f.timing['fmax'][clock]['achieved']=1
                elif name=='wrong clock':f.clock=10
                elif name=='RAM':f.synthesis['modules']['cart']['cells']['ram']={'type':'MISTRAL_M10K'}
                elif name=='header':f.placed.header=b'changed-header'
                elif name=='outside strict column':cram_set(f.placed.cram,f.placed.die,3921,3500,1)
                elif name=='source mutation':f.after_route=lambda output:(f.root/producer.SOURCES[0]).write_bytes(b'changed-source')
                elif name=='generated mutation':f.after_route=lambda output:(output/'scaffold.json').write_bytes(b'changed-scaffold')
                elif name=='map mismatch':(f.shell/'rom-map.json').write_bytes(b'changed-map')
                with self.assertRaises((ValueError,RuntimeError)):f.build()
                self.assertFalse(list(f.root.glob('build/atari-st-video-parts/**/*.tar')))
