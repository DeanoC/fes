
    if (const char *plan_path = std::getenv("PCXT_DIAGNOSTIC_MOVE_PLAN")) {
        if (!vm.count("no-pack") || !vm.count("no-place") || !vm.count("no-route") || routed)
            log_error("Diagnostic move requires an unrouted placed checkpoint and no pack/place/route.\n");
        std::ifstream input(plan_path); std::string text((std::istreambuf_iterator<char>(input)), {}), error;
        auto plan = json11::Json::parse(text, error);
        if (!error.empty() || !plan.is_object()) log_error("Invalid diagnostic plan.\n");
        auto name = ctx->id(plan["cell"].string_value());
        if (!ctx->cells.count(name)) log_error("Missing diagnostic cell.\n");
        auto cell = ctx->cells.at(name).get();
        if (cell->cluster != ClusterId() || cell->region || cell->type != id_MISTRAL_ALUT5)
            log_error("Diagnostic requires an unclustered unconstrained ALUT5.\n");
        for (const auto &net : ctx->nets) if (!net.second->wires.empty()) log_error("Diagnostic requires no routes.\n");
        for (const auto &entry : ctx->cells) if (entry.second->bel != BelId() && !ctx->isBelLocationValid(entry.second->bel)) { log_info("DIAGNOSTIC baseline invalid %s at %s\n", ctx->nameOf(entry.second.get()), ctx->getBelName(entry.second->bel).str(ctx).c_str()); ctx->isBelLocationValid(entry.second->bel, true); }
        auto original = cell->bel; auto strength = cell->belStrength;
        const auto saved_labs = ctx->labs;
        const auto saved_info = static_cast<const ArchCellInfo &>(*cell);
        std::vector<BelId> candidates;
        for (auto bel : ctx->getBels()) {
            auto loc = ctx->getBelLocation(bel);
            if (loc.x == plan["x"].int_value() && loc.y == plan["y"].int_value() &&
                ctx->getBelType(bel).in(id_MISTRAL_COMB, id_MISTRAL_MCOMB) && !ctx->getBoundBelCell(bel)) candidates.push_back(bel);
        }
        std::sort(candidates.begin(), candidates.end(), [&](BelId a, BelId b) { return ctx->getBelName(a).str(ctx) < ctx->getBelName(b).str(ctx); });
        std::set<BelId> legal;
        for (auto bel : candidates) {
            ctx->unbindBel(original); ctx->bindBel(bel, cell, strength);
            for (const auto &lab : ctx->labs) { auto loc = ctx->getBelLocation(lab.alms[0].lut_bels[0]); if (loc.x == plan["x"].int_value() && loc.y == plan["y"].int_value()) { int inputs = 0; for (const auto &alm : lab.alms) inputs += alm.unique_input_count; log_info("DIAGNOSTIC target LAB input sum=%d limit=42\n", inputs); } }
            bool valid = true;
            for (const auto &entry : ctx->cells) if (entry.second->bel != BelId() && ((ctx->getBelLocation(entry.second->bel).x == plan["x"].int_value() && ctx->getBelLocation(entry.second->bel).y == plan["y"].int_value()) || (ctx->getBelLocation(entry.second->bel).x == ctx->getBelLocation(original).x && ctx->getBelLocation(entry.second->bel).y == ctx->getBelLocation(original).y)) && !ctx->isBelLocationValid(entry.second->bel)) { log_info("DIAGNOSTIC invalid %s at %s\n", ctx->nameOf(entry.second.get()), ctx->getBelName(entry.second->bel).str(ctx).c_str()); ctx->isBelLocationValid(entry.second->bel, true); valid = false; break; }
            log_info("DIAGNOSTIC candidate %s legal=%d\n", ctx->getBelName(bel).str(ctx).c_str(), int(valid));
            if (valid) legal.insert(bel);
            ctx->unbindBel(bel); ctx->bindBel(original, cell, strength);
            static_cast<ArchCellInfo &>(*cell) = saved_info; ctx->labs = saved_labs;
        }
        auto selected = plan["bel"].string_value();
        if (!selected.empty()) {
            BelId bel; for (auto candidate : legal) if (ctx->getBelName(candidate).str(ctx) == selected) bel = candidate;
            if (!legal.count(bel)) log_error("Requested diagnostic site is not legal.\n");
            ctx->unbindBel(original); ctx->bindBel(bel, cell, strength);
            log_info("DIAGNOSTIC moved %s from %s to %s\n", ctx->nameOf(cell), ctx->getBelName(original).str(ctx).c_str(), selected.c_str());
        }
        ctx->save_fes_pin_maps(false); ctx->archInfoToAttributes();
    }
