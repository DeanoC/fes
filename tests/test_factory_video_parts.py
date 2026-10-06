import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts import factory_video_parts as video


class VideoIndexTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tree = self.root / "tree"
        self.addCleanup(lambda: video._remove(self.tree))
        self.package_id = "a" * 64
        self.files = {}
        parts = []
        for profile, identity in (("direct", "b" * 64), ("scanlines", "c" * 64)):
            relative = f"{self.package_id}/{identity}.tar"
            archive = (profile + " opaque archive").encode()
            self.files[relative] = archive
            parts.append(dict(profile=profile, part_id=identity, archive_path=relative,
                              archive_sha256=hashlib.sha256(archive).hexdigest(), archive_size=len(archive)))
        self.index = dict(version=1, packages=[dict(package_id=self.package_id, parts=parts)])

    def publish(self, index=None):
        return video._publish(self.tree, self.files | {"index.json": video.canonical(index or self.index)})

    def rewrite(self, name, data):
        path = self.tree / name
        path.chmod(0o644)
        path.write_bytes(data)
        path.chmod(0o444)

    def test_closed_inventory_is_canonical_sealed_and_idempotent(self):
        self.publish()
        self.assertEqual(video.read_index(self.tree), self.index)
        before = (self.tree / "index.json").stat().st_ino
        self.publish()
        self.assertEqual((self.tree / "index.json").stat().st_ino, before)
        self.assertEqual((self.tree / "index.json").stat().st_mode & 0o777, 0o444)
        self.assertEqual((self.tree / self.package_id).stat().st_mode & 0o777, 0o555)

    def test_canonical_json_and_exact_schema(self):
        self.publish()
        for data in (json.dumps(self.index, indent=2).encode(), video.canonical(self.index)[:-1],
                     video.canonical(self.index | {"extra": 1}),
                     video.canonical(self.index | {"version": True}),
                     video.canonical(dict(version=1, packages=[]))):
            with self.subTest(data=data[:60]):
                self.rewrite("index.json", data)
                with self.assertRaises(ValueError):
                    video.read_index(self.tree)

    def test_profiles_order_duplicates_and_exact_pair(self):
        self.publish()
        original = self.index["packages"][0]["parts"]
        for parts in (list(reversed(original)), original[:1], [original[0], original[0]],
                      [original[0], original[1] | {"profile": "crt"}],
                      [original[0], original[1] | {"part_id": original[0]["part_id"]}]):
            with self.subTest(parts=parts):
                changed = copy.deepcopy(self.index)
                changed["packages"][0]["parts"] = parts
                self.rewrite("index.json", video.canonical(changed))
                with self.assertRaises(ValueError):
                    video.read_index(self.tree)

    def test_path_traversal_digest_size_and_boolean_size_rejected(self):
        self.publish()
        for change in ({"archive_path": "../outside.tar"}, {"archive_path": "b" * 64 + "/bad.tar"},
                       {"archive_sha256": "0" * 64}, {"archive_size": 1},
                       {"archive_size": True}, {"archive_size": video.MAX_ARCHIVE_BYTES + 1}):
            with self.subTest(change=change):
                changed = copy.deepcopy(self.index)
                changed["packages"][0]["parts"][0].update(change)
                self.rewrite("index.json", video.canonical(changed))
                with self.assertRaises(ValueError):
                    video.read_index(self.tree)

    def test_hash_tampering_and_unsealed_or_linked_members(self):
        self.publish()
        relative = self.index["packages"][0]["parts"][0]["archive_path"]
        self.rewrite(relative, b"tampered")
        with self.assertRaisesRegex(ValueError, "digest or size"):
            video.read_index(self.tree)
        self.rewrite(relative, self.files[relative])
        (self.tree / relative).chmod(0o400)
        with self.assertRaisesRegex(ValueError, "required mode"):
            video.read_index(self.tree)
        (self.tree / relative).chmod(0o444)
        (self.tree / self.package_id).chmod(0o755)
        (self.tree / relative).unlink()
        (self.tree / relative).symlink_to(self.root / "elsewhere")
        with self.assertRaisesRegex(ValueError, "linked or special"):
            video.read_index(self.tree, sealed=False)

    def test_unlisted_tree_members_rejected_and_publication_does_not_replace(self):
        self.publish()
        self.tree.chmod(0o755)
        (self.tree / "unexpected").mkdir()
        (self.tree / "unexpected").chmod(0o555)
        self.tree.chmod(0o555)
        with self.assertRaisesRegex(ValueError, "unexpected members"):
            video.read_index(self.tree)
        with self.assertRaisesRegex(ValueError, "immutable video artifacts differ"):
            self.publish()

    def test_failed_publication_leaves_no_partial_destination(self):
        original = Path.rename
        def fail(path, destination):
            if path.name.startswith(".publish-video-"):
                raise OSError("injected rename failure")
            return original(path, destination)
        with patch.object(Path, "rename", fail), self.assertRaisesRegex(OSError, "injected"):
            self.publish()
        self.assertFalse(self.tree.exists())
        self.assertEqual(list(self.root.iterdir()), [])


