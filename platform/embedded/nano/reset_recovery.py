"""One-use, read-only qualification of a full volatile recovery SOF reload.

The caller owns the top-kit claim, private server, UART capture window, exact
artifact admission, finite callback deadlines and verified host cleanup. This
helper never requests power, programs flash, writes SD, resumes native work or
reuses any old source. Posting a reset or successful programming is insufficient.
"""
import copy
import hashlib
import os
from pathlib import Path
import re
import sys
import threading

from jtag_programming import ProgramAttempt, _identity, _persist
from flash_trial import programmer_success
from recovery_reset_probe import checked_nonce, observe
from upload import UploadSession

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from experiment import require_experiment


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


class ResetRecoveryAttempt:
    def __init__(self, consumed, *, serial, source_commit, image, nonce, failed_wire,
                 expected_payload, loader_fit_sha256, loader_manifest_sha256,
                 transport, scan, program, capture, session_factory):
        if (serial != 'TRWU8CBL' or type(source_commit) is not str or
                re.fullmatch('[0-9a-f]{40}', source_commit) is None or
                type(image) is not dict or set(image) != {'path', 'sha256'} or
                any(type(v) is not str or re.fullmatch('[0-9a-f]{64}', v) is None
                    for v in (image['sha256'], loader_fit_sha256, loader_manifest_sha256)) or
                type(failed_wire) is not bytes or not 0 < len(failed_wire) <= 1 << 20 or
                type(expected_payload) is not bytes or not 0 < len(expected_payload) <= 16 << 20 or
                any(not callable(f) for f in (transport, scan, program, capture, session_factory))):
            raise ValueError('fixed top-kit source, image, prior wire and read-only loader inputs required')
        checked_nonce(nonce)
        p = Path(image['path'])
        if (not p.is_absolute() or p.suffix != '.sof' or '..' in p.parts or
                re.fullmatch(r'[A-Za-z0-9_./-]+', str(p)) is None or
                any(q.is_symlink() for q in (p, *p.parents))):
            raise ValueError('exact regular volatile SOF path required')
        self.path, self.image = Path(consumed), copy.deepcopy(image)
        self.serial, self.source, self.nonce = serial, source_commit, nonce
        self.failed_wire, self.payload = failed_wire, expected_payload
        self.loader_fit, self.loader_manifest = loader_fit_sha256, loader_manifest_sha256
        self.transport, self.scan, self.program = transport, scan, program
        self.capture, self.session_factory = capture, session_factory
        self.retired = False; self.lock = threading.Lock(); self.binding = None

    def _guard(self):
        current = _identity(self.transport())
        if (current['serial'] != self.serial or current['source_commit'] != self.source or
                self.binding is not None and current != self.binding):
            raise ValueError('reset/recovery ownership or transport changed')
        p = Path(self.image['path'])
        if (any(q.is_symlink() for q in (p, *p.parents)) or not p.is_file() or
                not 0 < p.stat().st_size <= 128 << 20):
            raise ValueError('reset/recovery SOF changed')
        with p.open('rb') as f:
            if hashlib.file_digest(f, 'sha256').hexdigest() != self.image['sha256']:
                raise ValueError('reset/recovery SOF identity changed')
        return current

    def run(self):
        require_experiment()
        if not self.lock.acquire(blocking=False):
            raise ValueError('reset/recovery attempt is active')
        session = None
        try:
            if self.retired:
                raise ValueError('reset/recovery attempt is retired')
            self.retired = True
            _persist(self.path, dict(format=1, source_commit=self.source, image=self.image,
                     nonce=self.nonce, previous_wire_sha256=sha(self.failed_wire),
                     state='consumed-before-recovery', automatic_retry=False, sd_write_authorized=False))
            self.binding = copy.deepcopy(self._guard())
            def programming(cable):
                self._guard()
                if cable != self.binding['cable']:
                    raise ValueError('recovery cable changed')
                # A full volatile image only. No flash/erase/verify-write command.
                return self.program(['-c', cable, '-m', 'jtag', '-o', 'p;' + self.image['path'] + '@1'], timeout=90)
            gate = ProgramAttempt(self.path.with_name(self.path.name + '.program'),
                                  self._guard, self.scan, programming)
            grant, result = gate.run('recovery', self.image['sha256'])
            programmer_success(result)
            self._guard()
            raw = self.capture(timeout=90, maximum=1 << 20)
            witness = observe(raw, nonce=self.nonce)
            if witness['boot_kind'] != 4:
                raise ValueError('volatile JTAG qualification requires the JTAG boot-kind witness')
            self._guard()
            guard = self._guard
            session = self.session_factory(owns=guard)
            if (type(session) is not UploadSession or session.owns is not guard or
                    sha(session.kit['fit']) != self.loader_fit or
                    sha(session.kit['manifest']) != self.loader_manifest):
                raise ValueError('actual new read-only upload session and pinned loader/card required')
            receipt = {'commands': [], 'sd_write_attempted': False}
            session.read_existing(self.payload, receipt, purpose='boot')
            self._guard()
            if (receipt.get('status') != 'verified' or receipt.get('read_only') is not True or
                    receipt.get('sd_write_attempted') is not False or
                    receipt.get('boot_files_unchanged') is not True or
                    receipt.get('recovery_manifest_unchanged') is not True):
                raise ValueError('protected recovery loader/readback incomplete')
            return dict(status='verified-read-only-recovery-bootstrap', source_commit=self.source,
                        image_sha256=self.image['sha256'], previous_wire_sha256=sha(self.failed_wire),
                        programming_grant=grant, reset_entry=witness, loader_readback=receipt,
                        recovery_loader_admitted=True, old_source_reuse_authorized=False,
                        ordinary_runtime_resume=False, sdm_reset_verified=False,
                        sd_write_authorized=False, cid_verified=False, power_cycle_requested=False,
                        transport_cleanup_verified=False, hardware_acceptance=False)
        finally:
            try:
                if session is not None and type(session) is UploadSession:
                    session.close()
            finally:
                self.lock.release()
