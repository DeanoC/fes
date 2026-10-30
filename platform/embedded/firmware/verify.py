#!/usr/bin/env python3
"""Compile and exercise the actual patched TF-A functions with the RTL endpoint."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import struct
import subprocess
import base64
import urllib.request
import uuid
import prepare_tfa

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
FPGA = ROOT / "sources/misteross"


def function(source, name):
    # Exact source blob is verified before extraction; these functions contain
    # no brace-bearing strings. Fail closed if the known definition is absent.
    match = re.search(r"(?m)^(?:static )?(?:int|bool|void|uint32_t) " + name + r"\(", source)
    if not match:
        raise ValueError("missing TF-A function " + name)
    start = source.index("{", match.start())
    depth = 1
    end = start + 1
    while depth:
        depth += (source[end] == "{") - (source[end] == "}")
        end += 1
    return source[match.start():end] + "\n"


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--reference", type=Path, help="exact TF-A file; otherwise fetch the pinned public Git blob")
    p.add_argument("--output", type=Path, default=ROOT / "out" / ("embedded-control-" + uuid.uuid4().hex))
    p.add_argument("--cross-cxx", type=Path, help="optional AArch64 freestanding compiler")
    p.add_argument("--verilator", default="verilator")
    a = p.parse_args()
    out = a.output.absolute()
    if ".." in out.parts or any(x.is_symlink() for x in (out, *out.parents)) or not out.is_relative_to(ROOT / "out") or out.exists():
        p.error("output must be a fresh private directory beneath out/")
    if a.reference is None:
        request = urllib.request.Request("https://api.github.com/repos/ARM-software/arm-trusted-firmware/git/blobs/" + prepare_tfa.REFERENCE_BLOB,
                                         headers={"User-Agent": "fes-embedded-control-check"})
        with urllib.request.urlopen(request, timeout=60) as response:
            reference = base64.b64decode(json.load(response)["content"], validate=False)
    else:
        reference = a.reference.read_bytes()
    source = prepare_tfa.contained_source(reference).decode()
    names = ["fes_contained_query", "intel_fpga_sdm_write_buffer", "intel_fpga_sdm_write_all", "intel_mailbox_fpga_config_isdone",
             "mark_last_buffer_xfer_completed", "intel_fpga_config_completed_write", "intel_fpga_config_start",
             "is_fpga_config_buffer_full", "is_address_in_ddr_range", "intel_fpga_config_write", "intel_smmu_hps_remapper_init"]
    out.mkdir(parents=True)
    (out / "reference.c").write_bytes(reference)
    subset = source[:source.index("*/")+2] + '\n#include "tfa_stubs.h"\n' + '''
#define FPGA_CONFIG_BUFFER_SIZE 4
typedef enum {NO_REQUEST,RECONFIGURATION,BITSTREAM_AUTH} config_type;
struct fpga_config_info {uint32_t addr; int size,size_written; uint32_t write_requested; int subblocks_sent,block_number;};
static config_type request_type;
static int current_block,current_buffer,read_block,max_blocks;
static uint32_t send_id,rcv_id,bytes_per_block,blocks_submitted,g_remapper_bypass;
static bool bridge_disable;
struct fpga_config_info fpga_config_buffers[4];
'''
    subset += "\n".join(function(source, name) for name in names)
    subset += '''
void fes_tfa_reset(void) {
 request_type=NO_REQUEST; current_block=current_buffer=read_block=max_blocks=0;
 send_id=rcv_id=bytes_per_block=blocks_submitted=g_remapper_bypass=0;bridge_disable=false;
 for(unsigned i=0;i<4;i++) fpga_config_buffers[i]=(struct fpga_config_info){0};
}
void fes_tfa_query(uint64_t result[4]){fes_contained_query(result);}
int fes_tfa_start(unsigned f){return intel_fpga_config_start(f);}
int fes_tfa_write(uint64_t a,uint64_t n){return intel_fpga_config_write(a,n);}
int fes_tfa_completed(uint32_t a[3]) {
 uint32_t n=0,id=0; a[0]=a[1]=a[2]=0;
 int status=intel_fpga_config_completed_write(a,&n,&id);
 return n ? 0 : status;
}
int fes_tfa_isdone(void){return intel_mailbox_fpga_config_isdone();}
uint64_t fes_tfa_translate(uint64_t a){intel_smmu_hps_remapper_init(&a);return a;}
'''
    (out / "tfa_subset.c").write_text(subset)
    def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
    files=[HERE/"control.hpp",HERE/"control.cpp",HERE/"tests/contained_path.hpp",HERE/"tests/tfa_stubs.h",HERE/"tests/endpoint_tb.cpp",Path(__file__).resolve(),
           HERE/"prepare_tfa.py",HERE/"generated/fes_embedded_control.hpp",FPGA/"cores/fes-common/rtl/fes_embedded_control.sv",
           FPGA/"cores/fes-common/generated/fes_embedded_control.vh"]
    sources = {str(f.relative_to(ROOT)):sha(f) for f in files}
    commands = []
    def run(command, label):
        command = [str(c) for c in command]
        commands.append(command)
        with (out / (label + ".log")).open("w") as log:
            subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True, cwd=ROOT)
    # Compile the original patched C bodies, not a reimplementation of their
    # START/WRITE/completion/status behaviour. Only platform calls are fakes.
    run(["cc", "-std=c11", "-Wall", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I"+str(HERE/"tests"),
         "-c",out/"tfa_subset.c","-o",out/"tfa_subset.o"],"firmware-functions")
    if a.cross_cxx:
        run([a.cross_cxx,"-std=c++14","-ffreestanding","-fno-exceptions","-fno-rtti","-mgeneral-regs-only","-Wall","-Wextra","-Werror",
             "-c",HERE/"control.cpp","-o",out/"control-aarch64.o"],"cross-control")
        obj = (out/"control-aarch64.o").read_bytes()
        if obj[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H",obj,18)[0] != 183:
            raise ValueError("cross compiler did not produce an AArch64 object")
    run([a.verilator,"--cc","--exe","--build","--top-module","fes_embedded_control","-Wall",
         "--Mdir",out/"obj","-I"+str(FPGA/"cores/fes-common/generated"),
         "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
         "-CFLAGS","-std=c++14 -O2 -fsanitize=undefined -fno-sanitize-recover=all -I"+str(HERE),"-LDFLAGS",str(out/"tfa_subset.o")+" -fsanitize=undefined",
         FPGA/"cores/fes-common/rtl/fes_embedded_control.sv",HERE/"control.cpp",HERE/"tests/endpoint_tb.cpp"],"verilator-build")
    run([out/"obj/Vfes_embedded_control"],"checks")
    if sources != {str(f.relative_to(ROOT)):sha(f) for f in files}:
        raise ValueError("sources changed while compiling/verifying endpoint")
    report={"status":"passed","classification":"actual RTL plus patched TF-A C functions with simulated platform operations; no hardware acceptance",
            "commands":commands,"source_sha256":sources,
            "reference_git_blob":prepare_tfa.REFERENCE_BLOB,"reference_sha256":hashlib.sha256(reference).hexdigest(),
            "patched_tfa_sha256":hashlib.sha256(source.encode()).hexdigest(),
            "cross_compiler_sha256":sha(a.cross_cxx.resolve()) if a.cross_cxx else None,
            "aarch64_object_sha256":sha(out/"control-aarch64.o") if a.cross_cxx else None,
            "not_run":[] if a.cross_cxx else ["AArch64 freestanding adapter compilation"],
            "checks":(out/"checks.log").read_text().splitlines()}
    (out/"result.json").write_text(json.dumps(report,indent=2)+"\n")
    print("PASS:",out)


if __name__ == "__main__":
    main()
