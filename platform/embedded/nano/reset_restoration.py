"""One explicitly selected boot-FIT restoration through a fresh recovery gate.

The transaction retains one new UploadSession from admission through all
preservation checks and the single write/readback. It never activates native
code, resets SDM, writes raw SD/flash or reuses a previous boot's session.
"""
from installer_boot_refresh import refresh_staged_boot
from installer_recovery import CATALOG_PURPOSES, _source_inputs
from reset_recovery import ResetRecoveryAttempt, sha


class ResetRestorationAttempt:
    def __init__(self, recovery, payloads, *, previous_boot, retained_records):
        if type(recovery) is not ResetRecoveryAttempt or type(previous_boot) is not bytes:
            raise ValueError('new recovery gate and independently expected previous boot required')
        expected, records = _source_inputs(payloads, CATALOG_PURPOSES, retained_records)
        if (not records or sha(previous_boot) != sha(recovery.payload) or
                previous_boot == expected['boot']):
            raise ValueError('distinct restoration target, previous boot and retained files required')
        self.recovery, self.payloads = recovery, dict(expected)
        self.previous, self.records = previous_boot, tuple(records.values())
        self.receipt = {}; self.retired = False

    def run(self):
        if self.retired:
            raise ValueError('restoration attempt is retired')
        self.retired = True
        with self.recovery._admitted_session(restoration=True) as (session, boot):
            refresh_staged_boot(session, self.payloads, self.receipt,
                                previous_boot=self.previous, retained_records=self.records)
            self.recovery._guard()
            return dict(status='verified-boot-restoration', recovery=boot,
                        restoration=self.receipt, sd_write_authorized=True,
                        flash_write_authorized=False, physical_reset_requested=False,
                        ordinary_runtime_resume=False, sdm_reset_verified=False,
                        power_loss_durability_verified=False, hardware_acceptance=False)
