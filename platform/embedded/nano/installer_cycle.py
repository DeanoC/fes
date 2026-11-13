"""Explicit install/discovery cycles and saved-wire replay; no device authority.

Discovery is an observed catalogue state, not persistence proof. The operator
must independently admit a new boot and read back the empty bootstrap/source
files without rewriting them before joining the two hardware experiments.
"""
import copy
import math

from installer_client import InstallerClient
from service_trace import number, require
from tenfoot_trace import MAX_BYTES


def exercise(client, *, discovered, settle):
    require(type(discovered) is bool and callable(settle), 'explicit discovery context and settle hook required')
    require(client.installed == [discovered]*len(client.programs) and not client.retired and client.loads == 0,
            'cycle context differs from the fresh observed catalogue')
    count = len(client.programs)
    for selection in range(count):
        client.open_source(selection)
        if not discovered:
            client.install()
        client.open_library()
    for selection in range(count):
        client.focus(selection)
        client.key('launch',selection)
        settle(.5)
        client.key('quit' if selection == count-1 else 'stop')
    origin = 0 if discovered else count
    require(client.loads == count and client.retired and client.records[-1]['exit_generation'] == origin+2*count+1,
            'installer launch/return did not reach final Park')
    menu = client.menu.finish()
    sessions = 1+count+(0 if discovered else count)
    require(menu['menu_sessions'] == sessions and menu['final_return']['Generation'] == 7+11*count+(0 if discovered else 6*count),
            'installer launch/return omitted final menu drain')
    usb = client.records[-1]['shared_usb']['stop'] if client.usb is not None else None
    return dict(passed=True,installed_during_test=not discovered,initially_discovered=discovered,
                fetch_activity_observed=True,display='fpga-menu',menu=menu,
                launch_generation=origin+1,exit_generation=origin+2*count+1,
                shared_usb_reports=usb['reports'] if usb is not None else None,
                shared_usb_polls=usb['polls'] if usb is not None else None,
                hardware_acceptance=False,ordinary_admission=False,
                physical_display_verified=False,persistence_verified=False)


def _seconds(value):
    try:
        return type(value) in (int,float) and math.isfinite(value) and value >= 0
    except OverflowError:
        return False


def replay(raw, receipt, *, discovered, **options):
    """Check every saved command/offer under the caller's independent context."""
    require(type(discovered) is bool, 'explicit replay discovery context required')
    require(type(raw) is bytes and 0 < len(raw) <= MAX_BYTES and type(receipt) is dict,
            'bounded saved wire and receipt required')
    initial = receipt['initial_capture_end']
    require(number(initial,len(raw)) and initial > 0, 'invalid startup capture extent')
    windows, commands = receipt['command_windows'], receipt['app_commands']
    require(type(windows) is list and type(commands) is list and
            0 < len(windows) == len(commands) <= len(raw), 'missing or unbounded command capture')
    previous = initial
    for window in windows:
        require(type(window) is dict and window['status'] == 'received' and
                type(window['start']) is int and window['start'] == previous and
                number(window['end'],len(raw)) and previous < window['end'],
                'noncontiguous or invalid command capture')
        elapsed, bound = window['elapsed_seconds'], window['bound_seconds']
        require(_seconds(elapsed) and _seconds(bound) and 0 <= elapsed < bound,
                'invalid saved action deadline')
        offers = window['offers']
        require(type(offers) is list and 0 < len(offers) <= window['end']-previous+1,
                'missing or unbounded polling offers')
        end, stamp = previous-1, 0
        for offer in offers:
            require(type(offer) is dict and number(offer['end'],window['end']) and
                    previous <= offer['end'] and end < offer['end'] and
                    _seconds(offer['elapsed_seconds']) and stamp <= offer['elapsed_seconds'] <= elapsed,
                    'saved offer changed byte extent or clock order')
            end, stamp = offer['end'], offer['elapsed_seconds']
        require(end == window['end'], 'terminal polling offer missing')
        previous = window['end']
    require(not raw[previous:].strip(), 'unexpected output after final Park')
    clock, index, offered = 0., 0, 0
    def exchange(payload, bound, done):
        nonlocal clock, index, offered
        require(index < len(windows), 'replay requested an uncaptured command')
        window = windows[index]
        index += 1
        require(payload.hex() == window['request_hex'] and bound == window['bound_seconds'],
                'saved command or bound differs')
        before, finished = clock, False
        for offer in window['offers']:
            clock = before+offer['elapsed_seconds']
            offered += 1
            finished = done(raw[window['start']:offer['end']])
            require(not finished or offer['end'] == window['end'],
                    'saved transport continued after completed exchange')
        require(finished, 'replay did not reach a verified exchange')
        clock = before+window['elapsed_seconds']
        return raw[window['start']:window['end']]
    client = InstallerClient(raw[:initial],installed=[discovered]*len(options['programs']),exchange=exchange,
                             now=lambda:clock,owns=lambda:True,**options)
    require(client.view == receipt['startup_reply'] and client.boot == receipt['startup_service_boot'],
            'saved startup receipt differs from wire')
    def settle(seconds):
        nonlocal clock
        require(seconds == .5, 'unexpected settle')
        clock += seconds
    result = exercise(client,discovered=discovered,settle=settle)
    require(index == len(windows) and result == receipt['service'] and client.retired,
            'saved service differs from wire')
    require(len(client.records) == len(commands), 'saved action count differs')
    for actual, saved in zip(client.records,commands):
        actual, saved = copy.deepcopy(actual), copy.deepcopy(saved)
        require(('elapsed_seconds' in actual) == ('elapsed_seconds' in saved), 'saved timing fields differ')
        actual.pop('elapsed_seconds',None)
        saved.pop('elapsed_seconds',None)
        require(actual == saved, 'saved action receipt differs from wire')
    return dict(status='passed-saved-installer-wire-and-poll-times',discovered=discovered,
                commands=index,polling_offers=offered,hardware_actions=False,
                live_authority=False,persistence_verified=False)