_FIXTURE = r'''
import hashlib, io, json, subprocess, sys, tarfile
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from scripts import build_fes_coleco_socket_v2 as shell_producer, build_video_part as producer, video_parts, native_video_parts, native_video_clock
from scripts import build_fes_atari_st_oss, build_atari_st_video_part, atari_st_video_parts
from scripts.export_core_package import build_identity, source_input_closure, POLICY
from scripts.functional_execution import source_roots_for_inputs
from scripts.core_package import read_package, package_identity
from scripts.cyclonev_rbf import SX120F, LoadedRbf, header_nbytes, cram_set, rbf_save, rbf_load, CramRect, classify_cram_diff, overlay_cram
root, output=Path(sys.argv[1]), Path(sys.argv[2])
native=len(sys.argv)>3 and sys.argv[3]=='native'
st=len(sys.argv)>3 and sys.argv[3]=='st'
if st: shell_producer, producer=build_fes_atari_st_oss, build_atari_st_video_part
layout=atari_st_video_parts if st else (native_video_parts if native else video_parts)
strict=native or st
clock_producer=producer.slot_recipe if st else producer.sgm
options={} if st else ({'native_video':True} if native else {'video_socket':True})
inputs=producer.NATIVE_INPUTS if native else producer.INPUTS
def enc(value): return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def sha(value): return hashlib.sha256(value).hexdigest()
revision=subprocess.check_output(['git','-C',str(root),'rev-parse','HEAD'],text=True).strip()
current={'inputs':source_input_closure(root,source_roots_for_inputs(inputs),policy=POLICY),
         'source_roots':source_roots_for_inputs(inputs),'source_closure_policy':POLICY,
         'tools':{'yosys':'synthetic validated fixture'},'execution':{'version':1,'gpu_device':0},'revision':revision,'map':layout.MAP}
record=(shell_producer.create_build_record(root,'https://github.com/DeanoC/fes.git',revision,current['tools'],execution=current['execution']) if st else shell_producer.create_build_record(root,'https://github.com/DeanoC/fes.git',revision,current['tools'],current['execution'],**options))
base=LoadedRbf(die=SX120F,header=bytes(header_nbytes(SX120F)),cram=bytearray((SX120F.cram_sx*SX120F.cram_sy+7)//8),compressed=True)
base_bytes=rbf_save(base,compressed=True)
evidence={'build_id':build_identity(record),'rbf':{'sha256':sha(base_bytes),'size':len(base_bytes)}}
if st:
    mapping={'format':1,'device':'5CSEBA6U23I7','encoding':'m10k-1024x10-v1','base_sha256':sha(base_bytes),'source_size':1024,'blocks':[{'bel':'fixture','source_offset':0,'word_bits':[[32*7605+w*40+i for i in range(40)] for w in range(256)]}]}
    rom_map=enc(mapping)+b'\n'
    evidence['rom']={'id':'fes.atari-st.firmware','role':'firmware','source_size':1024,'file':'rom-map.json','size':len(rom_map),'sha256':sha(rom_map)}
manifest=(shell_producer._manifest(record,evidence,'https://github.com/DeanoC/fes.git',revision,current['tools']) if st else shell_producer.manifest(record,evidence,'https://github.com/DeanoC/fes.git',revision,current['tools'],**options))
package_id=package_identity(manifest,base_bytes,rom_map if st else None)
package=output/package_id; package.mkdir()
for name,value in [('manifest.toml',manifest),('core.rbf',base_bytes)]: (package/name).write_bytes(value)
shell=output/'shell'; shell.mkdir()
for name,value in [('manifest.toml',manifest),('core.rbf',base_bytes),('routed.json',b'{}'),('socket.qsf',b'fixture constraints'),('build-inputs.json',record)]: (shell/name).write_bytes(value)
if st:
    (package/'rom-map.json').write_bytes(rom_map)
    (shell/'rom-map.json').write_bytes(rom_map)
cases={}
variants=('direct','scanlines') if len(sys.argv)>4 and sys.argv[4]=='archives' else ('direct','scanlines','outside','header','inside-legacy-column','outside-legacy-column')
frames={}
for case in variants:
    profile='scanlines' if case=='scanlines' else 'direct'
    directory=output/case; directory.mkdir()
    placed=LoadedRbf(die=SX120F,header=base.header,cram=bytearray(base.cram),compressed=True)
    cram_set(placed.cram,SX120F,3488 if 'legacy-column' in case else 1800,(3441 if st else 1799) if case.startswith('outside') else (3443 if st else 1801),1)
    if case=='header': placed.header=bytes([1])+placed.header[1:]
    coordinate=(3488 if 'legacy-column' in case else 1800,(3441 if st else 1799) if case.startswith('outside') else (3443 if st else 1801),case=='header')
    if coordinate not in frames:
        frames[coordinate]=rbf_save(placed,compressed=True)
    cart=frames[coordinate]
    recipe={k:current[k] for k in ('inputs','source_roots','source_closure_policy','tools','execution')}
    recipe.update(shell={name:sha((shell/name).read_bytes()) for name in ('manifest.toml','core.rbf','routed.json','socket.qsf')+(('rom-map.json',) if st else ())},variant=profile,slot_clock=layout.CLOCK,map=layout.MAP,cram_region=list(layout.CRAM),required_clocks_mhz=clock_producer.REQUIRED_CLOCKS_MHZ,clock_constraints_sha256=sha(clock_producer.cart_clock_constraints(root)))
    if st: recipe['placer_seed']=producer.PLACER_SEED
    part_manifest={'cart_sha256':sha(cart),'cart_size':len(cart),'device':'5CSEBA6U23I7','format':1,'map':layout.MAP,'recipe_sha256':sha(enc(recipe)),'revision':revision,'shell_build_id':build_identity(record),'shell_package_id':package_id,'shell_sha256':sha(base_bytes),'slot':layout.INTERFACE,'slot_major':1,'slot_minor':0}
    encoded=enc(part_manifest); part_id=sha(b'fes-expansion-v1\0'+encoded)
    archive=directory/'part.tar'
    with tarfile.open(archive,'w',format=tarfile.USTAR_FORMAT) as tar:
        for name,value in [('manifest.json',encoded),('cart.rbf',cart)]:
            info=tarfile.TarInfo(name);info.size=len(value);tar.addfile(info,io.BytesIO(value))
    cells={} if profile=='direct' else {'state':{'type':'MISTRAL_FF','connections':{'CLK':[4]},'port_directions':{'CLK':'input'}}}
    if native:
        cells.update(clock_input={'type':'MISTRAL_IB','connections':{'PAD':[2],'O':[3]},'port_directions':{'PAD':'input','O':'output'}},clock_buffer={'type':'MISTRAL_CLKBUF','connections':{'A':[3],'Q':[4]},'port_directions':{'A':'input','Q':'output'}})
        cells.update({f'ram{i}':{'type':'MISTRAL_M10K','parameters':{'CFG_DUAL_CLOCK':1,'CFG_MIXED_WIDTH':1},'connections':{'CLK1':[4],'CLK2':[4]},'port_directions':{'CLK1':'input','CLK2':'input'}} for i in range(48)})
    synth={'modules':{'cart':{'cells':cells,'ports':{'FPGA_CLK1_50':{'direction':'input','bits':[2]}}}}}
    (directory/'cart.json').write_bytes(enc(synth))
    if native:
        (directory/'cart-synth.json').write_bytes(enc(synth))
        clock_boundary=native_video_clock.prepare_native_clock(directory/'cart.json')
    counts=build_fes_atari_st_oss._cell_counts(synth) if st else producer._cell_counts(synth)
    routed={'modules':{'top':{'netnames':{layout.CLOCK:{'bits':[42]}},'cells':{}}}}
    for name,cell in cells.items():
        if cell['type'] in ('MISTRAL_FF','MISTRAL_M10K'):
            routed['modules']['top']['cells']['fes_cart$'+name]={'type':cell['type'],'connections':{pin:[42] for pin in cell['connections']}}
    checked_clocks=producer.validate_clocks(routed,layout=layout)
    timing={'fmax':{name:{'constraint':freq,'achieved':freq+10} for name,freq in clock_producer.REQUIRED_CLOCKS_MHZ.items()},'utilization':{'MISTRAL_FF':{'used':counts.get('MISTRAL_FF',0),'available':167640}}}
    if native: timing['utilization']['MISTRAL_M10K']={'used':48,'available':397}
    changes=classify_cram_diff(base,placed,CramRect(*layout.CRAM),include_outside_coordinates=True,ignore_ecc_columns=not strict)
    summary={'recipe':recipe,'part_id':part_id,'manifest':part_manifest,'cram_diff':changes,'checked_clock_pins':checked_clocks,'timing':{name:(freq+10 if st else [name,freq,freq+10]) for name,freq in clock_producer.REQUIRED_CLOCKS_MHZ.items()},'resources':timing['utilization'],'synthesis_cells':counts,'route':{'complete':True,'gpu_backend':'hip'},'cram_policy':'strict-rectangle-v1' if strict else 'legacy-columns-v1','preview_matches_routed_cram':overlay_cram(base,placed,CramRect(*layout.CRAM)).cram==placed.cram}
    if native: summary['native_clock_boundary']=clock_boundary
    report={'archive_published':True,'cart_sha256':sha(cart),'cram_diff':changes,'cram_region':list(layout.CRAM),'map':layout.MAP,'part_id':part_id,'route_contract':'passed'}
    for name,value in [('cart-routed.json',routed),('timing.json',timing),('build-summary.json',summary),('cram-diff.json',report)]: (directory/name).write_bytes(enc(value))
    (directory/'route.log').write_text('Info: GPU router backend hip: fixture device ready\nInfo: Program finished normally.\n')
    cases[case]={'package':str(package),'shell':str(shell),'current':current,'profile':profile,'archive':str(archive),'directory':str(directory)}
(output/'cases.json').write_bytes(enc(cases))
'''


