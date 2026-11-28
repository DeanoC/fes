    // The DE25 routed x87 fast lane adds six clocks to these dependent forms.
    // The one-stage wrapper instead completes the same multiply in 13 clocks.
    {
        constexpr int fast_clocks = 19;
        constexpr unsigned fmul = 0x0c9, fadd = 0x0c1;
        const uint8_t two[10] = {0,0,0,0,0,0,0,0x80,0x00,0x40};
        const uint8_t three[10] = {0,0,0,0,0,0,0,0xc0,0x00,0x40};
        const uint8_t four[10] = {0,0,0,0,0,0,0,0x80,0x01,0x40};
        const uint8_t six[10] = {0,0,0,0,0,0,0,0xc0,0x01,0x40};
        const uint8_t eight[10] = {0,0,0,0,0,0,0,0x80,0x02,0x40};
        const uint8_t zero[10] = {};
        const uint8_t infinity[10] = {0,0,0,0,0,0,0,0x80,0xff,0x7f};
        Unit u;
        auto initialize = [&](const uint8_t *first, const uint8_t *second) {
            u.clear_latch();
            u.reg_form(OP_FNINIT);
            u.load(OP_FLD_M80, FMT_F80, first);
            u.load(OP_FLD_M80, FMT_F80, second);
        };
        auto waiting = [&](bool presented) {
            u.top.iss = u.top.iss_wait = u.top.iss_first = presented;
            u.top.eval();
        };
        auto stack_is = [&](const uint8_t *value) {
            uint8_t result[10];
            u.st0(result);
            return std::memcmp(result, value, sizeof result) == 0;
        };

        // Sweep later pipeline squashes over every clock of a retired FMUL.
        // FWAIT must still hold until the product is architecturally visible.
        std::string failure;
        int measured = 0;
        for (int at = -1; at < fast_clocks; ++at) {
            initialize(two, three);
            u.present(fmul, 0, true, true, 0);
            u.commit(0, 0, true);
            waiting(true);
            int clocks = 0;
            bool held = true;
            while (u.top.busy_o && clocks < fast_clocks + 1) {
                held &= u.top.hold;
                u.top.flush = clocks == at;
                u.tick();
                u.top.flush = 0;
                ++clocks;
            }
            if (at == -1) measured = clocks;
            if ((!held || clocks != fast_clocks || u.top.hold || !stack_is(six)) && failure.empty())
                failure = "flush offset " + std::to_string(at) + ", busy clocks " + std::to_string(clocks);
            waiting(false);
        }
        std::printf("LATENCY fast-fmul-wait pc64 %d\n", measured);
        if (failure.empty()) ok("fast-pipeline-fwait-retired-flush");
        else bad("fast-pipeline-fwait-retired-flush", failure);

        // A younger FMUL squashed before writeback may leave neither state
        // changes nor a completion that masquerades as the next fast result.
        initialize(two, three);
        u.present(fmul, 0, true, true, 0);
        u.top.flush = 1;
        u.tick();
        u.top.flush = 0;
        u.idle(fast_clocks);
        waiting(true);
        const bool squash_clean = !u.top.busy_o && !u.top.hold && stack_is(three);
        waiting(false);
        u.reg_form(fmul);
        if (squash_clean && stack_is(six)) ok("fast-pipeline-unretired-squash");
        else bad("fast-pipeline-unretired-squash", "squash changed ST(0), hold, or the next result");

        // Reset via F1h and the machine reset while every pipeline slot is
        // exercised. An abandoned product must never become the next sum.
        for (bool machine_reset : {false, true}) {
            const char *label = machine_reset ? "fast-pipeline-machine-reset" : "fast-pipeline-f1-reset";
            failure.clear();
            for (int at = 0; at < fast_clocks; ++at) {
                initialize(two, three);
                u.present(fmul, 0, true, true, 0);
                u.commit(0, 0, true);
                u.idle(at);
                (machine_reset ? u.top.reset : u.top.f1) = 1;
                u.tick();
                u.top.reset = u.top.f1 = 0;
                u.top.eval();
                const bool cleared = !u.top.busy_o && u.tw() == 0xffff &&
                                     u.cw() == (machine_reset ? 0x037f : 0x037e) &&
                                     u.sw() == (machine_reset ? 0x0000 : 0x8081);
                u.load(OP_FLD_M80, FMT_F80, four);
                u.load(OP_FLD_M80, FMT_F80, four);
                u.reg_form(fadd);
                if ((!cleared || !stack_is(eight)) && failure.empty())
                    failure = "reset offset " + std::to_string(at);
            }
            if (failure.empty()) ok(label);
            else bad(label, failure);
        }

        // The fast invalid product latches BUSY# without giving a waiting
        // FWAIT one free clock between arithmetic completion and the error.
        initialize(zero, infinity);
        u.present(OP_FLDCW, FMT_I16, true, true, 0);
        u.commit(0, 0x037e, true);
        u.settle();
        u.present(fmul, 0, true, true, 0);
        u.commit(0, 0, true);
        waiting(true);
        bool held = true;
        int clocks = 0;
        while (u.top.busy_o && clocks < fast_clocks + 1) {
            held &= u.top.hold;
            u.tick();
            ++clocks;
        }
        const bool raised = held && u.top.hold && u.top.error && u.top.latched &&
                            (u.sw() & 0x0081) == 0x0081 && stack_is(infinity);
        waiting(false);
        u.reg_form(OP_FNCLEX);
        waiting(true);
        const bool still_latched = !u.top.error && u.top.hold;
        u.clear_latch();
        if (raised && still_latched && !u.top.hold) ok("fast-pipeline-fwait-exception");
        else bad("fast-pipeline-fwait-exception", "invalid multiply failed the ERROR#/BUSY# sequence");
    }
