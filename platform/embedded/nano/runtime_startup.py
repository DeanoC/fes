"""Bounded cold BL31 progress observations; never a hardware permission."""
import re

RUNTIME_PHASES = (
    'main-before', 'platform-before', 'owned-before', 'owned-after',
    *(name+'-'+edge for name in ('gic-driver','gic-distributor','gic-redistributor',
                                'gic-cpu','mailbox') for edge in ('before','after')),
    'platform-after',
    *(name+'-'+edge for name in ('libraries','services','next-image','runtime','console')
      for edge in ('before','after')),
    'main-after', 'data-clean-before', 'data-clean-after',
    'bss-clean-before', 'bss-clean-after', 'exit-before',
)


def state():
    return dict(records=[], valid=True, complete=False)


EXIT_PHASES = ('clean-before','clean-after')


def exit_state():
    return dict(records=[], valid=True, complete=False, maintenance_may_have_started=False)


def observe_exit(progress,line,number,*,startup_valid,runtime_phase,exception,uboot_seen):
    def invalid():
        progress['valid']=progress['complete']=False
        return f'invalid cold exit cache trace at line {number}'
    if (not startup_valid or not progress['valid'] or progress['complete'] or
            exception or uboot_seen or runtime_phase!='bss-clean-before' or
            len(progress['records'])>=len(EXIT_PHASES)):
        return invalid()
    match=re.fullmatch(r'FES_NANO_RAM EXIT_CACHE phase=(clean-before|clean-after)',line)
    if match is None or match[1]!=EXIT_PHASES[len(progress['records'])]:
        return invalid()
    progress['records'].append(dict(phase=match[1],line=number))
    progress['maintenance_may_have_started']=True
    progress['complete']=len(progress['records'])==len(EXIT_PHASES)
    return None


def observe(progress, line, number, *, startup_valid, vendor_complete,
            cache_complete, logging_restored, exception, provider_seen,
            sd_seen, uboot_seen, exit_cache_complete):
    def invalid():
        progress['valid'] = progress['complete'] = False
        return f'invalid cold runtime startup at line {number}'
    if (not progress['valid'] or progress['complete'] or not startup_valid or
            not vendor_complete or not cache_complete or not logging_restored or
            exception or uboot_seen or len(progress['records']) >= len(RUNTIME_PHASES)):
        return invalid()
    match = re.fullmatch(r'FES_NANO_RAM RUNTIME phase=([a-z-]+)', line)
    expected = RUNTIME_PHASES[len(progress['records'])]
    if match is None or match[1] != expected:
        return invalid()
    if expected in ('bss-clean-after','exit-before') and not exit_cache_complete:
        return invalid()
    if ((len(progress['records']) < 3 and (provider_seen or sd_seen)) or
            (len(progress['records']) >= 3 and not sd_seen)):
        return invalid()
    progress['records'].append(dict(phase=expected,line=number))
    progress['complete'] = len(progress['records']) == len(RUNTIME_PHASES)
    return None
