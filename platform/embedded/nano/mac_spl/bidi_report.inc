/* SPDX-License-Identifier: GPL-2.0-or-later */
#include "mac_rx_diagnostic.h"
static void positive_report_grant(void *ctx,unsigned vmid,unsigned cmd,unsigned ste)
{
    (void)ctx;
    printf("FES_CBL_BIDI GRANT id=%s round=%u vmid=%u previous=%u cmd=%u ste=%08x\n",
           FES_CPU_DIAG_ID,vmid,vmid,vmid-1,cmd,ste);
}
static void positive_hex(const unsigned char *bytes,unsigned size)
{
    for (unsigned i=0;i<size;i++) printf("%02x",bytes[i]);
}
static void positive_both_rounds(const struct fes_tx_io *io)
{
    struct fes_rx_result rx[2]={0};
    struct fes_tx_result tx[2]={0};
    const unsigned char *const frames[]={fes_positive_frame,fes_second_frame};
    for (unsigned i=0;i<2;i++) {
        unsigned round=i+1;
        tx[i].rx=&rx[i];
        tx[i].previous=i ? &tx[0] : NULL;
        printf("FES_CBL_BIDI BEGIN id=%s round=%u vmid=%u nonce=",FES_CPU_DIAG_ID,round,round);
        positive_hex(frames[i]+28,16);
        printf("\n");
        int rc=fes_tx_once(io,frames[i],&tx[i]);
        enum fes_tx_disposition closed=fes_tx_terminal(io,&tx[i],rc);
        if (!rc && closed!=FES_TX_CLOSED) rc=-1;
        positive_end(&tx[i],rc,closed);
        printf("FES_CBL_BIDI RX id=%s round=%u possible=%u armed=%u matched=%u stopped=%u retained=%u failure=%u desc=%08x channel=%08x mac=%08x mtl=%08x dma=%08x ch=%08x dwell_us=%llu reply=",
            FES_CPU_DIAG_ID,round,rx[i].dma_possible,rx[i].armed,rx[i].matched,rx[i].stopped,tx[i].retained,
            (unsigned)rx[i].failure,rx[i].desc_status,rx[i].channel_status,rx[i].mac_debug,rx[i].mtl_debug,
            rx[i].dma_debug,rx[i].ch_debug,(unsigned long long)tx[i].dwell_us);
        if (rx[i].matched) positive_hex(rx[i].bytes,FES_RX_BYTES); else printf("-");
        printf("\n");
        int passed=!rc && closed==FES_TX_CLOSED && rx[i].matched && rx[i].stopped && tx[i].retained;
        printf("FES_CBL_BIDI ROUND id=%s round=%u vmid=%u cmd=%u success=%d\n",FES_CPU_DIAG_ID,round,round,tx[i].command,passed);
        if (!passed) break;
    }
}
