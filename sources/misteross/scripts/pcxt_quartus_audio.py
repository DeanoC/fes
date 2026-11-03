"""Bind the PC/XT audio clock-domain synchronizers to emitted Quartus names."""

PREFIX = 'speaker_output.audio.output_stage.'


def validate_audio(before: dict, after: dict) -> dict:
    def require(ok, message):
        if not ok:
            raise ValueError('PC/XT audio: ' + message)

    cells = after['cells']
    names = [PREFIX + role + '_' + stage
             for role in ('request', 'ack', 'hold', 'lock') for stage in ('meta', 'sync')]
    names += [PREFIX + 'request', PREFIX + 'acknowledge', PREFIX + 'held_sample', PREFIX + 'pcm',
              'speaker_output.audio.hold']
    wires, flops = {}, {}
    for name in names:
        require(name in before['netnames'], 'missing declaration ' + name)
        wire = before['netnames'][name]
        emitted = name.replace('.', '_')
        require([n for n, w in after['netnames'].items() if w == wire] == [emitted],
                'ambiguous declaration transport ' + name)
        wires[name] = wire
        drivers = [c for c in cells.values() if c['connections'].get('Q') == wire['bits']]
        if name != 'speaker_output.audio.hold':
            require(len(drivers) == 1, 'missing or ambiguous register ' + name)
            flops[name] = drivers[0]

    board = cells['memory_clock']['connections']['outclk'][0]
    pll = cells['speaker_output_audio_clock_pll']
    audio = pll['connections']['outclk'][0]
    locked = pll['connections']['locked']
    require(board != audio, 'board/audio clocks collapsed')
    for name, clk in ((PREFIX + 'request', audio), (PREFIX + 'acknowledge', board)):
        f = flops[name]
        require(f['type'] in ('$dff', '$dffe') and f['connections']['CLK'] == [clk],
                'wrong source register ' + name)
    held, pcm = flops[PREFIX + 'held_sample'], flops[PREFIX + 'pcm']
    require(held['type'] == pcm['type'] == '$dffe' and
            held['connections']['CLK'] == [board] and pcm['connections']['CLK'] == [audio] and
            len(held['connections']['Q']) == len(pcm['connections']['Q']) == 32 and
            pcm['connections']['D'] == held['connections']['Q'], 'changed held PCM payload')
    def enable_is(cell, op, left, right):
        gates = [c for c in cells.values() if c['type'] == op and
                 c['connections'].get('Y') == cell['connections']['EN']]
        return len(gates) == 1 and {tuple(gates[0]['connections']['A']), tuple(gates[0]['connections']['B'])} == {
            tuple(wires[left]['bits']), tuple(wires[right]['bits'])}
    require(enable_is(held, '$ne', PREFIX + 'request_sync', PREFIX + 'acknowledge') and
            held['connections']['EN'] == flops[PREFIX + 'acknowledge']['connections']['EN'] and
            enable_is(pcm, '$eq', PREFIX + 'ack_sync', PREFIX + 'request'),
            'changed payload handshake enable')
    reset_names = ('system_machine_reset', 'sys_reset', 'fes_computer_runtime_pcxt_floppy_gp_mailbox_core_exec_reset')
    require(all(n in after['netnames'] for n in reset_names), 'missing hold source declaration')
    reset_bits = {after['netnames'][reset_names[0]]['bits'][0], after['netnames'][reset_names[1]]['bits'][1],
                  after['netnames'][reset_names[2]]['bits'][0]}
    def hold_leaves(bit):
        gates = [c for c in cells.values() if c['connections'].get('Y') == [bit]]
        if not gates: return {bit}
        require(len(gates) == 1 and gates[0]['type'] == '$logic_or', 'changed hold logic')
        return hold_leaves(gates[0]['connections']['A'][0]) | hold_leaves(gates[0]['connections']['B'][0])
    require(hold_leaves(wires['speaker_output.audio.hold']['bits'][0]) == reset_bits,
            'changed hold sources')
    chains = []
    for role, source, clock in (('request', PREFIX + 'request', board),
                                ('ack', PREFIX + 'acknowledge', audio),
                                ('hold', 'speaker_output.audio.hold', audio),
                                ('lock', None, audio)):
        h, t = PREFIX + role + '_meta', PREFIX + role + '_sync'
        head, tail = flops[h], flops[t]
        typ = '$adff' if role == 'lock' else '$dff'
        require(head['type'] == tail['type'] == typ and head['parameters'] == tail['parameters'] and
                int(head['parameters']['WIDTH'], 2) == 1 and int(head['parameters']['CLK_POLARITY'], 2) == 1,
                'changed synchronizer registers ' + role)
        require(head['connections']['CLK'] == tail['connections']['CLK'] == [clock] and
                tail['connections']['D'] == wires[h]['bits'] and
                head['connections']['D'] == (['1'] if source is None else wires[source]['bits']),
                'changed synchronizer connectivity ' + role)
        if role == 'lock':
            require(head['connections']['ARST'] == tail['connections']['ARST'] == locked and
                    int(head['parameters']['ARST_POLARITY'], 2) == 0 and
                    int(head['parameters']['ARST_VALUE'], 2) == 0,
                    'changed audio PLL lock release')
        for stage in (h, t):
            require(wires[stage]['attributes'].get('async_reg') == 'true', 'missing synchronizer intent ' + stage)
        consumers = [(c, p) for c in cells.values() for p, bits in c['connections'].items()
                     if c.get('port_directions', {}).get(p) == 'input' and set(bits) & set(wires[h]['bits'])]
        require(len(consumers) == 1 and consumers[0] == (tail, 'D'), 'interstage logic or fanout ' + role)
        chains.append({'head': h.replace('.', '_'), 'tail': t.replace('.', '_'), 'clock_bit': clock})
    return {'chains': chains, 'physical_reset_and_metastability_qualified': False}
