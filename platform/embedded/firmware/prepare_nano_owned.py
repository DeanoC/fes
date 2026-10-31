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


def service_source(data):
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


def install(source):
    service = source / prepare_tfa.SOURCE
    reset = source / prepare_tfa.RESET_SOURCE
    original, raw = service.read_bytes(), reset.read_bytes()
    service.write_bytes(service_source(original))
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
    psci = source / PSCI
    text = psci.read_text()
    for name in ("socfpga_pwr_domain_on", "socfpga_pwr_domain_off", "socfpga_pwr_domain_suspend",
                 "socfpga_pwr_domain_on_finish", "socfpga_pwr_domain_suspend_finish", "socfpga_system_off",
                 "socfpga_system_reset", "socfpga_system_reset2"):
        text, count = re.subn(r"(\b" + name + r"\([^;]*?\)\s*\{)", r"\1\n\tfes_nano_bridge_platform_invalidate();", text, count=1)
        if count != 1: raise ValueError("owned PSCI context changed: " + name)
    psci.write_text('#include "nano_bridge_platform.h"\n' + text)
    return {"service_sha256": hashlib.sha256(service.read_bytes()).hexdigest(), "reset_sha256": hashlib.sha256(reset.read_bytes()).hexdigest(),
            "setup_sha256": hashlib.sha256(setup.read_bytes()).hexdigest(), "makefile_sha256": hashlib.sha256(mk.read_bytes()).hexdigest(),
            "psci_sha256": hashlib.sha256(psci.read_bytes()).hexdigest(),
            "boot_proof_installed": False, "expected_boot_state": "recognized owned service, NOT_READY"}
