"""Install the opt-in owned channel in a fresh export of the exact TF-A pin."""
import hashlib
import re
from pathlib import Path

import prepare_tfa
import nano_digest
import nano_mailbox
import nano_firmware_ram
from verify import function

HERE = Path(__file__).resolve().parent
SETUP = "plat/intel/soc/agilex5/bl31_plat_setup.c"
MAKEFILE = "plat/intel/soc/agilex5/platform.mk"
PSCI = "plat/intel/soc/common/socfpga_psci.c"
PSCI_FRONTEND = "lib/psci/psci_main.c"

# These objects implement the bounded shared-ROM ownership path. Keep the
# vendor's size policy for all other objects, including startup and drivers
# outside this explicitly selected profile.
ROM_HOT_OBJECTS = (
    "nano_cold", "nano_cold_platform", "nano_boot", "nano_bridges",
    "nano_usb_platform", "nano_usb_board", "nano_usb_composition", "nano_rom",
)


def rom_compile_policy(profile):
    if (profile is None or profile.get('hps_rom') is None or
            profile.get('usb_pio') is not True or profile.get('usb_shared') is not True):
        return None
    return {'mode': 'shared-rom-ownership-o2-v1', 'flags': '-O2',
            'objects': [name + '.o' for name in ROM_HOT_OBJECTS],
            'make_rules': ''.join('$(BUILD_PLAT)/bl31/' + name + '.o: override CFLAGS += -O2\n'
                                  for name in ROM_HOT_OBJECTS),
            'cache_state_changed': False, 'source_semantics_changed': False}


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
	    (smc_fid == FES_EMBEDDED_SD_CALL && x3 == FES_EMBEDDED_SD_COOKIE) ||
	    (smc_fid == FES_EMBEDDED_USB_CALL && x3 == FES_EMBEDDED_USB_COOKIE) ||
\t    (smc_fid == FES_EMBEDDED_TAPE_CALL && x3 == FES_EMBEDDED_TAPE_COOKIE) ||
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
    return [HERE / name for name in ("prepare_nano_owned.py", "nano_digest.py", "nano_mailbox.py", "nano_mailbox_observe.inc", "nano_mailbox_observe.h", "nano_mailbox_status.inc", "verify.py", "nano_bridges.c", "nano_bridges.h", "nano_boot.c", "nano_boot.h", "nano_bridge_sip.c", "nano_bridge_sip.h", "nano_bridge_platform.h", "nano_bridge_platform.inc", "nano_owned_sip.inc", "nano_computer_control.inc", "nano_window_control.inc", "nano_program_control.inc", "generated/fes_embedded_bridges.h", "nano_sd.h", "nano_sd.c", "generated/fes_embedded_sd.h", "nano_usb_service.c", "nano_usb_service.h", "nano_usb_platform.h", "nano_usb_platform.c", "nano_usb_board.c", "nano_usb_board.h", "nano_usb_boot.inc", "nano_usb_composition.c", "nano_usb_composition.h", "nano_cold.h", "generated/fes_embedded_usb.h", "nano_identity.h", "nano_identity.c", "nano_configuration.h", "nano_configuration.c", "nano_program.h", "nano_program.c", "nano_rom.h", "nano_rom.c", "nano_rom_platform.inc", "generated/fes_embedded_rom.h", "nano_smmu.h", "nano_smmu.c", "nano_fabric_stop.c", "nano_fabric_stop.h", "nano_probe_stop.c", "nano_probe_stop.h", "nano_ddr_firewall.c", "nano_ddr_firewall.h", "nano_ddr_read.c", "nano_ddr_read.h", "nano_hps_io.c", "nano_hps_io.h", "generated/fes_de25_ddr_probe.h", "generated/fes_embedded_control.h", "nano_tape.h", "nano_tape.c", "nano_tape_service.h", "nano_tape_service.c", "nano_tape_platform.inc", "generated/fes_embedded_tape.h", "generated/fes_computer.h", "generated/fes_de25_computer.h")] + [HERE/name for name in ('nano_cpu_mode.h','nano_mmu.h','nano_mmu.c','nano_mmu_platform.h','nano_mmu_platform.c','nano_mmu_memory.c','nano_mmu_memory.h','nano_mmu_transition.S','nano_mmu_transition.h','nano_firmware_mmu.py')]


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


