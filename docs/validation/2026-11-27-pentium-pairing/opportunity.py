"""Interpret a verified C28 window at a fixed clock and unchanged stall costs."""


def opportunity(window, clock_hz):
    cycles, retired, pairs = (window[k] for k in ('cycles', 'retired', 'pairs'))
    x87 = window['x87']
    if cycles != x87['sampled_cpu_clocks'] + window['parked_cycles']:
        raise ValueError('the x87 samples and parked clocks do not cover the window')
    if x87['x87_hold_asserted'] != (x87['hold_redirect_or_flush'] +
                                    x87['hold_with_another_issue_blocker'] +
                                    x87['sole_x87_issue_blocker']):
        raise ValueError('the x87 hold classes do not partition the held clocks')
    if sum(x87['sole_by_hold_term_set'].values()) != x87['sole_x87_issue_blocker']:
        raise ValueError('the x87 hold reasons do not cover the sole-blocker clocks')
    dependency_classes = ('sole_stable_stack_reg_arith_raw', 'sole_stable_stack_reg_arith_no_raw',
                          'sole_register_raw_unclassified')
    if sum(x87[k] for k in dependency_classes) != x87['sole_x87_issue_blocker']:
        raise ValueError('the x87 register dependency classes do not cover the sole-blocker clocks')
    if not 0 <= 2 * pairs <= retired - window['exceptions'] or not 0 < retired <= cycles:
        raise ValueError('invalid retirement or non-overlapping pair counts')
    # Count one saved issue cycle per successful pair. A pair spanning an
    # existing bubble can save zero. Other pipeline changes can alter stalls.
    return {
        'retirements_in_candidate_pairs_pct': 200 * pairs / retired,
        'pairing_only_max_cycles_saved_pct': 100 * pairs / cycles,
        'pairing_only_max_same_clock_speedup': cycles / (cycles - pairs),
        'pairing_only_best_case_break_even_mhz': float(clock_hz / 1_000_000) * (1 - pairs / cycles),
        'sole_x87_issue_blocker_pct_of_window': 100 * x87['sole_x87_issue_blocker'] / cycles,
    }
