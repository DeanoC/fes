"""Record the native program boundary inside an existing one-use admission.

The admission callback retains image/claim/transport/scan checks and its durable
reservation. This wrapper adds no device access, programming grant or recovery
authority. Missing-worker and incomplete-operation observations stay uncertain.
"""
import copy
import json
import os
from pathlib import Path
import re
import threading


def _persist(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


class NativeProgramDispatch:
    def __init__(self, marker, *, image_sha256, transport_epoch, owns):
        if (not isinstance(image_sha256, str) or
                re.fullmatch('[0-9a-f]{64}', image_sha256) is None or
                not isinstance(transport_epoch, str) or
                re.fullmatch('[1-9][0-9]*:[0-9]+', transport_epoch) is None or not callable(owns)):
            raise ValueError('explicit native image, transport epoch and ownership callback required')
        self.marker = Path(marker)
        self.owns = owns
        self._binding = dict(format=1, scope='nano-native-program-dispatch-v1',
                            image_sha256=image_sha256, transport_epoch=transport_epoch,
                            programming_may_have_started=True)
        self.retired = self.finished = self.entered = self.completed = self.violated = False
        self.ownership_failed = False
        self._run_lock = threading.Lock()
        self._dispatch_lock = threading.Lock()

    @property
    def binding(self):
        return copy.deepcopy(self._binding)

    def _owned(self):
        try:
            owned = self.owns() is True
        except BaseException:
            self.ownership_failed = True
            raise
        if not owned:
            self.ownership_failed = True
            raise ValueError('native dispatch ownership lost')

    def run(self, admitted, program):
        """admitted(dispatch) must use its existing synchronous one-use gate."""
        if not self._run_lock.acquire(blocking=False):
            raise ValueError('native program admission is active')
        try:
            if self.retired:
                raise ValueError('native program admission is retired')
            self.retired = True
            if not callable(admitted) or not callable(program):
                raise ValueError('admission and program callbacks required')
            if self.marker.exists() or self.marker.is_symlink():
                raise FileExistsError('native execution marker already exists')
            self._owned()

            def dispatch(*args, **kwargs):
                if not self._dispatch_lock.acquire(blocking=False):
                    self.violated = True
                    raise ValueError('native dispatch is active')
                try:
                    if self.entered:
                        self.violated = True
                        raise ValueError('native dispatch already consumed')
                    # Set before persistence: even partial/failed fsync leaves
                    # an uncertain attempt. Never infer no execution from it.
                    self.entered = True
                    self._owned()
                    _persist(self.marker, self.binding)
                    self._owned()
                    result = program(*args, **kwargs)
                    self.completed = True
                    return result
                finally:
                    self._dispatch_lock.release()

            result = admitted(dispatch)
            if not self.completed or self.violated:
                raise ValueError('admission did not dispatch exactly one native program')
            return result
        finally:
            self.finished = True
            self._run_lock.release()

    def observation(self):
        """Report this in-process operation; never a standalone reset waiver."""
        owned = False
        try:
            owned = self.owns() is True
        except BaseException:
            pass
        if not owned:
            self.ownership_failed = True
        owned = owned and not self.ownership_failed
        marker_present = True
        try:
            marker_present = self.marker.exists() or self.marker.is_symlink()
        except OSError:
            pass
        possible = not self.finished or not owned or self.entered or marker_present
        return dict(scope='nano-native-program-dispatch-observation-v1',
                    operation_finished=self.finished, ownership_verified=owned,
                    ownership_failed=self.ownership_failed,
                    programming_may_have_started=possible,
                    marker_path=str(self.marker), hardware_acceptance=False,
                    recovery_authority=False)
