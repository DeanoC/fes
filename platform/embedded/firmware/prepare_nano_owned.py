"""Install the opt-in owned channel in a fresh export of the exact TF-A pin."""
import hashlib
import re
from pathlib import Path

import prepare_tfa
from verify import function

HERE = Path(__file__).resolve().parent
SETUP = "plat/intel/soc/agilex5/bl31_plat_setup.c"
MAKEFILE = "plat/intel/soc/agilex5/platform.mk"
PSCI = "plat/intel/soc/common/socfpga_psci.c"
PSCI_FRONTEND = "lib/psci/psci_main.c"


def service_source(data, cold_serial=False):
    blob, _ = prepare_tfa.source_identity(data)
    if blob != prepare_tfa.TERASIC_BLOB:
        raise ValueError("owned Nano service requires the exact Terasic source")
    text = prepare_tfa.contained_source(data).decode()
    anchor = "uintptr_t sip_smc_handler("
    if text.count(anchor) != 1: raise ValueError("owned SiP entry context changed")
    text = text.replace(anchor, (HERE / "nano_owned_sip.inc").read_text() + "\n" + anchor)
    marker = "\tuint32_t cmd = smc_fid & INTEL_SIP_SMC_CMD_MASK;\n"
    guard = '''
	if ((smc_fid == FES_EMBEDDED_BRIDGE_CALL && x3 == FES_EMBEDDED_BRIDGE_COOKIE) ||
	    smc_fid == INTEL_SIP_SMC_FPGA_CONFIG_START || smc_fid == INTEL_SIP_SMC_FPGA_CONFIG_WRITE ||
	    smc_fid == INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE || smc_fid == INTEL_SIP_SMC_FPGA_CONFIG_ISDONE) {
		uint64_t result[4];
		fes_nano_owned_call(smc_fid, x1, x2, x3, x4, result);
		SMC_RET4(handle, result[0], result[1], result[2], result[3]);
	}
	if (smc_fid != 0xc2000300U) {
		SMC_RET1(handle, INTEL_SIP_SMC_STATUS_REJECTED);
	}
'''
    if text.count(marker) != 1: raise ValueError("owned SiP range context changed")
    text = text.replace(marker, marker + guard)
    old = function(text, "intel_hps_set_bridges")
    text = text.replace(old, "uint32_t intel_hps_set_bridges(uint64_t enable, uint64_t mask)\n{\n\t(void)enable;\n\t(void)mask;\n\treturn INTEL_SIP_SMC_STATUS_REJECTED;\n}\n")
    if cold_serial:
        old=function(text,'fes_contained_query')
        new,count=re.subn(r'(?m)^\tresult\[3\] = 0x[0-9a-f]+U;', '\tresult[3] = 0U;',old)
        if count!=1:raise ValueError('cold capability query context changed')
        text=text.replace(old,new)
    return text.encode()


def reset_source(data):
    text = prepare_tfa.nano_bridge_driver_source(data).decode()
    for name in ("enable", "disable"):
        marker = "int socfpga_bridges_" + name + "(uint32_t mask)\n{"
        if text.count(marker) != 1: raise ValueError("owned raw bridge context changed")
        text = text.replace(marker, "static int fes_nano_raw_" + name + "(uint32_t mask)\n{")
    old = function(text, "socfpga_bridge_nongraceful_disable")
    text = text.replace(old, "")
    return (text + "\n" + (HERE / "nano_bridge_platform.inc").read_text()).encode()


def inputs():
    return [HERE / name for name in ("prepare_nano_owned.py", "verify.py", "nano_bridges.c", "nano_bridges.h", "nano_boot.c", "nano_boot.h", "nano_bridge_sip.c", "nano_bridge_sip.h", "nano_bridge_platform.h", "nano_bridge_platform.inc", "nano_owned_sip.inc", "generated/fes_embedded_bridges.h")]


def psci_source(text, cold_serial=False):
    names = ("socfpga_pwr_domain_on", "socfpga_pwr_domain_off", "socfpga_pwr_domain_suspend",
             "socfpga_pwr_domain_on_finish", "socfpga_pwr_domain_suspend_finish", "socfpga_system_off",
             "socfpga_system_reset", "socfpga_system_reset2")
    if cold_serial:
        old = function(text, "socfpga_pwr_domain_on")
        # Denial precedes every reset/vector/power write and does not retire a
        # still-valid owner. This profile never grants a secondary CPU lifecycle.
        text = text.replace(old, """int socfpga_pwr_domain_on(u_register_t mpidr)
{
\t(void)mpidr;
\treturn PSCI_E_INTERN_FAIL;
}
""")
        names = names[1:]
    for name in names:
        text, count = re.subn(r"(\b" + name + r"\([^;]*?\)\s*\{)", r"\1\n\tfes_nano_bridge_platform_invalidate();", text, count=1)
        if count != 1: raise ValueError("owned PSCI context changed: " + name)
    return '#include "nano_bridge_platform.h"\n' + text


