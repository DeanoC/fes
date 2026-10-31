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
    match = re.search(r"(?m)^(?:static )?(?:int|bool|void|uint32_t|uintptr_t) " + name + r"\(", source)
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
    p.add_argument("--reset-reference", type=Path, help="Terasic bridge source; defaults to soc/ beside the service")
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
    blob, profile = prepare_tfa.source_identity(reference)
    vendor = profile == "terasic-de25-v2.12"
    source = prepare_tfa.contained_source(reference).decode()
    reset_reference = reset_source = None
    if vendor:
        reset_path = a.reset_reference or a.reference.parent / "soc/socfpga_reset_manager.c"
        reset_reference = reset_path.read_bytes()
        reset_source = prepare_tfa.contained_terasic_reset(reset_reference).decode()
        try:
            prepare_tfa.contained_terasic_reset(reset_reference + b"\n")
        except ValueError:
            pass
        else:
            raise ValueError("modified Terasic reset source was accepted")
    elif a.reset_reference:
        p.error("reset source is only supported with the Terasic profile")
    try:
        prepare_tfa.contained_source(reference + b"\n")
    except ValueError:
        pass
    else:
        raise ValueError("modified TF-A reference was accepted")
    names = ["fes_contained_query", "intel_fpga_sdm_write_buffer", "intel_fpga_sdm_write_all", "intel_mailbox_fpga_config_isdone",
             "mark_last_buffer_xfer_completed", "intel_fpga_config_completed_write", "intel_fpga_config_start",
             "is_fpga_config_buffer_full", "is_address_in_ddr_range", "intel_fpga_config_write", "intel_smmu_hps_remapper_init"]
    if vendor:
        names.insert(1, "fes_contained_isdone_detail")
    out.mkdir(parents=True)
    (out / "reference.c").write_bytes(reference)
    subset = source[:source.index("*/")+2] + ('\n#define FES_TEST_TERASIC_TFA 1\n' if vendor else '\n') + '#include "tfa_stubs.h"\n' + '''
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
'''
    if vendor:
        subset += '''
int fes_tfa_isdone_detail(uint32_t *detail){
 uint32_t raw=0; int status=intel_mailbox_fpga_config_isdone(&raw);
 *detail=fes_contained_isdone_detail(status,raw);return status;
}
'''
    else:
        subset += '''
int fes_tfa_isdone_detail(uint32_t *detail){*detail=0;return intel_mailbox_fpga_config_isdone();}
'''
    subset += '''
int fes_tfa_isdone(void){uint32_t detail=0;int status=fes_tfa_isdone_detail(&detail);return detail ? INTEL_SIP_SMC_STATUS_ERROR : status;}
uint64_t fes_tfa_translate(uint64_t a){intel_smmu_hps_remapper_init(&a);return a;}
'''
    (out / "tfa_subset.c").write_text(subset)
    def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
    files=[HERE/"control.hpp",HERE/"control.cpp",HERE/"tests/contained_path.hpp",HERE/"tests/tfa_stubs.h",HERE/"tests/endpoint_tb.cpp",HERE/"tests/terasic_reset_test.h",Path(__file__).resolve(),
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
    if vendor:
        # Compile the actual original and patched bridge routines against the
        # same simulated polls. Original functions must reproduce the masked
        # timeout; patched functions must return each Agilex5 poll failure.
        for direction in ("enable", "disable"):
            for name, code, flags in (("original", reset_reference.decode(), ["-DFES_TEST_VENDOR_ORIGINAL"]),
                                      ("patched", reset_source, [])):
                label = direction + "-" + name
                test_source = out / ("reset-" + label + ".c")
                test_source.write_text('#include "terasic_reset_test.h"\n' + function(code, "socfpga_bridges_" + direction))
                executable = out / ("reset-" + label)
                if direction == "enable":
                    flags = [*flags, "-DFES_TEST_ENABLE"]
                run(["cc", "-std=c11", "-Wall", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all",
                     "-I" + str(HERE/"tests"), *flags, test_source, "-o", executable], "reset-build-" + label)
                run([executable], "reset-check-" + label)
        # Exercise the fork's real top-level SiP dispatcher: the FES query
        # must not be swallowed by either vendor range and nearby IDs must
        # continue to reach their original versioned handlers.
        dispatch = '''#include <stdint.h>
typedef uint64_t u_register_t;
typedef struct { uint64_t r[4]; unsigned route; } Reply;
#ifdef FES_TEST_NO_V3
#define SIP_SVC_V3 0
#else
#define SIP_SVC_V3 1
#endif
#define INTEL_SIP_SMC_CMD_MASK 0xffffU
#define INTEL_SIP_SMC_CMD_V2_RANGE_BEGIN 0x400U
#define INTEL_SIP_SMC_CMD_V2_RANGE_END 0x4ffU
#define INTEL_SIP_SMC_CMD_V3_RANGE_BEGIN 0xc8U
#define INTEL_SIP_SMC_CMD_V3_RANGE_END 0x1f4U
#define SMC_RET4(h,a,b,c,d) do { Reply *r=(h); r->r[0]=(a); r->r[1]=(b); r->r[2]=(c); r->r[3]=(d); return 0; } while (0)
#define MOCK(name,n) static uintptr_t name(uint32_t f,u_register_t a,u_register_t b,u_register_t c,u_register_t d,void *cookie,void *handle,u_register_t flags) { (void)f;(void)a;(void)b;(void)c;(void)d;(void)cookie;(void)flags; ((Reply*)handle)->route=(n); return (n); }
MOCK(sip_smc_handler_v1,1)
MOCK(sip_smc_handler_v2,2)
#if SIP_SVC_V3
MOCK(sip_smc_handler_v3,3)
#endif
''' + function(source, "fes_contained_query") + function(source, "sip_smc_handler") + '''
int main(void) {
 Reply r={{0},0};
 if(sip_smc_handler(0xc2000300U,0,0,0,0,0,&r,0)!=0 || r.route || r.r[0] || r.r[1]!=0x46455343U || r.r[2]!=0x10000U || r.r[3]!=0xfU) return 1;
 if(sip_smc_handler(0x42000300U,0,0,0,0,0,&r,0)!=1 || r.route!=1) return 2;
 if(sip_smc_handler(0xc2000401U,0,0,0,0,0,&r,0)!=2 || r.route!=2) return 3;
#if SIP_SVC_V3
 if(sip_smc_handler(0xc20000c8U,0,0,0,0,0,&r,0)!=3 || r.route!=3) return 4;
#else
 if(sip_smc_handler(0xc20000c8U,0,0,0,0,0,&r,0)!=1 || r.route!=1) return 4;
#endif
 return 0;
}
'''
        (out / "dispatch.c").write_text(dispatch)
        for name, flags in (("v3", []), ("v1-v2", ["-DFES_TEST_NO_V3"])):
            executable = out / ("dispatch-" + name)
            run(["cc", "-std=c11", "-Wall", "-Werror", *flags, out/"dispatch.c", "-o", executable], "dispatch-build-" + name)
            run([executable], "dispatch-check-" + name)
    if a.cross_cxx:
        run([a.cross_cxx,"-std=c++14","-ffreestanding","-fno-exceptions","-fno-rtti","-mgeneral-regs-only","-Wall","-Wextra","-Werror",
             "-c",HERE/"control.cpp","-o",out/"control-aarch64.o"],"cross-control")
        obj = (out/"control-aarch64.o").read_bytes()
        if obj[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H",obj,18)[0] != 183:
            raise ValueError("cross compiler did not produce an AArch64 object")
    run([a.verilator,"--cc","--exe","--build","--top-module","fes_embedded_control","-Wall",
         "--Mdir",out/"obj","-I"+str(FPGA/"cores/fes-common/generated"),
         "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
         "-CFLAGS","-std=c++14 -O2 -fsanitize=undefined -fno-sanitize-recover=all " + ("-DFES_TEST_TERASIC_TFA " if vendor else "") + "-I"+str(HERE),"-LDFLAGS",str(out/"tfa_subset.o")+" -fsanitize=undefined",
         FPGA/"cores/fes-common/rtl/fes_embedded_control.sv",HERE/"control.cpp",HERE/"tests/endpoint_tb.cpp"],"verilator-build")
    run([out/"obj/Vfes_embedded_control"],"checks")
    if sources != {str(f.relative_to(ROOT)):sha(f) for f in files}:
        raise ValueError("sources changed while compiling/verifying endpoint")
    report={"status":"passed","classification":"actual RTL plus patched TF-A C functions with simulated platform operations; no hardware acceptance",
            "commands":commands,"source_sha256":sources,
            "reference_git_blob":blob,"reference_profile":profile,"reference_sha256":hashlib.sha256(reference).hexdigest(),
            "patched_tfa_sha256":hashlib.sha256(source.encode()).hexdigest(),
            "reset_reference_git_blob":prepare_tfa.TERASIC_RESET_BLOB if vendor else None,
            "reset_reference_sha256":hashlib.sha256(reset_reference).hexdigest() if vendor else None,
            "reset_patched_sha256":hashlib.sha256(reset_source.encode()).hexdigest() if vendor else None,
            "cross_compiler_sha256":sha(a.cross_cxx.resolve()) if a.cross_cxx else None,
            "aarch64_object_sha256":sha(out/"control-aarch64.o") if a.cross_cxx else None,
            "not_run":[] if a.cross_cxx else ["AArch64 freestanding adapter compilation"],
            "checks":(out/"checks.log").read_text().splitlines() + (["PASS terasic-query-dispatch-v1-v2-and-v3", "PASS terasic-original-timeout-reproduction-and-patched-enable-disable-polls"] if vendor else [])}
    (out/"result.json").write_text(json.dumps(report,indent=2)+"\n")
    print("PASS:",out)


if __name__ == "__main__":
    main()
