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
