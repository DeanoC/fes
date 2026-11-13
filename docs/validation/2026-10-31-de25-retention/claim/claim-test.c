// SPDX-License-Identifier: GPL-2.0-only
// Host harness for the actual patched function, not a replacement driver.
#include <assert.h>
#include <limits.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#ifndef CONFIG_FES_DE25_DMA_PLATFORM
#define CONFIG_FES_DE25_DMA_PLATFORM 1
#endif
#define IS_ENABLED(x) (x)
#define FPGA_CONFIG_DATA_CLAIM_TIMEOUT_MS 2000
#define INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE 7
#define INTEL_SIP_SMC_STATUS_OK 0
#define INTEL_SIP_SMC_STATUS_BUSY 1
#define SVC_STATUS_BUFFER_DONE 2
#define SVC_STATUS_ERROR 3
#define complete(p) ((void)(p))
#define reinit_completion(p) ((void)(p))
#define wait_for_completion_timeout(p,t) ((void)(p), jiffies += (t), 0)
#define BIT(n) (1UL << (n))
#define pr_debug(...) ((void)0)
#define pr_err(...) ((void)0)
#define msecs_to_jiffies(n) ((unsigned long)(n))
#define time_before(a,b) ((long)((a)-(b)) < 0)
struct arm_smccc_res { unsigned long a0, a1, a2, a3; };
struct stratix10_svc_cb_data { unsigned long status; void *kaddr1, *kaddr2, *kaddr3, *kaddr4; };
struct client { void (*receive_cb)(struct client *, struct stratix10_svc_cb_data *); };
struct channel { struct client *scl; };
struct stratix10_svc_data { struct channel *chan; };
struct stratix10_svc_controller {
    int complete_status;
    void (*invoke_fn)(unsigned long, unsigned long, unsigned long, unsigned long,
                      unsigned long, unsigned long, unsigned long, unsigned long,
                      struct arm_smccc_res *);
};
static unsigned long jiffies;
static unsigned int calls, sleeps, callbacks, stop_after;
static bool stopped;
static struct stratix10_svc_cb_data reply;
static struct arm_smccc_res sequence[4004];
static unsigned int sequence_count;
static inline bool kthread_should_stop(void) { return stopped || calls >= stop_after; }
static inline void usleep_range(unsigned int low, unsigned int high)
{
    assert(low == 1000 && high == 2000);
    ++sleeps;
    ++jiffies;
}
static void *svc_pa_to_va(unsigned long address) { return (void *)(uintptr_t)(address + 0x1000); }
static void callback(struct client *client, struct stratix10_svc_cb_data *data)
{
    (void)client;
    ++callbacks;
    reply = *data;
}
static void invoke(unsigned long command, unsigned long a1, unsigned long a2,
                   unsigned long a3, unsigned long a4, unsigned long a5,
                   unsigned long a6, unsigned long a7, struct arm_smccc_res *res)
{
    assert(command == INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE);
    assert(!(a1 | a2 | a3 | a4 | a5 | a6 | a7));
    unsigned int index = calls < sequence_count ? calls : sequence_count - 1;
    *res = sequence[index];
    ++calls;
}
#include "claim-function.c"
static void run_case(const char *name, unsigned long initial_time, unsigned int mode)
{
    calls = sleeps = callbacks = 0;
    stopped = false;
    stop_after = UINT_MAX;
    jiffies = initial_time;
    memset(&reply, 0xa5, sizeof(reply));
    memset(sequence, 0, sizeof(sequence));
    sequence_count = 1;
    sequence[0].a0 = INTEL_SIP_SMC_STATUS_BUSY;
    if (mode == 1) sequence[0].a0 = 5;
    if (mode == 2) stopped = true;
    if (mode == 3) stop_after = 3;
    if (mode == 4) {
        sequence_count = 3;
        sequence[0] = (struct arm_smccc_res){0, 1, 2, 3};
        sequence[1] = (struct arm_smccc_res){0, 4, 0, 0};
        sequence[2] = (struct arm_smccc_res){0, 0, 0, 0};
    }
    if (mode == 6) {
        sequence_count = 3802;
        for (unsigned int i = 0; i < sequence_count; ++i)
            sequence[i].a0 = INTEL_SIP_SMC_STATUS_BUSY;
        sequence[1900] = (struct arm_smccc_res){0, 1, 2, 3};
        sequence[3800] = (struct arm_smccc_res){0, 4, 0, 0};
        sequence[3801] = (struct arm_smccc_res){0, 0, 0, 0};
    }
    if (mode == 7) {
        sequence[0] = (struct arm_smccc_res){0, 0, 0, 0};
        stop_after = 1;
    }
    if (mode == 5) sequence[0] = (struct arm_smccc_res){0, 1, 2, 3};
    struct client client = {callback};
    struct channel chan = {&client};
    struct stratix10_svc_data data = {&chan};
    struct stratix10_svc_controller ctrl = {.invoke_fn=invoke};
    struct stratix10_svc_cb_data result;
    memset(&result, 0xa5, sizeof(result));
    svc_thread_cmd_data_claim(&ctrl, &data, &result);
    if (mode == 2 || mode == 3 || mode == 7) {
        assert(callbacks == 0);
        assert(calls == (mode == 2 ? 0 : (mode == 7 ? 1 : 3)));
    } else if (mode == 4 || mode == 6) {
        assert(callbacks == 1 && calls == (mode == 4 ? 3U : 3802U));
        assert(reply.status == BIT(SVC_STATUS_BUFFER_DONE));
        assert(reply.kaddr1 == svc_pa_to_va(1) && reply.kaddr2 == svc_pa_to_va(2));
        assert(reply.kaddr3 == svc_pa_to_va(3) && reply.kaddr4 == svc_pa_to_va(4));
    } else {
        assert(callbacks == 1 && reply.status == BIT(SVC_STATUS_ERROR));
        assert(!reply.kaddr1 && !reply.kaddr2 && !reply.kaddr3 && !reply.kaddr4);
        assert(calls <= 2002);
        if (mode == 1) assert(calls == 1 && sleeps == 0);
        else assert(sleeps >= 2000);
    }
    puts(name);
}
int main(void)
{
    if (!IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM)) {
        run_case("disabled profile preserves four-buffer success", 0, 4);
        return 0;
    }
    run_case("busy firmware reaches bounded error", 0, 0);
    run_case("firmware error clears all stale callback pointers", 0, 1);
    run_case("already stopped client produces no SMC or callback", 0, 2);
    run_case("stop during busy polling produces no late callback", 0, 3);
    run_case("four buffers across two replies preserve success", 0, 4);
    run_case("repeated OK replies without progress terminate", 0, 5);
    run_case("deadline survives jiffies wrap", ULONG_MAX - 100, 0);
    run_case("real buffer progress refreshes only the inactivity deadline", 0, 6);
    run_case("stop after SMC suppresses even a successful late callback", 0, 7);
    return 0;
}
