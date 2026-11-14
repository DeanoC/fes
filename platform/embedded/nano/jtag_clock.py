"""One owned fixed-rate host link; restore both parameters after any write.

This never operates a cable itself or grants programming authority. Bound
callbacks must retain their exact private server, target and process limits.
"""
import threading


class FixedClock:
    def __init__(self, guard, read, write):
        if not all(callable(f) for f in (guard, read, write)):
            raise ValueError('bound clock operations required')
        self.guard, self.read, self.write = guard, read, write
        self.started = self.attempted = self.active = self.consumed = False
        self.original = self.readback = None
        self.restored = False
        self.errors = []
        self._lock = threading.Lock()

    def _check(self):
        if self.guard() is not True:
            raise RuntimeError('owned top-only transport required')

    def _read(self, name):
        self._check()
        return self.read(name)

    def _write(self, name, value):
        self._check()
        self.write(name, value)

    def _snapshot(self):
        automatic = self._automatic()
        clock = self._read('JtagClock')
        if clock not in ('15M', '3M'):
            raise RuntimeError('unexpected clock parameters')
        return dict(auto_adjust=automatic, clock=clock)

    def _automatic(self):
        automatic = self._read('JtagClockAutoAdjust')
        if type(automatic) is not int or automatic not in (0, 1):
            raise RuntimeError('invalid automatic clock adjustment state')
        return automatic

    def start(self):
        with self._lock:
            if self.started or self.consumed:
                raise RuntimeError('clock transition already consumed')
            self.started = True
            self.original = self._snapshot()
            if self.original['clock'] != '15M':
                raise RuntimeError('expected original15MHz condition absent')
            # Even a throwing setter may have changed the cable parameter.
            self.attempted = True
            self._write('JtagClockAutoAdjust', 0)
            if self._automatic() != 0:
                raise RuntimeError('automatic clock adjustment remains enabled')
            self._write('JtagClock', '3M')
            self._verify()
            self.active = True

    def _verify(self):
        current = self._snapshot()
        if current != dict(auto_adjust=0, clock='3M'):
            raise RuntimeError('fixed programming clock changed')
        return current

    def verify(self):
        with self._lock:
            if not self.active or self.consumed:
                raise RuntimeError('fixed clock is not active')
            try:
                return self._verify()
            except Exception:
                self.active = False
                raise

    def restore(self):
        with self._lock:
            if self.consumed:
                return self.status()
            self.consumed, self.active = True, False
            if self.attempted:
                # Attempt both independent restorations even if one fails.
                # Every callback is guarded; loss of ownership prevents I/O.
                for name, value in (('JtagClock', self.original['clock']),
                                    ('JtagClockAutoAdjust', self.original['auto_adjust'])):
                    try:
                        self._write(name, value)
                    except Exception as error:
                        self.errors.append(str(error))
                try:
                    self.readback = self._snapshot()
                    self.restored = self.readback == self.original and not self.errors
                except Exception as error:
                    self.errors.append(str(error))
                if not self.restored:
                    raise RuntimeError('original clock restoration unproven: '+repr(self.errors))
            return self.status()

    def status(self):
        return dict(original=None if self.original is None else dict(self.original),
                    write_attempted=self.attempted, active=self.active,
                    restore_consumed=self.consumed, restored=self.restored,
                    readback=None if self.readback is None else dict(self.readback),
                    errors=list(self.errors))