class RealProducerEvidenceTests(unittest.TestCase):
    """Exercise actual package, clock/resource and full-device CRAM readers."""
    lane = "raster"
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        cls.source = Path(__file__).resolve().parents[1] / "sources/misteross"
        subprocess.run([sys.executable, "-I", "-B", "-c", _FIXTURE, str(cls.source), str(cls.root), cls.lane], check=True)
        cls.cases = json.loads((cls.root / "cases.json").read_bytes())
        cls.recipe = video.recipes.recipe_for("fes.atari-st" if cls.lane == "st" else "fes.coleco")

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def inspect(self, case, **changes):
        args = copy.deepcopy(self.cases[case])
        args.update(changes)
        return video._inspect(self.source, "part", args, self.recipe, {})

    def test_direct_and_scanlines_real_frame_evidence(self):
        for case in ("direct", "scanlines"):
            with self.subTest(case=case):
                value = self.inspect(case)
                self.assertEqual(value["profile"], case)
                self.assertTrue(video.HEX64.fullmatch(value["part_id"]))

    def test_actual_outside_bit_and_header_rejected(self):
        with self.assertRaisesRegex(ValueError, "outside its CRAM"):
            self.inspect("outside")
        with self.assertRaisesRegex(ValueError, "configuration header"):
            self.inspect("header")

    def test_legacy_companion_column_policy_is_not_widened(self):
        self.inspect("outside-legacy-column")

    def test_profile_and_current_source_or_tool_mismatch_rejected(self):
        with self.assertRaisesRegex(ValueError, "current build recipe"):
            self.inspect("direct", profile="scanlines")
        for field, update in (("tools", {"yosys": "changed"}), ("inputs", {"changed.v": "f" * 64})):
            current = copy.deepcopy(self.cases["direct"]["current"])
            current[field] = update
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "current build recipe"):
                self.inspect("direct", current=current)

    def test_evidence_snapshot_cannot_change_before_validation(self):
        with self.assertRaisesRegex(ValueError, "evidence changed after snapshot"):
            self.inspect("direct", evidence_sha256={"build-summary.json": "0" * 64})

    def test_timing_clock_and_publication_evidence_tampering(self):
        case = self.cases["scanlines"]
        directory = Path(case["directory"])
        for name, mutate, expected in (
                ("timing.json", lambda data: data["fmax"]["pixel_clk"].update(achieved=10), "timing"),
                ("cart-routed.json", lambda data: data["modules"]["top"]["cells"]["fes_cart$state"]["connections"].update(CLK=[99]), "pixel clock"),
                ("cram-diff.json", lambda data: data.update(archive_published=False), "publication evidence")):
            path = directory / name
            before = path.read_bytes()
            try:
                changed = json.loads(before)
                mutate(changed)
                path.write_bytes(video.canonical(changed))
                with self.subTest(name=name), self.assertRaisesRegex(ValueError, expected):
                    self.inspect("scanlines")
            finally:
                path.write_bytes(before)


