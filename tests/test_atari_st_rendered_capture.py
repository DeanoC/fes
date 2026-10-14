"""Authored HDMI patterns verify the full replay canvas and torn-pixel rejection."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from scripts import analyse_atari_st_raster_trace as raster

ROOT = Path(__file__).resolve().parents[1]


class RenderedCaptureTests(unittest.TestCase):
    def test_actual_hdmi_geometry_and_four_sample_agreement(self):
        compiler = shutil.which('g++')
        if compiler is None:
            self.skipTest('g++ unavailable')
        source = r'''
#include "st_rendered_capture.hpp"
#include <cassert>
int main() {
    for (bool pal : {false, true}) {
        const unsigned height=pal ? 276 : 255, top=pal ? 84 : 105;
        std::vector<uint8_t> full(1280*720*3, 251), expected;
        for (unsigned y=0;y<height;++y) for (unsigned x=0;x<416;++x)
            for (unsigned c=0;c<3;++c) {
                const uint8_t value=(x*13+y*7+c*83)%251;
                expected.push_back(value);
                for (unsigned dy=0;dy<2;++dy) for (unsigned dx=0;dx<2;++dx)
                    full[((top+2*y+dy)*1280+224+2*x+dx)*3+c]=value;
            }
        auto result=st_rendered_capture::canvas(full,pal);
        assert(result.top==top && result.height==height);
        assert(result.repeated && result.rgb==expected);
        // Last corner's fourth sample must also be checked, not just top-left.
        full[((top+height*2-1)*1280+1055)*3+2]^=1;
        assert(!st_rendered_capture::canvas(full,pal).repeated);
    }
    bool rejected=false;
    try { st_rendered_capture::canvas(std::vector<uint8_t>(7),true); }
    catch (const std::invalid_argument&) { rejected=true; }
    assert(rejected);
}
'''
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)
            (path/'test.cpp').write_text(source)
            subprocess.run([compiler,'-std=c++17','-Wall','-Wextra','-Werror',
                '-I',str(ROOT/'sources/misteross/cores/fes-atari-st/sim'),
                str(path/'test.cpp'),'-o',str(path/'test')],check=True,capture_output=True)
            subprocess.run([str(path/'test')],check=True,capture_output=True)

    def test_sealed_replay_pair_and_pixel_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            full_name, native_name = 'demo-rendered-1-hdmi.ppm', 'demo-rendered-1-canvas.ppm'
            (path/full_name).write_bytes(b'P6\n1280 720\n255\n'+bytes(1280*720*3))
            (path/native_name).write_bytes(b'P6\n416 276\n255\n'+bytes(416*276*3))
            entry = {'hdmi_frame': 1, 'native_frame': 23, 'start_cycle': 10, 'end_cycle': 20,
                     'pal': True, 'valid': True, 'canvas_xywh': [224,84,832,552],
                     'repeat_2x2': True, 'hdmi_file': full_name, 'canvas_file': native_name}
            proof = {'rendered_capture': {'trace':'demo-rendered.jsonl'}, 'artifacts':{}}
            def seal():
                (path/'demo-rendered.jsonl').write_text(json.dumps(entry)+'\n')
                proof['artifacts'] = {p.name:hashlib.sha256(p.read_bytes()).hexdigest()
                                      for p in path.iterdir()}
            seal()
            result = raster.rendered_capture(path,proof)
            self.assertEqual(result['saved_frames'][0]['native_frame'],23)
            self.assertEqual(result['nonrepeated_frames'],[])
            self.assertEqual(result['unsaved_replication_claims'],[])
            # Matching manifest hashes cannot conceal a bad derived image.
            data=bytearray((path/native_name).read_bytes());data[-1]=1
            (path/native_name).write_bytes(data);seal()
            with self.assertRaisesRegex(ValueError,'differs from HDMI'):
                raster.rendered_capture(path,proof)
            (path/native_name).write_bytes(b'P6\n416 276\n255\n'+bytes(416*276*3));seal()
            with (path/full_name).open('ab') as stream: stream.write(b'x')
            with self.assertRaisesRegex(ValueError,'digest mismatch'):
                raster.rendered_capture(path,proof)

    def test_torn_hdmi_claim_and_unsafe_artifact_name(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)
            full=bytearray(1280*720*3)
            full[((84+1)*1280+225)*3]=1
            (path/'demo-full.ppm').write_bytes(b'P6\n1280 720\n255\n'+full)
            entry={'hdmi_frame':1,'native_frame':7,'start_cycle':10,'end_cycle':20,
                   'pal':True,'valid':True,'canvas_xywh':[224,84,832,552],
                   'repeat_2x2':False,'hdmi_file':'demo-full.ppm','canvas_file':''}
            proof={'rendered_capture':{'trace':'demo-rendered.jsonl'},'artifacts':{}}
            def seal():
                (path/'demo-rendered.jsonl').write_text(json.dumps(entry)+'\n')
                proof['artifacts']={p.name:hashlib.sha256(p.read_bytes()).hexdigest()
                                     for p in path.iterdir()}
            seal()
            checked = raster.rendered_capture(path,proof)
            self.assertEqual(checked['nonrepeated_frames'],[1])
            self.assertEqual(checked['unsaved_replication_claims'],[])
            entry['repeat_2x2']=True;seal()
            with self.assertRaisesRegex(ValueError,'replication claim'):
                raster.rendered_capture(path,proof)
            entry['hdmi_file']='../demo-full.ppm';seal()
            with self.assertRaisesRegex(ValueError,'artifact name'):
                raster.rendered_capture(path,proof)

    def test_unsaved_replication_claim_is_not_a_pixel_finding(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            full = bytearray(1280*720*3)
            full[((84+1)*1280+225)*3] = 1
            (path/'demo-full.ppm').write_bytes(b'P6\n1280 720\n255\n'+full)
            saved = {'hdmi_frame':1,'native_frame':7,'start_cycle':10,'end_cycle':20,
                     'pal':True,'valid':True,'canvas_xywh':[224,84,832,552],
                     'repeat_2x2':False,'hdmi_file':'demo-full.ppm','canvas_file':''}
            unsaved = {'hdmi_frame':2,'native_frame':8,'start_cycle':30,'end_cycle':40,
                       'pal':True,'valid':True,'canvas_xywh':[224,84,832,552],
                       'repeat_2x2':False,'hdmi_file':'','canvas_file':''}
            proof = {'rendered_capture':{'trace':'demo-rendered.jsonl'},'artifacts':{}}
            def seal(*rows):
                (path/'demo-rendered.jsonl').write_text(''.join(json.dumps(row)+'\n' for row in rows))
                proof['artifacts'] = {p.name:hashlib.sha256(p.read_bytes()).hexdigest()
                                       for p in path.iterdir()}
            seal(unsaved)
            claimed_only = raster.rendered_capture(path,proof)
            self.assertEqual(claimed_only['nonrepeated_frames'],[])
            self.assertEqual(claimed_only['saved_frames'],[])
            self.assertEqual(claimed_only['unsaved_replication_claims'],[{
                'hdmi_frame':2,'native_frame':8,'valid':True,
                'repeat_2x2_claimed':False,'pixel_verified':False}])
            seal(saved, unsaved)
            mixed = raster.rendered_capture(path,proof)
            self.assertEqual(mixed['nonrepeated_frames'],[1])
            self.assertEqual(mixed['unsaved_replication_claims'][0]['hdmi_frame'],2)
            self.assertFalse(mixed['unsaved_replication_claims'][0]['pixel_verified'])
            unsaved['valid'] = False
            unsaved['repeat_2x2'] = True
            seal(unsaved)
            invalid = raster.rendered_capture(path,proof)['unsaved_replication_claims'][0]
            self.assertFalse(invalid['valid'])
            self.assertTrue(invalid['repeat_2x2_claimed'])
            unsaved['repeat_2x2'] = 'false';seal(unsaved)
            with self.assertRaisesRegex(ValueError,'metadata is invalid'):
                raster.rendered_capture(path,proof)
            unsaved['repeat_2x2'] = False
            unsaved['valid'] = 'true';seal(unsaved)
            with self.assertRaisesRegex(ValueError,'metadata is invalid'):
                raster.rendered_capture(path,proof)
            unsaved['valid'] = True
            unsaved['canvas_file'] = 'demo-canvas.ppm';seal(unsaved)
            with self.assertRaisesRegex(ValueError,'lacks HDMI source'):
                raster.rendered_capture(path,proof)