def cold_setup_source(text):
    """Keep vendor setup order and establish its console before cold capture.

    This remains secure early setup, before BL33, stream-ID setup and bridge
    authority. The two markers only locate a returned copy; provider validation
    later decides whether the saved record and live ownership are acceptable.
    """
    declaration='void bl31_early_platform_setup2('
    console='\tconsole_set_scope(&console,\n\t\t(CONSOLE_FLAG_BOOT | CONSOLE_FLAG_RUNTIME | CONSOLE_FLAG_CRASH));\n'
    stream='\tsetup_smmu_stream_id();'
    if (text.count(declaration)!=1 or text.count(console)!=1 or text.count(stream)!=1 or
            text.index(console)>text.index(stream) or 'fes_nano_cold_capture' in text):
        raise ValueError('cold early capture context changed')
    text=text.replace(declaration,'extern void fes_nano_cold_capture(uintptr_t, uint64_t);\n'+declaration)
    trace='''
\tNOTICE("FES_NANO_EL3_CAPTURE phase=before pointer=%llx cookie=%llx\\n",
\t       (unsigned long long)arg2, (unsigned long long)arg3);
\tfes_nano_cold_capture(arg2, arg3);
\tNOTICE("FES_NANO_EL3_CAPTURE phase=after pointer=%llx cookie=%llx\\n",
\t       (unsigned long long)arg2, (unsigned long long)arg3);
'''
    return text.replace(console,console+trace)


