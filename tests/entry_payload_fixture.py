"""Compressed metadata fixture only; the zero-filled ELF is not bootable."""
import hashlib
import importlib.util
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('fes_fixture_entry_payload', ROOT/'platform/embedded/entry/payload.py')
codec = importlib.util.module_from_spec(spec)
spec.loader.exec_module(codec)


def bind_payload(entry, receipt):
    payload = bytes(64)
    packed = codec.encode(payload)
    blob = bytes(64) + packed
    (entry/'entry.img').write_bytes(blob)
    (entry/'payload.elf').write_bytes(payload)
    base, capacity = codec.workspace(receipt['ram_start'], receipt['ram_end'])
    receipt.update(entry_image_size=len(blob), entry_image_sha256=hashlib.sha256(blob).hexdigest(),
                   payload_encoding=codec.ENCODING,payload_offset=64,payload_size=len(payload),
                   payload_sha256=hashlib.sha256(payload).hexdigest(),payload_stored_size=len(packed),
                   payload_stored_sha256=hashlib.sha256(packed).hexdigest(),decode_base=base,decode_capacity=capacity)
