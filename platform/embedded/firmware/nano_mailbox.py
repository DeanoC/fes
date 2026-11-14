"""Bind cold-only queue observations to the exact reviewed vendor transport."""
import hashlib
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOURCE = "plat/intel/soc/common/soc/socfpga_mailbox.c"
HEADER = "plat/intel/soc/common/include/socfpga_mailbox.h"
SOURCE_SHA256 = "e68ded43ea82299ccee284c971dcabeeab811de33358123c70b994c73ed74834"
HEADER_SHA256 = "a7a3e8dccc604fb6f9285572d5977bc00f36ba56417e1308cf0c8b16989db24f"


def checked(source, header):
    if (hashlib.sha256(source).hexdigest() != SOURCE_SHA256 or
            hashlib.sha256(header).hexdigest() != HEADER_SHA256):
        raise ValueError("Nano mailbox is not the exact reviewed transport/header")
    return source


def install(root, enabled):
    if enabled is not True:
        return None
    root = Path(root)
    original = checked((root / SOURCE).read_bytes(), (root / HEADER).read_bytes())
    observer = (HERE / "nano_mailbox_observe.inc").read_bytes()
    status = (HERE / "nano_mailbox_status.inc").read_bytes()
    observer_header = (HERE / "nano_mailbox_observe.h").read_bytes()
    generated = original + b"\n" + observer + b"\n" + status
    (root / HEADER).with_name("nano_mailbox_observe.h").write_bytes(observer_header)
    (root / SOURCE).write_bytes(generated)
    return {"mode": "pinned-mailbox-v3-retired-status-v1",
            "reference_sha256": SOURCE_SHA256, "header_sha256": HEADER_SHA256,
            "observer_sha256": hashlib.sha256(observer).hexdigest(),
            "observer_header_sha256": hashlib.sha256(observer_header).hexdigest(),
            "retired_status_sha256": hashlib.sha256(status).hexdigest(),
            "observer_commands_added": False, "retired_status_command": 9,
            "retired_status_poll_limit": 10000, "retired_status_fifo_word_limit": 64,
            "retired_status_argument_limit": 6, "mailbox_restarts_added": False,
            "generated_sha256": hashlib.sha256(generated).hexdigest(),
            "mailbox_commands_added": True, "mailbox_register_writes_added": True}


ADDRESS_MODE = "sctlr-m-secure-mailbox-v1"
ADDRESS_HELPER = """#include <arch_helpers.h>
/* BL31 starts with the MMU off. The selected EL3 table supplies one secure
 * Device/XN mailbox page once SCTLR.M is set; no mutable mode latch is used. */
static inline uintptr_t fes_nano_mailbox_base(void)
{
    return (read_sctlr_el3() & 1U) ? 0x90000UL : 0x10a30000UL;
}
#define MBOX_OFFSET (fes_nano_mailbox_base())"""


def secure_header(header):
    if hashlib.sha256(header).hexdigest() != HEADER_SHA256:
        raise ValueError("secure mailbox base requires the exact reviewed header")
    old = "#define MBOX_OFFSET\t\t\t\t\t0x10a30000"
    text = header.decode()
    if text.count(old) != 1:
        raise ValueError("Nano mailbox physical base context differs")
    generated = text.replace(old, ADDRESS_HELPER)
    if generated.replace(ADDRESS_HELPER, old).encode() != header:
        raise ValueError("secure mailbox header changed more than base selection")
    return generated.encode()


def select_secure_base(root, binding, mmu_binding):
    if mmu_binding is None:
        return binding
    import nano_firmware_mmu as mmu
    if (not isinstance(mmu_binding, dict) or mmu_binding.get('contract') != mmu.RUNTIME_CONTRACT or
            not isinstance(mmu_binding.get('sources'), dict) or not mmu_binding['sources'] or
            not isinstance(binding, dict) or binding.get('mode') != 'pinned-mailbox-v3-retired-status-v1' or
            binding.get('header_sha256') != HEADER_SHA256):
        raise ValueError("secure mailbox base requires the selected MMU/transport bindings")
    mmu.verify_installed(Path(root), mmu_binding)
    header = Path(root)/HEADER
    original = header.read_bytes()
    generated = secure_header(original)
    if hashlib.sha256((Path(root)/SOURCE).read_bytes()).hexdigest() != binding.get('generated_sha256'):
        raise ValueError("mailbox transport changed before secure base selection")
    header.write_bytes(generated)
    return dict(binding, reference_header_sha256=HEADER_SHA256,
                header_sha256=hashlib.sha256(generated).hexdigest(),
                address_selection=dict(mode=ADDRESS_MODE, physical_base=0x10a30000,
                    mmu_active_base=0x90000, register='SCTLR_EL3.M',
                    additional_mailbox_commands=0, additional_mailbox_register_reads=0,
                    additional_mailbox_register_writes=0))
