"""Best-effort parent build timing and cache diagnostics."""
from contextlib import contextmanager
import json
from pathlib import Path
import time


class BuildDiagnostics:
    """Write non-authorizing diagnostics without exposing build inputs."""

    def __init__(self, output, action):
        self.output = Path(output)
        self.path = self.output / 'build-diagnostics.json'
        self.started = time.monotonic()
        self.data = {
            'format': 1,
            'action': str(action),
            'scope': 'parent',
            'status': 'running',
            'elapsed_seconds': 0.0,
            'stages': [],
        }
        try:
            self.output.mkdir(parents=True, exist_ok=True)
        except OSError:
            # The build's own output operation remains responsible for any
            # filesystem error; diagnostics are advisory only.
            pass
        self._write()

    def _elapsed(self, started):
        return max(0.0, time.monotonic() - started)

    def _write(self):
        try:
            temporary = self.path.with_name(self.path.name + '.tmp')
            temporary.write_text(json.dumps(self.data, indent=2, sort_keys=True) + '\n')
            temporary.replace(self.path)
        except OSError:
            # Diagnostics must never alter the build's receipt or failure path.
            pass

    def cache(self, name, status, reason):
        if status not in ('hit', 'miss', 'forced'):
            raise ValueError('invalid diagnostic cache status')
        self.data['stages'].append({'name': str(name), 'status': status,
                                    'reason': str(reason)})
        self._write()

    @contextmanager
    def measure(self, name):
        started = time.monotonic()
        try:
            yield
        except BaseException:
            self.data['stages'].append({
                'name': str(name), 'status': 'failed',
                'scope': 'parent-subprocess-wrapper',
                'elapsed_seconds': self._elapsed(started),
            })
            self.data['status'] = 'failed'
            self.data['elapsed_seconds'] = self._elapsed(self.started)
            self._write()
            raise
        else:
            self.data['stages'].append({
                'name': str(name), 'status': 'success',
                'scope': 'parent-subprocess-wrapper',
                'elapsed_seconds': self._elapsed(started),
            })
            self._write()

    def finish(self, status):
        if status not in ('success', 'failed'):
            raise ValueError('invalid diagnostic final status')
        if self.data['status'] != 'failed' or status == 'failed':
            self.data['status'] = status
        self.data['elapsed_seconds'] = self._elapsed(self.started)
        self._write()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_value, traceback):
        self.finish('failed' if exc_type is not None else 'success')
        return False