class NativeProducerEvidenceTests(RealProducerEvidenceTests):
    lane = "native"

    def test_legacy_companion_column_policy_is_not_widened(self):
        self.inspect("inside-legacy-column")
        with self.assertRaisesRegex(ValueError, "outside its CRAM"):
            self.inspect("outside-legacy-column")

    def test_original_synthesis_and_normalized_clock_proof_are_required(self):
        directory = Path(self.cases["direct"]["directory"])
        for name, mutate, expected in (
                ("cart-synth.json", lambda data: data["modules"]["cart"]["cells"]["ram0"]["parameters"].update(CFG_DUAL_CLOCK=0), "mixed-width SDP"),
                ("cart.json", lambda data: data["modules"]["cart"]["cells"]["ram0"]["parameters"].update(CFG_MIXED_WIDTH=0), "proven clock normalization"),
                ("build-summary.json", lambda data: data["native_clock_boundary"].update(synth_sha256="0" * 64), "clock normalization evidence"),
                ("build-summary.json", lambda data: data.update(cram_policy="legacy-columns-v1"), "containment policy"),
                ("build-summary.json", lambda data: data.update(preview_matches_routed_cram=False), "preview differs")):
            path = directory / name
            before = path.read_bytes()
            try:
                changed = json.loads(before)
                mutate(changed)
                path.write_bytes(video.canonical(changed))
                with self.subTest(name=name, expected=expected), self.assertRaisesRegex(ValueError, expected):
                    self.inspect("direct")
            finally:
                path.write_bytes(before)

    def test_synthesized_snapshot_cannot_change_before_validation(self):
        with self.assertRaisesRegex(ValueError, "evidence changed after snapshot"):
            self.inspect("direct", evidence_sha256={"cart-synth.json": "0" * 64})

    def test_native_inputs_cannot_be_validated_under_raster_profile(self):
        current = copy.deepcopy(self.cases["direct"]["current"])
        current["map"] = "fes.coleco-video.socket/1"
        with self.assertRaisesRegex(ValueError, "different shell profile"):
            self.inspect("direct", current=current)