def psci_frontend_source(text):
    old = function(text, "psci_cpu_on")
    # The platform callback only permits SUCCESS/INTERN_FAIL, after PSCI has
    # already set ON_PENDING. Deny here before validation, locks, bookkeeping
    # or hardware. Both 32-bit and 64-bit CPU_ON dispatch through this function.
    text = text.replace(old, """int psci_cpu_on(u_register_t target_cpu, uintptr_t entrypoint, u_register_t context_id)
{
\t(void)target_cpu;
\t(void)entrypoint;
\t(void)context_id;
\treturn PSCI_E_DENIED;
}
int fes_nano_cold_cpu_release_locked(void)
{
\treturn 1;
}
""")
    return '#include "nano_cold.h"\n' + text


def install(source, cold_profile=None, cold_header=None):
    service = source / prepare_tfa.SOURCE
    reset = source / prepare_tfa.RESET_SOURCE
    original, raw = service.read_bytes(), reset.read_bytes()
    service.write_bytes(service_source(original,cold_serial=cold_profile is not None))
    reset.write_bytes(reset_source(raw))
    include = source / "plat/intel/soc/common/include"
    for name in ("nano_bridges.h", "nano_bridge_sip.h", "nano_bridge_platform.h", "nano_boot.h"):
        (include / name).write_bytes((HERE / name).read_bytes())
    (include / "generated").mkdir(exist_ok=True)
    (include / "generated/fes_embedded_bridges.h").write_bytes((HERE / "generated/fes_embedded_bridges.h").read_bytes())
    for name in ("nano_bridges.c", "nano_bridge_sip.c", "nano_boot.c"):
        (source / "plat/intel/soc/common" / name).write_bytes((HERE / name).read_bytes())
    mk = source / MAKEFILE
    mk.write_text(mk.read_text() + "\nBL31_SOURCES += plat/intel/soc/common/nano_bridges.c plat/intel/soc/common/nano_bridge_sip.c plat/intel/soc/common/nano_boot.c\n")
    setup = source / SETUP
    text = setup.read_text()
    marker = "\tsocfpga_delay_timer_init();\n"
    if text.count(marker) != 1: raise ValueError("owned boot observer context changed")
    setup.write_text('#include "nano_bridge_platform.h"\n' + text.replace(marker, marker + "\tfes_nano_bridge_platform_setup();\n"))
    if cold_profile is not None:
        if cold_header is None: raise ValueError("missing matched cold profile header")
        for name in ("nano_cold.h", "nano_cold_mmio.inc"):
            (include/name).write_bytes((HERE/name).read_bytes())
        (include/'fes_nano_cold_profile.h').write_text(cold_header)
        for name in ("nano_cold.c", "nano_cold_platform.c"):
            (source/'plat/intel/soc/common'/name).write_bytes((HERE/name).read_bytes())
        mk.write_text(mk.read_text()+"\nTF_CFLAGS += -DFES_NANO_COLD_SERIAL=1\nBL31_SOURCES += plat/intel/soc/common/nano_cold.c plat/intel/soc/common/nano_cold_platform.c\n")
        text=setup.read_text()
        marker='\tstatic console_t console;\n'
        if text.count(marker)!=1: raise ValueError('cold early capture context changed')
        text=text.replace('void bl31_early_platform_setup2(', 'extern void fes_nano_cold_capture(uintptr_t, uint64_t);\nvoid bl31_early_platform_setup2(')
        setup.write_text(text.replace(marker,marker+'\tfes_nano_cold_capture(arg2, arg3);\n'))
    psci = source / PSCI
    psci.write_text(psci_source(psci.read_text(), cold_serial=cold_profile is not None))
    frontend = source / PSCI_FRONTEND
    if cold_profile is not None:
        frontend.write_text(psci_frontend_source(frontend.read_text()))
    return {"service_sha256": hashlib.sha256(service.read_bytes()).hexdigest(), "reset_sha256": hashlib.sha256(reset.read_bytes()).hexdigest(),
            "setup_sha256": hashlib.sha256(setup.read_bytes()).hexdigest(), "makefile_sha256": hashlib.sha256(mk.read_bytes()).hexdigest(),
            "psci_sha256": hashlib.sha256(psci.read_bytes()).hexdigest(),
            "psci_frontend_sha256": hashlib.sha256(frontend.read_bytes()).hexdigest() if cold_profile is not None else None,
            "boot_proof_installed": cold_profile is not None,
            "secondary_release_locked": cold_profile is not None,
            "expected_boot_state": "validated cold serial record or rejection; hardware unqualified" if cold_profile else "recognized owned service, NOT_READY"}
