/* SPDX-License-Identifier: BSD-3-Clause */
/* Compiled only in the dedicated two-grant SPL. Normal TX has no RX leaf. */
static int rx_guard(void *arg)
{
    return guard(arg);
}

static int held_abort(void *arg)
{
    struct trial *t = arg;
    return guard(t) || read_reg(t, RST) != 0xff7fbfffU ||
        read_reg(t, SMMU+0x98) != t->prod || read_reg(t, SMMU+0x9c) != t->prod;
}

static int retained_after_reset(struct trial *t)
{
    const struct fes_rx_path path = {t->io,t,held_abort};
    if (t->expected_ste!=1 || held_abort(t) || fes_rx_retained(&path,t->out->rx)) return -1;
    uint64_t start = t->io->time_us(t->io->ctx);
    for (unsigned i=0; i<1000000; i++) {
        if (held_abort(t)) return -1;
        uint64_t elapsed=t->out->last_us-start;
        if (elapsed>=10000) {
            if (fes_rx_retained(&path,t->out->rx)) return -1;
            t->out->dwell_us=elapsed;
            t->out->retained=1;
            return 0;
        }
    }
    return -1;
}

/* The SMMU remains enabled throughout. Reuse the private pages only after
 * observing the previous round's abort STE, completed commands, hard reset
 * and unchanged receive memory while that grant is already retired. */
static int regrant(struct trial *t,const uint8_t frame[60])
{
    struct fes_tx_result *old=t->out->previous;
    if (!old || old->vmid!=1 || old->stage!=FES_TX_PARKED || old->consumed ||
        old->scope_lost || !old->mac_in_reset || !old->translation_revoked ||
        !old->process_stopped || !old->retained || old->dwell_us<10000 ||
        !old->rx || !old->rx->matched || !old->rx->stopped || !old->rx->stop.shared || old->rx->failure ||
        old->command!=12) return -1;
    unsigned different=0;
    for (unsigned i=0; i<16; i++) different|=old->nonce[i]^frame[28+i];
    if (!different) return -1;
    t->prod=old->command;
    t->expected_ste=1;
    t->inherited=1;
    const struct fes_rx_path prior={t->io,t,held_abort};
    if (held_abort(t) || fes_rx_retained(&prior,old->rx)) return -1;
    /* Audit the entire stream/L1/L2/L3 tables before accepting this
     * same-boot handoff; no other SID or address may have acquired a leaf. */
    for (uint32_t address=FES_TX_TABLES; address<S2_LEAF+PAGE; address+=8) {
        if (!(address&255) && held_abort(t)) return -1;
        uint64_t expected=address==SID1_STE ? 1U : table_word(address,1);
        if (t->io->load(t->io->ctx,address)!=expected) return -1;
    }
    if (held_abort(t)) return -1;
    old->consumed=1;
    t->out->stage=FES_TX_ARM;
    t->out->arm_attempted=1;
    t->out->write_attempted=1;
    if (store(t,SID1_STE,0)) return -1;
    t->expected_ste=0;
    if (command_sync(t,3U|(1ULL<<32),1) || command_sync(t,0x28U|(2ULL<<32),0) ||
        init_packets(t,frame) || guard(t) ||
        store(t,SID1_STE+16,table_word(SID1_STE+16,2))) return -1;
    t->vmid=2;
    if (guard(t) || store(t,SID1_STE,13)) return -1;
    t->expected_ste=13;
    return command_sync(t,3U|(1ULL<<32),1);
}

static int bidi_transfer(struct trial *t,const uint8_t request[60])
{
    struct fes_tx_result *tx=t->out;
    struct fes_rx_result *rx=tx->rx;
    const struct fes_rx_path path={t->io,t,rx_guard};
    /* The receiver's bounded wait also fits before the cleanup reserve. */
    if (guard(t) || tx->last_us-tx->started_us >=
        MAX_US-TX_COMPLETION_US-2000000U-CLEANUP_RESERVE_US ||
        fes_rx_arm(&path,rx,request)) return -1;
    int tx_failed=send_frame(t);
    if (tx_failed) {
        tx->fault_address=tx->last_address;
        tx->fault_value=tx->last_value;
    }
    if (!tx->dispatched || guard(t)) return -1;
    int rx_failed=fes_rx_wait(&path,rx);
    if (rx_failed && !tx_failed) {
        tx->fault_address=FES_RX_DESC+12;
        tx->fault_value=rx->desc_status;
    }
    /* Stop TX while RX is still active. After closing ingress and RX DMA,
     * prove both channel FSMs and both shared AXI channels are idle. */
    if (stop_mac(t,0xf000) || fes_rx_stop(&path,rx) || sample_tx_drain(t,0xff03))
        return -1;
    rx->stop.shared=1;
    return tx_failed || rx_failed ? -1 : 0;
}
