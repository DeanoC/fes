#!/usr/bin/env python3
"""Verify a generated DE25 diagnostic through its actual Avalon RTL model."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import uuid

HERE = Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
import reader_memory
import computer_model
import rom_transport
import timing_sources
ROOT = HERE.parents[3]
TAMAGO = ROOT / "platform/embedded/tamago"
SPEC = importlib.util.spec_from_file_location("embedded_sim_producer", HERE.parent / "embedded_control/producer.py")
PRODUCER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PRODUCER)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def run(label, args, cwd, env, output, expect=0, marker=None):
    result = subprocess.run([str(x) for x in args], cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=180)
    (output / (label + ".log")).write_bytes(result.stdout)
    if (result.returncode == 0) != (expect == 0) or (marker and marker not in result.stdout.decode(errors="replace")):
        raise ValueError(f"{label} returned {result.returncode}; see {output / (label + '.log')}")
    return result.stdout.decode()


def check_archive(path, expected):
    import seal
    if seal.read_package(path) != expected:
        raise ValueError("sealed archive differs from package directory")


def inputs_for(project):
    board = project / "board"
    inputs_path = board / "hps/inputs.json"
    generation_path = board / "hps/generation.json"
    inputs = json.loads(inputs_path.read_text())
    generation = json.loads(generation_path.read_text())
    identity = inputs["embedded_control"]
    timing_sources.verify(board/'hps/project', json.loads((board/'hps/build.json').read_text()).get('obsolete_jtag_constraints'))
    memory=reader_memory.checked(inputs['reader_memory'])
    if identity.get('reader_memory')!=memory or identity['control_abi']!=reader_memory.control_abi(memory):
        raise ValueError('embedded reader memory or control ABI differs')
    snapshot = board / "functional-source"
    sources = inputs["functional_snapshot_sha256"]
    if not sources or any(sha(snapshot / name) != expected for name, expected in sources.items()):
        raise ValueError("functional source snapshot differs")
    fingerprint_fields = {"source_sha256": sources,
        "vendor_sha256": inputs["vendor_archive_sha256"], "system_hz": 100000000,
        "pixel_hz": 74250000,"reader_memory":memory}
    if 'computer_model' in inputs:
        fingerprint_fields.update(computer_model.parameters(inputs['computer_model']))
    fingerprint_fields.update(rom_transport.parameters(inputs))
    if "hps_initialization" in inputs["computer"]:
        fingerprint_fields["hps_initialization"] = inputs["computer"]["hps_initialization"]
    fingerprint = hashlib.sha256(json.dumps(fingerprint_fields, sort_keys=True).encode()).hexdigest()[:32]
    build_id = identity["build_id"]
    if identity.get("build_inputs_sha256"):
        sys.path.insert(0, str(HERE))
        import seal
        from scripts.export_core_package import verify_record_source_at_revision
        record = (board / "hps/build-inputs.json").read_bytes()
        fields = verify_record_source_at_revision(ROOT, record)
        if (seal.encode_build_record(fields) != record or sha(board / "hps/build-inputs.json") != identity["build_inputs_sha256"] or
                fields["parameters"].get("snapshot_sha256") != seal.snapshot_digest(sources) or
                fields["parameters"].get("vendor_sha256") != inputs["vendor_archive_sha256"] or
                fields["parameters"].get("board_revision") != identity["board_fes_revision"] or
                fields["parameters"].get("control_abi") != identity["control_abi"] or
                any(fields['parameters'].get(k)!=v for k,v in reader_memory.parameters(memory).items()) or
                ('computer_model' in inputs and fields['parameters'].get('computer_model') != inputs['computer_model']) or
                any(fields['parameters'].get(k)!=v for k,v in rom_transport.parameters(inputs).items()) or
                {k: fields['parameters'][k] for k in ('package_contract','programming_profile') if k in fields['parameters']} != seal.package_parameters(inputs) or
                fields['abi_definition'] != seal.definition_for(inputs) or
                fields["parameters"].get("device") != seal.DEVICE or
                fields["parameters"].get("parallel_processors") != inputs.get("compile_processors") or
                fields["parameters"].get("classification") != "de25-two-revision-diagnostic" or
                fields["revision"] != identity["overlay_fes_revision"] or
                seal.build_identity(record) != build_id or identity.get("board_fingerprint") != fingerprint):
            raise ValueError("canonical diagnostic record differs from captured inputs")
        if rom_transport.checked(inputs) and not set(rom_transport.CORE_INPUTS).issubset(fields['source_inputs']):
            raise ValueError('ROM functional refresh is missing from the canonical source closure')
        for name, expected in sources.items():
            revision = fields["revision"] if name in identity["source_sha256"] else identity["board_fes_revision"]
            blob = subprocess.run(["git", "-C", str(ROOT), "show", revision + ":" + name],
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True).stdout
            if hashlib.sha256(blob).hexdigest() != expected:
                raise ValueError("captured source differs from recorded Git revision: " + name)
        result = json.loads((board / "hps/seal-result.json").read_text())
        if not re.fullmatch(r"[0-9a-f]{64}", result["package_id"]):
            raise ValueError("invalid sealed package ID")
        package = seal.read_package(board / "hps/packages" / result["package_id"])
        manifest, rom_map = seal.package_metadata(inputs, package.payload_bytes, record, fields['tools'])
        if package.manifest_bytes != manifest or package.rom_map_bytes != rom_map:
            raise ValueError('diagnostic package metadata differs from its sealed ROM selection')
        check_archive(board / "hps/packages" / (result["package_id"] + ".fcore"), package)
        build = json.loads((board / "hps/build.json").read_text())
        if build["artifact"] != "project/output_files/golden_top.sof":
            raise ValueError("unexpected diagnostic SOF path")
        raw_sof = board / "hps" / build["artifact"]
        combined = board / "hps/diagnostic-with-vendor-spl.sof"
        payload = board / "hps/core.rbf"
        spl = board / "hps/project/software/u-boot/spl/u-boot-spl-dtb.hex"
        if (package.package_id != result["package_id"] or package.fields["build"]["id"] != build_id or
                package.fields["target"] != {"platform": seal.PLATFORM, "device": seal.DEVICE, "programming_profile": seal.profile_for(inputs)} or
                package.fields["abi"] != {"id": seal.abi_for(inputs), "major": 1, "minor": 0} or
                package.fields['interfaces'] != seal.interfaces_for(inputs) or
                package.fields["build"]["revision"] != fields["revision"] or
                package.fields["build"]["recipe_sha256"] != fields["recipe_sha256"] or
                result["build_id"] != build_id or result["rbf_size"] != len(package.payload_bytes) or
                payload.read_bytes() != package.payload_bytes or
                (board / "hps/packages" / (result["package_id"] + ".build-inputs.json")).read_bytes() != record or
                build["inputs_sha256"] != sha(board / "hps/inputs.json") or
                build.get("parallel_processors") != inputs.get("compile_processors") or
                build["sha256"] != sha(raw_sof) or result["raw_sof_sha256"] != sha(raw_sof) or
                result["combined_sof_sha256"] != sha(combined) or result["vendor_spl_sha256"] != sha(spl) or
                result["build_inputs_sha256"] != identity["build_inputs_sha256"] or
                result["rbf_sha256"] != package.fields["payload"]["sha256"]):
            raise ValueError("sealed package differs from the diagnostic build")
        if seal.boot_order(inputs) == "HPS FIRST":
            pair_path = board / "hps/hps-first-pair.json"
            raw = pair_path.read_bytes()
            pair = json.loads(raw)
            phase1 = board / "hps/hps-first.hps.rbf"
            core = board / "hps/hps-first.core.rbf"
            keys = {"format", "classification", "package_id", "build_id", "build_inputs_sha256", "hps_io_hash",
                    "pfg_identity", "quartus_version", "phase1_sha256", "phase1_size", "core_sha256", "core_size", "vendor_spl_sha256"}
            hashes = [seal.parse_pfg_info((board / f"hps/pfg-info-{name}.log").read_bytes()) for name in ("hps", "core")]
            hashes.append(seal.parse_assembler_hash((board / "hps/project/output_files/golden_top.asm.rpt").read_text()))
            if (set(pair) != keys or raw != (json.dumps(pair, sort_keys=True, separators=(",", ":")) + "\n").encode() or
                    pair["format"] != 1 or pair["classification"] != "unqualified-hps-first-diagnostic" or
                    fields["parameters"].get("hps_initialization") != "HPS FIRST" or
                    fields["parameters"].get("conversion") != "hps-first-split-rbf-v1" or
                    pair["package_id"] != package.package_id or pair["build_id"] != build_id or
                    pair["build_inputs_sha256"] != identity["build_inputs_sha256"] or
                    pair["phase1_sha256"] != sha(phase1) or pair["phase1_size"] != phase1.stat().st_size or
                    pair["core_sha256"] != sha(core) or pair["core_size"] != core.stat().st_size or
                    core.read_bytes() != package.payload_bytes or pair["vendor_spl_sha256"] != sha(spl) or
                    pair["pfg_identity"] != fields["tools"]["quartus-pfg"] or
                    pair["quartus_version"] != fields["tools"]["quartus-version"] or
                    any(h != pair["hps_io_hash"] for h in hashes) or
                    result.get("hps_first_pair_sha256") != hashlib.sha256(raw).hexdigest()):
                raise ValueError("HPS-first pair differs from package or reported hardware hashes")
    elif fingerprint != build_id:
        raise ValueError("board producer fingerprint does not bind the endpoint")
    if (build_id != inputs["computer"]["build_id"] or
            not re.fullmatch(r"[0-9a-f]{32}", build_id) or build_id == "0" * 32):
        raise ValueError("invalid or inconsistent build ID")
    local = board / "hps/project/fes_mailbox"
    mapped = {"fes_de25_embedded.sv": "sources/misteross/prototypes/de25_board_control/fes_de25_embedded.sv",
              "fes_de25_read_width.sv": "sources/misteross/prototypes/de25_board_control/fes_de25_read_width.sv",
              "fes_de25_embedded_hw.tcl": "sources/misteross/prototypes/de25_board_control/fes_de25_embedded_hw.tcl",
              "fes_embedded_control.sv": "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv",
              "generated/fes_embedded_control.vh": "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh"}
    computer_sources=('fes_de25_computer.v','video.v','generated/fes_de25_computer.vh','generated/fes_de25_computer.hpp')
    selected=["sources/misteross/prototypes/de25_board_control/"+name in sources for name in computer_sources]
    if any(selected) and not all(selected):
        raise ValueError('incomplete captured computer transport overlay')
    if all(selected):
        material=local.parent/'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_computer/fes_de25_computer_10/synth'
        for name in computer_sources:
            source="sources/misteross/prototypes/de25_board_control/"+name
            mapped[name]=source
            if inputs['computer']['source_sha256'].get(name)!=sources[source]:
                raise ValueError('computer transport differs from the functional source snapshot')
            if not name.endswith('.hpp') and sha(material/name)!=sources[source]:
                raise ValueError('generated computer transport differs from captured source')
    for installed, source in mapped.items():
        if sha(local / installed) != sources[source]:
            raise ValueError("project endpoint differs from source snapshot: " + installed)
    script = board / "hps/project/integrate-mailbox.tcl"
    if sha(script) != inputs["computer"]["integration_sha256"]:
        raise ValueError("project integration script differs")
    qsf = (board / "hps/project/golden_top.qsf").read_text()
    model_sources = {name: (snapshot / name).read_bytes() for name in sources if name.startswith(computer_model.ROM_PATH)}
    computer_model.verify(board/'hps/project', inputs, model_sources)
    rom_transport.verify(board/'hps/project',inputs,{name:(snapshot/name).read_bytes() for name in sources})
    if rom_transport.checked(inputs) and generation.get('rom_transport') != rom_transport.verify_generated(board/'hps/project',inputs):
        raise ValueError('generated ROM evidence differs')
    if re.findall(r'(?m)^set_global_assignment -name NUM_PARALLEL_PROCESSORS ([^\r\n]+)$', qsf) != [str(inputs.get('compile_processors'))]:
        raise ValueError('project compile processor selection differs')
    if qsf.count(f"FES_DE25_COMPUTER_BUILD_ID=128'h{build_id}") != 1:
        raise ValueError("project build-ID macro differs")
    if inputs["computer"].get("hps_initialization") == "HPS FIRST" and re.findall(
            r'(?m)^\s*set_global_assignment\s+-name\s+HPS_INITIALIZATION\s+"([^"\n]+)"\s*$', qsf) != ["HPS FIRST"]:
        raise ValueError("project HPS-first assignment differs")
    generated = generation["embedded_control"]
    if generated.get('reader_memory')!=memory:
        raise ValueError('generated reader memory selection differs')
    if (generated["maps"]["lw"]["base"] != identity["peripheral_offset"] or
        generated["maps"]["lw"]["span"] != identity["span"] or
        generated["maps"]["jtag"]["base"] != identity["jtag_bridge_base"] + identity["peripheral_offset"] or
        generated["maps"]["jtag"]["span"] != identity["span"]):
        raise ValueError("generated control maps differ")
    changed = dict(sources)
    changed[mapped["fes_de25_embedded.sv"]] = "f" * 64
    modified = hashlib.sha256(json.dumps(dict(fingerprint_fields, source_sha256=changed), sort_keys=True).encode()).hexdigest()[:32]
    if modified == fingerprint:
        raise ValueError("board fingerprint did not respond to endpoint source")
    return inputs, generation, local


def main():
    p = argparse.ArgumentParser(description=__doc__)
    mode = p.add_mutually_exclusive_group(required=True)
    mode.add_argument("--project", type=Path, help="output of build.py generate/build")
    mode.add_argument("--standalone", action="store_true", help="test the Avalon adapter without vendor tools")
    p.add_argument("--output", type=Path, default=ROOT / "out" / ("de25-board-control-check-" + uuid.uuid4().hex))
    args = p.parse_args()
    try:
        project = args.project.absolute() if args.project else None
        output = args.output.absolute()
        for path in (output, *([project] if project else [])):
            if ".." in path.parts or any(x.is_symlink() for x in (path, *path.parents)) or not path.is_relative_to(ROOT / "out"):
                raise ValueError("project and output must be private paths under this worktree's out/")
        if (project is not None and not project.is_dir()) or output.exists():
            raise ValueError("project must exist and output must be fresh")
        inputs, generation, local = inputs_for(project) if project else (None, None, None)
        if inputs and inputs['reader_memory']['backend']=='hps-ddr-window':
            return subprocess.run([sys.executable,str(HERE.parent/'embedded_control/verify_window.py'),
                '--project',str(project),'--output',str(output)],timeout=180).returncode
        if inputs and inputs['reader_memory']['backend']=='hps-ddr':
            return subprocess.run([sys.executable,str(HERE/'verify_ddr.py'),'--project',str(project),'--output',str(output)],timeout=180).returncode
        output.mkdir(parents=True)
        source = (HERE / "probe.cpp").read_bytes()
        (output / "probe.cpp").write_bytes(source)
        tcl_sources = {name: sha(HERE / name) for name in ("hardware.tcl", "sim_jtag.tcl")}
        tcl = shutil.which("tclsh")
        if tcl is None:
            raise ValueError("tclsh is required for the diagnostic-script check")
        if project:
            endpoint = local / "fes_embedded_control.sv"
            wrapper = local / "fes_de25_embedded.sv"
            includes = local / "generated"
            build_id, abi = inputs["embedded_control"]["build_id"], inputs["embedded_control"]["control_abi"]
        else:
            local = output / "simulation-source"
            includes = local / "generated"
            includes.mkdir(parents=True)
            endpoint = local / "fes_embedded_control.sv"
            wrapper = local / "fes_de25_embedded.sv"
            endpoint.write_bytes((ROOT / "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv").read_bytes())
            wrapper.write_bytes((HERE / "fes_de25_embedded.sv").read_bytes())
            (local/'fes_de25_read_width.sv').write_bytes((HERE/'fes_de25_read_width.sv').read_bytes())
            (includes / "fes_embedded_control.vh").write_bytes((ROOT / "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh").read_bytes())
            build_id, abi = "00112233445566778899aabbccddeeff", 0x10203040
        width_adapter=local/'fes_de25_read_width.sv'
        source_sha256 = {"endpoint": sha(endpoint), "wrapper": sha(wrapper), "read_width": sha(width_adapter), "definition": sha(includes / "fes_embedded_control.vh")}
        tools, paths = PRODUCER.tools_snapshot()
        obj = output / "obj"
        command = [paths["verilator"], "--cc", "--exe", "--build", "--top-module", "fes_de25_embedded",
                   "-Wall", "-Wno-UNUSEDSIGNAL", "--Mdir", obj, "-I" + str(includes),
                   "-DFES_DE25_COMPUTER_BUILD_ID=128'h" + build_id, "-CFLAGS", "-std=c++17 -O2",
                   endpoint, width_adapter, wrapper, output / "probe.cpp"]
        env = PRODUCER.build_environment(paths)
        run("build", command, ROOT, env, output)
        executable = obj / "Vfes_de25_embedded"
        observed = json.loads(run("model", [executable, build_id, str(abi)], ROOT, env, output))
        if observed != {"status": "passed", "build_id": build_id, "control_abi": abi,
                        "held_address": True, "partial_drain": True, "fault": True}:
            raise ValueError("model returned unexpected observations")
        wrong = ("0" if build_id[0] != "0" else "1") + build_id[1:]
        run("wrong-id", [executable, wrong, str(abi)], ROOT, env, output, expect=1, marker="live build identity mismatch")
        run("wrong-abi", [executable, build_id, str(abi + 1)], ROOT, env, output, expect=1, marker="control ABI mismatch")
        tcl_env = dict(os.environ, FES_MODEL=str(executable), FES_DIAGNOSTIC_SCRIPT=str(HERE / "hardware.tcl"),
                       FES_DE25_SOF="simulation.sof", FES_DE25_DEVICE="simulated-device",
                       FES_DE25_MASTER="simulated-master", FES_DE25_BASE="0x110400",
                       FES_DE25_BUILD_ID=wrong, FES_DE25_ABI=str(abi))
        run("jtag-wrong-id", [tcl, HERE / "sim_jtag.tcl"], ROOT, tcl_env, output, expect=1,
            marker="FES_IDENTITY_REJECTED writes=0")
        tcl_env["FES_DE25_BUILD_ID"] = build_id
        result = run("jtag-model", [tcl, HERE / "sim_jtag.tcl"], ROOT, tcl_env, output,
                     marker="FES_DIAGNOSTIC_PASS")
        if "FES_DRAIN_PASS" not in result or "FES_FAULT_PASS" not in result or "FES_MASTER_CLOSED" not in result:
            raise ValueError("JTAG diagnostic did not report all cases and closure")
        for unsupported in (0x10203041,0x10203042):
            rejected_env=dict(tcl_env,FES_DE25_ABI=str(unsupported))
            run('jtag-unsupported-'+hex(unsupported),[tcl,HERE/'sim_jtag.tcl'],ROOT,rejected_env,output,
                expect=1,marker='FES_PROFILE_REJECTED writes=0')
        native_env = dict(os.environ)
        for name in ("GOOS", "GOARCH", "GOARM", "GOAMD64", "GOARM64", "GOOSPKG", "GOFLAGS", "GOTOOLCHAIN",
                     "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "GNUMAKEFLAGS", "PYTHON"):
            native_env.pop(name, None)
        native_env["GOWORK"] = "off"
        go_root = subprocess.check_output(["go", "env", "GOROOT"], cwd=TAMAGO, env=native_env, text=True).strip()
        native_env.update({"GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off",
                           "FES_DE25_BOARD_CONTROL_PROBE": str(executable), "FES_DE25_BOARD_CONTROL_ID": build_id,
                           "FES_DE25_BOARD_CONTROL_ABI": str(abi)})
        go = Path(go_root) / "bin/go"
        run("native", [go, "test", "-race", "-count=1", "-timeout", "45s", "-v", "-run",
                       "^TestBoardAvalonIdentityBindsNativeControl$", "./nativefabric"], TAMAGO, native_env, output)
        if (HERE / "probe.cpp").read_bytes() != source or tcl_sources != {name: sha(HERE / name) for name in tcl_sources}:
            raise ValueError("diagnostic test source changed during verification")
        if project:
            inputs_for(project)
        elif source_sha256 != {"endpoint": sha(ROOT / "sources/misteross/cores/fes-common/rtl/fes_embedded_control.sv"),
                               "wrapper": sha(HERE / "fes_de25_embedded.sv"),
                               "read_width": sha(HERE / "fes_de25_read_width.sv"),
                               "definition": sha(ROOT / "sources/misteross/cores/fes-common/generated/fes_embedded_control.vh")}:
            raise ValueError("standalone sources changed during verification")
        run('ddr-model',[sys.executable,HERE/'verify_ddr.py','--output',output/'ddr'],ROOT,env,output,
            marker='PASS25 DDR reader cases and3 identity/window rejection cases')
        if not project:
            run('rom-model',[sys.executable,HERE/'verify_rom.py','--output',output/'rom'],ROOT,env,output,
                marker='PASS ROM stream RTL:')
        report = {"status": "passed", "classification": "generated DE25 diagnostic Avalon RTL and native adapter in host simulation" if project else "standalone Avalon RTL and native adapter in host simulation",
                  "board_revision": inputs["embedded_control"]["board_fes_revision"] if project else None,
                  "overlay_revision": inputs["embedded_control"]["overlay_fes_revision"] if project else None,
                  "overlay_source_state": inputs["embedded_control"]["overlay_source_state"] if project else "working-tree",
                  "build_id": build_id, "control_abi": abi, "observed": observed,
                  "generated_routes": generation["embedded_control"] if project else None, "tools": tools,
                  "go_sha256": sha(go.resolve()), "tcl_sha256": sha(Path(tcl).resolve()),
                  "probe_source_sha256": hashlib.sha256(source).hexdigest(), "tcl_source_sha256": tcl_sources,
                  "source_sha256": source_sha256, "model_sha256": sha(executable),
                  "ddr_model_result_sha256":sha(output/'ddr/result.json'),
                  "rom_model_result_sha256":sha(output/'rom/result.json') if not project else None,
                  "project_inputs_sha256": sha(project / "board/hps/inputs.json") if project else None,
                  "generation_sha256": sha(project / "board/hps/generation.json") if project else None,
                  "command": [str(x) for x in command],
                  "logs_sha256": {name: sha(output / (name + ".log")) for name in ("build", "model", "wrong-id", "wrong-abi", "jtag-wrong-id", "jtag-model", "native")},
                  "limitations": ["host simulation only; no physical CPU MMIO or DDR path",
                                  "sealed hashes do not authenticate the producer or prove the running boot" if inputs and inputs["embedded_control"].get("build_inputs_sha256") else "board source/vendor fingerprint is not a sealed FES package",
                                  "on-chip responder tests one reader; other FPGA masters are not contained"]}
        (output / "result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
        print("PASS:", output)
        return 0
    except (ValueError, OSError, KeyError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print("de25 board control verification:", error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
