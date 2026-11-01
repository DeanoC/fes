#!/usr/bin/env python3
"""Run both actual C++ controllers against the combined ZX81/persona RTL."""
import argparse
import concurrent.futures
import importlib.util
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[5]
SCENARIOS = ("session", "wrong-build", "program-failure", "wrong-persona", "ambiguous-write", "missing-response", "late-reset")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


build = load(HERE.parent / "build.py", "de25_combined_builder")
helpers = load(HERE.parent.parent / "zx81-hps/client_proof.py", "de25_combined_proof_helpers")


def execute(args):
    selected = tuple(args.scenario or SCENARIOS)
    build.PROFILE = build.load_profile(HERE / "profile.py")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    frozen = build.capture()
    snapshot = output / "functional-source"
    for name, data in frozen["files"].items():
        dest = snapshot / name; dest.parent.mkdir(parents=True, exist_ok=True); dest.write_bytes(data)
    project = output / "hps/project"; project.mkdir(parents=True)
    local = project / "fes_link"; local.mkdir()
    for name in build.SOURCE_NAMES:
        dest = local / name.removeprefix("fpga/"); dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(frozen["files"]["sources/misteross/boards/de25nano/link-proof/" + name])
    parent, comp = build.PROFILE.captured_helpers(project)
    parent.prepare(local, project, frozen)
    mailbox = project / "fes_mailbox"; mailbox.mkdir()
    for name in comp.LOCAL:
        dest = mailbox / name; dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(frozen["files"][comp.BOARD + "zx81-hps/" + name])
    for name in comp.CORE_FILES:
        dest = mailbox / name
        dest.write_bytes(frozen["files"]["sources/misteross/cores/fes-zx81/rtl/" + name])
    for name in ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos"):
        (mailbox / (name + ".sv")).write_bytes(frozen["files"]["sources/misteross/cores/fes-common/rtl/z80/" + name + ".sv"])
    (mailbox / "fes_computer_mailbox.v").write_bytes(frozen["files"]["sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v"])
    (mailbox / "zx81_dpram_de25.v").write_bytes((local / "zx81_dpram.v").read_bytes())
    (mailbox / "zx81_tape_store_de25.v").write_bytes(comp.retarget_tape((mailbox / "zx81_tape_store.v").read_bytes(), (mailbox / "tape_store.v").read_bytes())[0])
    inputs = {"source_revision": frozen["revision"], "source_committed": frozen["committed"],
              "source_sha256": {name: build.sha(data) for name,data in frozen["files"].items()}}
    (output / "inputs.json").write_text(json.dumps(inputs,indent=2)+'\n')
    if not inputs["source_committed"] and not args.allow_working_copy:
        raise ValueError("commit captured production sources before producing evidence")
    snapshot = output / "functional-source"
    paths = list(HERE.glob("*.cpp")) + list(HERE.glob("*.hpp")) + [HERE / "sim_top.v", HERE / "proof.py"]
    runtime = ROOT / "sources/libmister-runtime"
    paths += list((runtime / "src/native").rglob("*.hpp"))
    paths += list((HERE.parent.parent / "zx81-hps/cosim").glob("*.hpp"))
    paths += [runtime / "include/libmister-runtime/runtime.h", runtime / "src/native/de25_computer.cpp", runtime / "src/native/de25_pr_control.cpp",
              HERE.parent.parent / "zx81-hps/cosim/wire.hpp", HERE.parent.parent / "zx81-hps/client_proof.py"]
    captured = {}
    for path in paths:
        relative = path.relative_to(ROOT).as_posix()
        data = path.read_bytes(); captured[relative] = data
        dest = snapshot / relative; dest.parent.mkdir(parents=True, exist_ok=True); dest.write_bytes(data)
    test_committed = all(subprocess.run(["git", "show", inputs["source_revision"] + ":" + n], cwd=ROOT, capture_output=True).stdout == data for n,data in captured.items())
    if not test_committed and not args.allow_working_copy: raise ValueError("commit selected test/runtime sources")
    logs = output / "logs"; logs.mkdir()
    local = project / "fes_link"; mailbox = project / "fes_mailbox"
    bench = snapshot / HERE.relative_to(ROOT)
    rtl = [local / n for n in ("shell.v", "personas.v", "zx81_ram_pack.v")]
    rtl += [mailbox / n for n in ("fes_de25_computer.v", "fes_computer_mailbox.v", "zx81_machine.sv", "zx81_machine_clock.v", "zx81_keyboard.v", "zx81_dpram_de25.v", "zx81_tape_store_de25.v", "video.v", "fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv")]
    obj = output / "obj"
    helpers.run([args.verilator,"--cc","--exe","--build","-j","2","--public-flat-rw","--x-initial","unique",
        "-O3","--top-module","fes_de25_combined_sim","-Wno-fatal", "-I"+str(mailbox),"-I"+str(mailbox/"generated"),"-I"+str(local),
        "-DFES_DE25_PR_BUILD_ID=128'h"+args.build_id,"-DFES_DE25_COMPUTER_BUILD_ID=128'h"+args.build_id,
        "-CFLAGS","-O3 -I"+str(bench),"-MAKEFLAGS","OPT_FAST=-O3","--Mdir",str(obj),*map(str,rtl),str(bench/"sim_top.v"),str(bench/"server.cpp")],logs/"rtl-build.log",cwd=project,timeout=300)
    sources = [snapshot / "sources/libmister-runtime/src/native" / n for n in ("de25_computer.cpp", "de25_pr_control.cpp")]
    include = snapshot / "sources/libmister-runtime"
    flags=["-std=c++14","-O2","-Wall","-Wextra","-Werror","-pthread","-I"+str(include/"src"),"-I"+str(include/"include")]
    native=output/"client-native"
    helpers.run([args.cxx,*flags,str(bench/"client.cpp"),*map(str,sources),"-o",str(native)],logs/"native-build.log",timeout=180)
    native_tools={str(Path(shutil.which(command) or command).resolve()): helpers.sha(Path(shutil.which(command) or command).resolve().read_bytes()) for command in (args.verilator,args.cxx)}
    clients={"native":[str(native)]}
    tools={"verilator":subprocess.check_output([args.verilator,"--version"],text=True).strip(),"cxx":subprocess.check_output([args.cxx,"--version"],text=True).strip()}
    if not args.native_only:
        if not all((args.builder_image,args.sdk_volume,args.sdk_cxx,args.qemu)):raise ValueError("AArch64 SDK image/volume/compiler and QEMU required")
        arm=output/"client-aarch64"
        container=["docker","run","--rm","--network=none","--user",f"{os.getuid()}:{os.getgid()}","--mount",f"type=volume,src={args.sdk_volume},dst=/build,readonly","--mount",f"type=bind,src={snapshot},dst=/src,readonly","--mount",f"type=bind,src={output},dst=/out",args.builder_image]
        sdk_files=[args.sdk_cxx,args.sdk_cxx+".br_real"]
        before=helpers.run([*container,"sha256sum",*sdk_files],logs/"sdk-before.txt")
        container_include=Path("/src/sources/libmister-runtime"); container_bench=Path("/src")/HERE.relative_to(ROOT)
        armflags=["-std=c++14","-O2","-Wall","-Wextra","-Werror","-pthread","-static","-I"+str(container_include/"src"),"-I"+str(container_include/"include")]
        helpers.run([*container,args.sdk_cxx,*armflags,str(container_bench/"client.cpp"),*[str(container_include/"src/native"/n.name) for n in sources],"-o","/out/client-aarch64"],logs/"aarch64-build.log",timeout=300)
        helpers.arm_elf(arm)
        qemu=output/"qemu-aarch64";shutil.copyfile(args.qemu,qemu);qemu.chmod(0o700)
        clients["aarch64-cortex-a76"]=[str(qemu),"-cpu","cortex-a76",str(arm)]
        tools.update(builder_image=args.builder_image,sdk_volume=args.sdk_volume,sdk_before=before.decode(),qemu_sha256=helpers.sha(qemu.read_bytes()),qemu_version=helpers.run([str(qemu),"--version"],logs/"qemu-version.txt").decode())
    def suite(architecture,command):
        rows=[]
        for scenario in selected:
            with tempfile.TemporaryDirectory(prefix="fes-zx81-pr-client-") as tmp:
                endpoint=Path(tmp)/"socket"
                with (logs/f"{architecture}-{scenario}-server.log").open("wb") as log:
                    server=subprocess.Popen([str(obj/"Vfes_de25_combined_sim"),str(endpoint),"+verilator+rand+reset+2","+verilator+seed+41"],cwd=project,stdout=log,stderr=subprocess.STDOUT)
                    try:
                        until=time.monotonic()+15
                        while not endpoint.exists():
                            if server.poll() is not None or time.monotonic()>until:raise ValueError("combined RTL server did not start")
                            time.sleep(.01)
                        result=json.loads(helpers.run([*command,str(endpoint),scenario,args.build_id,str(logs/f"{architecture}-saved.p")],logs/f"{architecture}-{scenario}-client.log",timeout=1800))
                        if result['scenario']!=scenario or result['result']!='passed':raise ValueError("wrong proof result")
                        if scenario=='session' and (result['callbacks']!=3 or result['roundtrip']['loads']!=2 or result['roundtrip']['runs']!=2 or not result['roundtrip']['reload_removed_edit'] or result['loaded_bytes']!=result['roundtrip']['saved_bytes'] or not 8<=result['interrupted_bytes']<result['loaded_bytes'] or result['ramtop_empty']!=0x4400 or result['ramtop_16k']!=0x8000):raise ValueError("incomplete real session evidence")
                        if scenario=='session':
                            helpers.check_roundtrip(result['roundtrip'],result['loaded_bytes'])
                            data=(logs/f"{architecture}-saved.p").read_bytes()
                            if len(data)!=result['loaded_bytes'] or data[0x403b-0x4009]!=0x40:raise ValueError('retained SAVE image differs')
                            result['saved_image_sha256']=helpers.sha(data)
                        if server.wait(timeout=10):raise ValueError("combined RTL server failed")
                        rows.append(dict(architecture=architecture,**result))
                    finally:
                        if server.poll() is None:
                            server.terminate()
                            try:server.wait(timeout=5)
                            except subprocess.TimeoutExpired:server.kill();server.wait()
        return rows
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(clients)) as pool:
        futures=[pool.submit(suite,a,c) for a,c in clients.items()]
        results=[r for future in futures for r in future.result()]
    for scenario in selected:
        outcomes=[{k:v for k,v in r.items() if k!='architecture'} for r in results if r['scenario']==scenario]
        if any(r!=outcomes[0] for r in outcomes):raise ValueError("native/AArch64 semantic results differ")
    for name,expected in inputs['source_sha256'].items():
        if helpers.sha((ROOT/name).read_bytes())!=expected:raise ValueError("production recipe/RTL source changed during proof")
    for name,expected in native_tools.items():
        if helpers.sha(Path(name).read_bytes())!=expected:raise ValueError("native tool changed during proof")
    for name,data in captured.items():
        if (ROOT/name).read_bytes()!=data:raise ValueError("test/runtime source changed during proof")
    if not args.native_only and helpers.run([*container,"sha256sum",*sdk_files],logs/"sdk-after.txt")!=before:raise ValueError("SDK changed")
    report={"status":"passed","kind":"actual-dual-client-combined-ZX81-PR-RTL","source_revision":inputs['source_revision'],"production_source_committed":inputs['source_committed'],"test_source_committed":test_committed,"compiler_source_sha256":inputs['source_sha256'],"test_runtime_source_sha256":{n:helpers.sha(d) for n,d in captured.items()},"results":results,"tools":dict(tools,native_tools_sha256=native_tools),"selected_build_id":args.build_id,"binaries_sha256":{p.name:helpers.sha(p.read_bytes()) for p in ([native,obj/"Vfes_de25_combined_sim"]+([] if args.native_only else [arm]))},"physical_PR":"selection-mux-model-only","memory":"behavioral","hardware":"not-run"}
    (output/'proof.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps({"status":"passed","cases":len(results)}))


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output',type=Path,required=True)
    p.add_argument('--native-only',action='store_true');p.add_argument('--allow-working-copy',action='store_true')
    p.add_argument('--scenario',action='append',choices=SCENARIOS)
    p.add_argument('--build-id',default='00112233445566778899aabbccddeeff')
    p.add_argument('--cxx',default='c++');p.add_argument('--verilator',default='verilator')
    for n in ('builder-image','sdk-volume','sdk-cxx'):p.add_argument('--'+n)
    p.add_argument('--qemu',type=Path)
    args=p.parse_args()
    if not re.fullmatch('[0-9a-f]{32}',args.build_id) or args.build_id=='0'*32:
        p.error('build ID must be nonzero lowercase 128-bit hex')
    execute(args)
if __name__=='__main__':main()
