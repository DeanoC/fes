"""Bind the PC/XT video mailbox declaration to its Quartus transport names.

This is a structural precondition for the optional diagnostic SDC. The actual
mailbox proof, native timing, reset pulse/MTBF and physical IO remain distinct.
"""
def validate_mailbox(before: dict, after: dict) -> dict:
    def require(condition, message):
        if not condition:
            raise ValueError('PC/XT mailbox: ' + message)

    cells = after['cells']
    roles = ('received', 'sent', 'ack_meta', 'ack_sync', 'sent_meta', 'sent_sync',
             'hold_meta', 'hold_sync', 'published_bank', 'front')
    names = {r: 'system.video.adapter.' + r for r in roles}
    names.update(hold_source='system.machine_reset', board_reset='board_reset_domain.board_reset',
                 pixel_reset='pixel_reset')
    wires, emitted, flops = {}, {}, {}
    for role, name in names.items():
        require(name in before['netnames'], 'missing declaration ' + name)
        wire = before['netnames'][name]
        matches = [n for n, w in after['netnames'].items() if w == wire]
        expected = name.replace('.', '_')
        require(matches == [expected], 'ambiguous declaration transport ' + name)
        wires[role], emitted[role] = wire, expected
        drivers = [c for c in cells.values() if c['connections'].get('Q') == wire['bits']]
        require(len(drivers) == 1, 'missing or ambiguous register driver ' + name)
        flops[role] = drivers[0]

    def integer(cell, parameter):
        value = cell['parameters'].get(parameter)
        require(isinstance(value, str) and bool(value) and set(value) <= {'0', '1'},
                'invalid register parameter ' + parameter)
        return int(value, 2)

    require(cells.get('memory_clock', {}).get('type') == 'altera_pll' and
            cells.get('video_clock', {}).get('type') == 'altera_pll', 'missing PLL roots')
    board = cells['memory_clock']['connections']['outclk'][2]
    pixel = cells['video_clock']['connections']['outclk'][0]
    require(board != pixel, 'board/pixel clocks collapsed')
    for role, clock in [('received', pixel), ('sent', board), ('hold_source', board),
                        ('published_bank', board), ('front', pixel)]:
        f = flops[role]
        require(f['type'] in {'$dff', '$sdff', '$dffe', '$sdffe', '$sdffce'} and
                f['connections']['CLK'] == [clock] and integer(f, 'CLK_POLARITY') == 1,
                'wrong launch/capture register ' + role)
        require(len(wires[role]['bits']) == (2 if role in {'published_bank', 'front'} else 1),
                'wrong mailbox register width ' + role)

    resets = []
    for role, clock in [('board_reset', board), ('pixel_reset', pixel)]:
        f, bits = flops[role], wires[role]['bits']
        require(wires[role]['attributes'].get('async_reg') == 'true' and len(bits) == 2 and
                f['type'] == '$adff' and f['connections']['CLK'] == [clock] and
                f['connections']['D'] == ['0', bits[0]] and
                integer(f, 'CLK_POLARITY') == 1 and integer(f, 'ARST_POLARITY') == 0 and
                integer(f, 'ARST_VALUE') == 3, 'changed two-stage reset release ' + role)
        resets.append(f['connections']['ARST'])
    require(resets[0] == resets[1], 'independent reset assertion')
    locked = [c for c in cells.values() if c['connections'].get('Y') == resets[0]]
    require(len(locked) == 1 and locked[0]['type'] == '$logic_and' and
            {tuple(locked[0]['connections']['A']), tuple(locked[0]['connections']['B'])} ==
            {tuple(cells['memory_clock']['connections']['locked']),
             tuple(cells['video_clock']['connections']['locked'])}, 'changed common PLL-lock assertion')

    chains = []
    for role, source, clock, reset in [('ack', 'received', board, 'board_reset'),
                                      ('sent', 'sent', pixel, 'pixel_reset'),
                                      ('hold', 'hold_source', pixel, 'pixel_reset')]:
        h, t = role + '_meta', role + '_sync'
        head, tail = flops[h], flops[t]
        require(head['type'] == tail['type'] == '$sdff' and
                head['parameters'] == tail['parameters'] and integer(head, 'WIDTH') == 1 and
                integer(head, 'CLK_POLARITY') == integer(head, 'SRST_POLARITY') == 1 and
                integer(head, 'SRST_VALUE') == 0, 'changed synchronizer registers ' + role)
        require(head['connections']['CLK'] == tail['connections']['CLK'] == [clock] and
                head['connections']['SRST'] == tail['connections']['SRST'] == [wires[reset]['bits'][1]] and
                head['connections']['D'] == wires[source]['bits'] and
                tail['connections']['D'] == wires[h]['bits'], 'changed synchronizer connectivity ' + role)
        for stage in (h, t):
            require(wires[stage]['attributes'].get('async_reg') == 'true', 'missing synchronizer intent ' + stage)
        consumers = [(c, p) for c in cells.values() for p, bits in c['connections'].items()
                     if c.get('port_directions', {}).get(p) == 'input' and
                     set(bits) & set(wires[h]['bits'])]
        require(len(consumers) == 1 and consumers[0] == (tail, 'D'), 'interstage logic or fanout ' + role)
        chains.append({'source': emitted[source], 'head': emitted[h], 'tail': emitted[t],
                       'clock_bit': clock, 'reset': emitted[reset]})
    return {'chains': chains, 'published': emitted['published_bank'], 'front': emitted['front'],
            'held_data_stable_pixel_samples': 2, 'max_delay_pixel_periods': 1,
            'common_reset_assertion_verified': True,
            'physical_reset_and_metastability_qualified': False}
