"""Exclusive recovery console requiring the full trace before TFTP can succeed."""
import re
from upload_transport import Console
from recovery_return_trace import decode


class TraceConsole(Console):
    def __init__(self, *args, **kwargs):
        self.retired=False
        self._trace_closed=False
        self.return_traces=[]
        super().__init__(*args, **kwargs)

    def command(self, value, timeout=15):
        if self.retired:
            raise ValueError('return-trace console is retired')
        try:
            transfer=value.startswith('tftpboot')
            if transfer and (not re.fullmatch(r'tftpboot 82000000 U[A-F0-9]{6}\.BIN',value) or timeout!=90):
                raise ValueError('return trace requires the fixed bounded TFTP command')
            output=super().command(value,timeout)
            # The base console already requires the fresh result nonce and
            # prompt. A complete boundary trace cannot replace either proof.
            if transfer:
                observation=decode(output)
                self.return_traces.append(dict(command=value,base_command_verified=True,trace=observation))
                if not observation['valid'] or not observation['complete']:
                    raise ValueError('TFTP return observations incomplete or invalid')
            elif 'FES_RECOVERY_RETURN' in output:
                raise ValueError('return observations outside their TFTP command')
            return output
        except BaseException:
            self.retired=True
            raise

    def park(self):
        if self.retired:
            raise ValueError('retired return-trace console sends no recovery control')
        try:
            return super().park()
        except BaseException:
            self.retired=True
            raise

    def close(self):
        self.retired=True
        if getattr(self,'_trace_closed',False):
            return
        self._trace_closed=True
        return super().close()