class STProducerEvidenceTests(RealProducerEvidenceTests):
    lane = "st"

    def test_legacy_companion_column_policy_is_not_widened(self):
        with self.assertRaisesRegex(ValueError, "outside its CRAM"):
            self.inspect("outside-legacy-column")

    def test_raster_part_cannot_allocate_ram(self):
        directory = Path(self.cases["direct"]["directory"])
        prepared = directory / "cart.json"
        before = prepared.read_bytes()
        try:
            changed = json.loads(before)
            changed["modules"]["cart"]["cells"]["ram"] = {"type": "MISTRAL_M10K"}
            prepared.write_bytes(video.canonical(changed))
            with self.assertRaisesRegex(ValueError, "must not allocate RAM"):
                self.inspect("direct")
        finally:
            prepared.write_bytes(before)

    def test_firmware_map_and_seed_are_bound_to_exact_recipe(self):
        shell = Path(self.cases["direct"]["shell"])
        mapping = shell / "rom-map.json"
        before = mapping.read_bytes()
        try:
            mapping.write_bytes(before + b" ")
            with self.assertRaisesRegex(ValueError, "firmware map"):
                self.inspect("direct")
        finally:
            mapping.write_bytes(before)
        directory = Path(self.cases["direct"]["directory"])
        summary = directory / "build-summary.json"
        before = summary.read_bytes()
        try:
            changed = json.loads(before)
            changed["recipe"]["placer_seed"] = 5
            summary.write_bytes(video.canonical(changed))
            with self.assertRaisesRegex(ValueError, "producer summary"):
                self.inspect("direct")
        finally:
            summary.write_bytes(before)