def install(source, cold_profile=None, cold_header=None):
    rom_policy = rom_compile_policy(cold_profile)
    digest = nano_digest.install(source, cold_profile is not None and
                                cold_profile.get('hps_configuration') is not None)
    mailbox = nano_mailbox.install(source, cold_profile is not None and
                                  cold_profile.get('hps_configuration') is not None)
    service = source / prepare_tfa.SOURCE
    reset = source / prepare_tfa.RESET_SOURCE
    original, raw = service.read_bytes(), reset.read_bytes()
    service.write_bytes(service_source(original,cold_serial=cold_profile is not None))
    reset.write_bytes(reset_source(raw))
    include = source / "plat/intel/soc/common/include"
    for name in ("nano_bridges.h", "nano_bridge_sip.h", "nano_bridge_platform.h", "nano_boot.h", "nano_sd.h", "nano_usb_service.h", "nano_usb_platform.h", "nano_cold.h", "nano_identity.h", "nano_configuration.h", "nano_program.h", "nano_rom.h", "nano_rom_platform.inc", "nano_computer_control.inc", "nano_window_control.inc", "nano_program_control.inc", "nano_tape.h", "nano_tape_service.h", "nano_tape_platform.inc"):
        (include / name).write_bytes((HERE / name).read_bytes())
    (include / "generated").mkdir(exist_ok=True)
    (include / "generated/fes_embedded_bridges.h").write_bytes((HERE / "generated/fes_embedded_bridges.h").read_bytes())
    (include / "generated/fes_embedded_sd.h").write_bytes((HERE / "generated/fes_embedded_sd.h").read_bytes())
    (include / "generated/fes_embedded_usb.h").write_bytes((HERE / "generated/fes_embedded_usb.h").read_bytes())
    (include / "generated/fes_embedded_control.h").write_bytes((HERE / "generated/fes_embedded_control.h").read_bytes())
    (include / "generated/fes_embedded_rom.h").write_bytes((HERE / "generated/fes_embedded_rom.h").read_bytes())
    for name in ("fes_embedded_tape.h", "fes_computer.h", "fes_de25_computer.h"):
        (include / "generated" / name).write_bytes((HERE / "generated" / name).read_bytes())
    for name in ("nano_bridges.c", "nano_bridge_sip.c", "nano_boot.c", "nano_sd.c", "nano_usb_service.c", "nano_identity.c", "nano_configuration.c", "nano_program.c", "nano_rom.c", "nano_tape.c", "nano_tape_service.c"):
        (source / "plat/intel/soc/common" / name).write_bytes((HERE / name).read_bytes())
    mk = source / MAKEFILE
    mk.write_text(mk.read_text() + "\nBL31_SOURCES += plat/intel/soc/common/nano_bridges.c plat/intel/soc/common/nano_bridge_sip.c plat/intel/soc/common/nano_boot.c plat/intel/soc/common/nano_sd.c plat/intel/soc/common/nano_usb_service.c plat/intel/soc/common/nano_identity.c plat/intel/soc/common/nano_configuration.c plat/intel/soc/common/nano_program.c plat/intel/soc/common/nano_rom.c plat/intel/soc/common/nano_tape.c plat/intel/soc/common/nano_tape_service.c\n")
    setup = source / SETUP
    text = setup.read_text()
    marker = "\tsocfpga_delay_timer_init();\n"
    if text.count(marker) != 1: raise ValueError("owned boot observer context changed")
    setup.write_text('#include "nano_bridge_platform.h"\n' + text.replace(marker, marker + "\tfes_nano_bridge_platform_setup();\n"))
    if cold_profile is not None:
        if cold_header is None: raise ValueError("missing matched cold profile header")
        for name in ("nano_cold.h", "nano_cpu_mode.h", "nano_cold_mmio.inc", "nano_sd_platform.h", "nano_smmu.h", "nano_fabric_stop.h", "nano_probe_stop.h", "nano_ddr_firewall.h", "nano_ddr_read.h", "nano_hps_io.h", "generated/fes_de25_ddr_probe.h"):
            (include/name).write_bytes((HERE/name).read_bytes())
        (include/'fes_nano_cold_profile.h').write_text(cold_header)
        for name in ("nano_cold.c", "nano_cold_platform.c", "nano_sd_platform.c", "nano_smmu.c", "nano_fabric_stop.c", "nano_probe_stop.c", "nano_ddr_firewall.c", "nano_ddr_read.c", "nano_hps_io.c"):
            (source/'plat/intel/soc/common'/name).write_bytes((HERE/name).read_bytes())
        mk.write_text(mk.read_text()+"\nTF_CFLAGS += -DFES_NANO_COLD_SERIAL=1\nBL31_SOURCES += plat/intel/soc/common/nano_cold.c plat/intel/soc/common/nano_cold_platform.c plat/intel/soc/common/nano_sd_platform.c plat/intel/soc/common/nano_smmu.c plat/intel/soc/common/nano_fabric_stop.c plat/intel/soc/common/nano_probe_stop.c plat/intel/soc/common/nano_ddr_firewall.c plat/intel/soc/common/nano_ddr_read.c plat/intel/soc/common/nano_hps_io.c\n")
        if cold_profile.get('usb_pio'):
            for name in ('nano_usb_board.h','nano_usb_boot.inc','nano_usb_composition.h'):
                (include/name).write_bytes((HERE/name).read_bytes())
            for name in ('nano_usb_platform.c','nano_usb_board.c','nano_usb_composition.c'):
                (source/'plat/intel/soc/common'/name).write_bytes((HERE/name).read_bytes())
            mk.write_text(mk.read_text()+'\nBL31_SOURCES += plat/intel/soc/common/nano_usb_platform.c plat/intel/soc/common/nano_usb_board.c plat/intel/soc/common/nano_usb_composition.c\n')
        if digest is not None:
            mk.write_text(mk.read_text() + '\n' + nano_digest.FLAGS)
        if rom_policy is not None:
            mk.write_text(mk.read_text() + '\n' + rom_policy['make_rules'])
        setup.write_text(cold_setup_source(setup.read_text()))
    psci = source / PSCI
    psci.write_text(psci_source(psci.read_text(), cold_serial=cold_profile is not None))
    frontend = source / PSCI_FRONTEND
    if cold_profile is not None:
        frontend.write_text(psci_frontend_source(frontend.read_text()))
    ram_binding = nano_firmware_ram.install(source, cold_profile) if cold_profile is not None else None
    import nano_firmware_mmu
    mmu_binding=nano_firmware_mmu.install(source,cold_profile,ram_binding) if cold_profile is not None else None
    return {"service_sha256": hashlib.sha256(service.read_bytes()).hexdigest(), "reset_sha256": hashlib.sha256(reset.read_bytes()).hexdigest(),
            "setup_sha256": hashlib.sha256(setup.read_bytes()).hexdigest(), "makefile_sha256": hashlib.sha256(mk.read_bytes()).hexdigest(),
            "psci_sha256": hashlib.sha256(psci.read_bytes()).hexdigest(),
            "psci_frontend_sha256": hashlib.sha256(frontend.read_bytes()).hexdigest() if cold_profile is not None else None,
            "boot_proof_installed": cold_profile is not None,
            "configuration_digest": digest,
            "rom_compilation": rom_policy,
            "firmware_ram": ram_binding,
            "firmware_mmu": mmu_binding,
            "configuration_mailbox_observer": mailbox,
            "secondary_release_locked": cold_profile is not None,
            "expected_boot_state": "validated cold serial record or rejection; hardware unqualified" if cold_profile else "recognized owned service, NOT_READY"}