class VideoShellAdmissionTests(unittest.TestCase):
    def descriptor(self, interface):
        return {"format": 2, "core": {"id": "fes.coleco"},
                "abi": {"id": "fes.application", "major": 1, "minor": 0},
                "interfaces": [{"id": interface, "major": 1, "minor": 0, "required": False}]}

    def test_each_exact_marker_selects_only_its_own_closed_profile(self):
        for interface, profile in video.VIDEO_INTERFACES.items():
            self.assertEqual(video.video_shell_profile(self.descriptor(interface)), profile)
        self.assertIsNone(video.video_shell_profile({"interfaces": []}))

    def test_duplicate_mixed_or_mutated_markers_are_rejected(self):
        for interface in video.VIDEO_INTERFACES:
            original = self.descriptor(interface)
            for key, value in (("major", 2), ("major", True), ("minor", 1),
                               ("minor", False), ("required", True), ("required", 0),
                               ("unknown", 1)):
                descriptor = copy.deepcopy(original)
                descriptor["interfaces"][0][key] = value
                with self.subTest(interface=interface, key=key, value=value), self.assertRaises(ValueError):
                    video.video_shell_profile(descriptor)
            for other in video.VIDEO_INTERFACES:
                descriptor = copy.deepcopy(original)
                descriptor["interfaces"] += self.descriptor(other)["interfaces"]
                with self.assertRaises(ValueError):
                    video.video_shell_profile(descriptor)

    def test_profile_requires_format2_coleco_application(self):
        original = self.descriptor("fes.fabric.video.native-pixels")
        for changed in ({"format": 3}, {"format": 2.0}, {"core": {"id": "fes.pong"}},
                        {"abi": {"id": "fes.application", "major": 2, "minor": 0}}):
            with self.assertRaises(ValueError):
                video.video_shell_profile(original | changed)


    def test_st_requires_firmware_computer_and_raster_marker(self):
        descriptor = self.descriptor("fes.fabric.video.raster-rgb888")
        descriptor.update(format=3, core={"id": "fes.atari-st"},
                          abi={"id": "fes.computer", "major": 1, "minor": 0},
                          rom={"role": "firmware"})
        descriptor["interfaces"].append({"id": "fes.expansion.atari-st-bus", "major": 1,
                                         "minor": 0, "required": False})
        self.assertEqual(video.video_shell_profile(descriptor), video.ST_MAP)
        for change in ({"format": 2}, {"rom": []}, {"rom": {"role": "cartridge"}},
                       {"abi": {"id": "fes.application", "major": 1, "minor": 0}},
                       {"interfaces": self.descriptor("fes.fabric.video.native-pixels")["interfaces"]},
                       {"interfaces": descriptor["interfaces"][:1]}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                video.video_shell_profile(descriptor | change)


class ResolverTransactionTests(unittest.TestCase):
    core_id = "fes.coleco"
    """Check cache/publication orchestration; real producer readers run above."""
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.addCleanup(self.temp.cleanup)
        self.addCleanup(lambda: self.unseal())
        self.source = self.root / "source"
        self.source.mkdir()
        self.package_id = "a" * 64
        self.package = self.root / self.package_id
        self.package.mkdir()
        self.resolved = {"directory": self.package, "inputs": {
            "selection": {"package_id": self.package_id},
            "source_selection": {"functional_inputs_sha256": "d" * 64}}}
        self.cache = self.root / "cache/core-video-parts"
        self.destination = self.root / "result"
        self.current = {"revision": "e" * 40, "tools": {"yosys": "fixture"}, "map": video.NATIVE_MAP}
        self.builder = patch.object(video, "_build_part", side_effect=self.build_part).start()
        self.addCleanup(patch.stopall)
        self.inspector = patch.object(video, "_inspect", side_effect=self.inspect).start()

    def unseal(self):
        for path in self.root.rglob("*"):
            if not path.is_symlink():
                path.chmod(0o755 if path.is_dir() else 0o644)

    def inspect(self, source, mode, args, recipe, env):
        if mode == "canonical":
            return copy.deepcopy(self.current)
        if mode == "shell":
            return {"package_id": self.package_id, "build_record_sha256": "1" * 64}
        archive = Path(args["archive"]).read_bytes()
        return {"profile": args["profile"], "part_id": ("b" if args["profile"] == "direct" else "c") * 64,
                "archive_sha256": hashlib.sha256(archive).hexdigest(), "archive_size": len(archive),
                "recipe_sha256": "2" * 64}

    def shell(self):
        shell = self.source / video.VIDEO_OUTPUTS[self.current["map"]]
        shell.mkdir(parents=True)
        for name in (video.ST_SHELL_MEMBERS if self.core_id == "fes.atari-st" else video.SHELL_MEMBERS):
            (shell / name).write_bytes(b"fixture evidence")

    def build_part(self, source, shell, package, profile, recipe, env):
        directory = self.source / "build/video-parts" / profile
        directory.mkdir(parents=True)
        for name in video.PART_MEMBERS:
            (directory / name).write_bytes(b"fixture evidence")
        if self.current["map"] == video.NATIVE_MAP:
            (directory / "cart-synth.json").write_bytes(b"fixture synthesis evidence")
        archive = directory / "part.tar"
        archive.write_bytes(profile.encode())
        return archive

    def resolve(self, destination=None):
        return video.resolve_video_parts(self.source, self.resolved, destination or self.destination,
                                         cache_root=self.cache, env={}, recipe=video.recipes.recipe_for(self.core_id))

    def test_missing_shell_is_explicit_and_does_not_build_or_publish(self):
        with self.assertRaises(video.MissingVideoShell):
            self.resolve()
        self.builder.assert_not_called()
        self.assertFalse(self.destination.exists())

    def test_sealed_companion_and_parts_are_reused_without_producer_invocation(self):
        self.shell()
        result = self.resolve()
        self.assertEqual(self.builder.call_count, 2)
        self.assertEqual(video.read_index(result["directory"]), result["inputs"]["index"])
        result2 = self.resolve(self.root / "second")
        self.assertEqual(self.builder.call_count, 2)
        self.assertEqual(result["inputs"], result2["inputs"])
        self.assertEqual(result["index_path"].read_bytes(), result2["index_path"].read_bytes())
        self.assertEqual(len(list(self.cache.glob("*/direct/cart-synth.json"))), 1)

    def test_raster_cache_keeps_its_original_member_set(self):
        self.current["map"] = "fes.coleco-video.socket/1"
        self.shell()
        result = self.resolve()
        self.resolve(self.root / "second")
        self.assertEqual(self.builder.call_count, 2)
        self.assertEqual(list(self.cache.glob("*/direct/cart-synth.json")), [])
        self.assertEqual(video.read_index(result["directory"]), result["inputs"]["index"])

    def test_missing_sealed_native_synthesis_evidence_is_not_rebuilt(self):
        self.shell()
        self.resolve()
        evidence = next(self.cache.glob("*/direct/cart-synth.json"))
        evidence.parent.chmod(0o755)
        evidence.unlink()
        evidence.parent.chmod(0o555)
        with self.assertRaisesRegex(ValueError, "unexpected members"):
            self.resolve(self.root / "second")
        self.assertEqual(self.builder.call_count, 2)
        self.assertFalse((self.root / "second").exists())

    def test_corrupt_cached_modes_fail_before_rebuilding(self):
        self.shell()
        self.resolve()
        archive = next(self.cache.glob("*/direct/archive.tar"))
        archive.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "required mode"):
            self.resolve(self.root / "second")
        self.assertEqual(self.builder.call_count, 2)
        self.assertFalse((self.root / "second").exists())

    def test_changed_final_inputs_leave_output_unpublished(self):
        self.shell()
        original = self.inspect
        calls = 0
        def changed(source, mode, args, recipe, env):
            nonlocal calls
            result = original(source, mode, args, recipe, env)
            if mode == "canonical":
                calls += 1
                if calls > 1:
                    result["tools"] = {"yosys": "changed"}
            return result
        self.inspector.side_effect = changed
        with self.assertRaisesRegex(ValueError, "inputs changed"):
            self.resolve()
        self.assertFalse(self.destination.exists())

    def test_second_profile_failure_does_not_publish_partial_index(self):
        self.shell()
        original = self.build_part
        def fail(source, shell, package, profile, recipe, env):
            if profile == "scanlines":
                raise ValueError("injected scanline failure")
            return original(source, shell, package, profile, recipe, env)
        self.builder.side_effect = fail
        with self.assertRaisesRegex(ValueError, "injected scanline"):
            self.resolve()
        self.assertFalse(self.destination.exists())
        self.assertEqual(len(list(self.cache.glob("*/direct/archive.tar"))), 1)
        self.assertEqual(len(list(self.cache.glob("*/scanlines"))), 0)


class STResolverTransactionTests(ResolverTransactionTests):
    core_id = "fes.atari-st"

    def setUp(self):
        super().setUp()
        self.current["map"] = video.ST_MAP

    def test_sealed_companion_and_parts_are_reused_without_producer_invocation(self):
        self.shell()
        first = self.resolve()
        second = self.resolve(self.root / "second")
        self.assertEqual(self.builder.call_count, 2)
        self.assertEqual(first["inputs"], second["inputs"])
        self.assertEqual(len(list(self.cache.parent.glob("core-video-shells/*/rom-map.json"))), 1)

    def test_missing_sealed_native_synthesis_evidence_is_not_rebuilt(self):
        self.shell()
        self.resolve()
        evidence = next(self.cache.parent.glob("core-video-shells/*/rom-map.json"))
        evidence.parent.chmod(0o755)
        evidence.unlink()
        evidence.parent.chmod(0o555)
        with self.assertRaisesRegex(ValueError, "unexpected members"):
            self.resolve(self.root / "second")
        self.assertEqual(self.builder.call_count, 2)


if __name__ == "__main__":
    unittest.main()
