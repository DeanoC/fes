#if !defined(FES_NANO_RETIRE_TIMING) || FES_NANO_RETIRE_TIMING != 1
#error Explicit_retirement_timing_profile_required
#endif
#if !defined(FES_NANO_RETIRE_QUEUE_OBSERVE) || FES_NANO_RETIRE_QUEUE_OBSERVE != 1
#error Explicit_retirement_queue_observation_profile_required
#endif
#if !defined(FES_NANO_SDM_RETIRE_SOURCE) || FES_NANO_SDM_RETIRE_SOURCE != 1
#error Explicit_source_retirement_profile_required
#endif
/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_sdm_retire_platform.inc"
#ifndef FES_NANO_INPUT_BUILD
#error Explicit_sealed_input_transfer_build_required
#endif
/* A separately visible boundary makes the final clock/owner order inspectable
 * in the final ELF. It observes only; it never acknowledges an event. */
__attribute__((noinline)) static int sdm_input_final_scope(void)
{return sdm_source_owns(&sdm_arm_probe,&sdm_arm_probe.input)==1;}
/* Always called under the existing owned dispatcher lock. No success reaches
 * configuration_after/activation, and no failure retries or releases memory. */
__attribute__((noreturn)) void fes_nano_sdm_source_finish(uint32_t status,uint32_t detail)
{
 struct nano_sdm_transfer_result r;fes_nano_sdm_transfer_result(&r);
 uint64_t final_now=0;unsigned proof=0;
 if(status==0 && detail==0 && !nano_sdm_input_validate(&r.binding) && r.prepared==1 && r.initialized==1 && !r.failed && r.configured==1 &&
    !r.first_error && !r.service_error && !r.pending && !r.outstanding &&
    r.accepted==r.binding.bytes && r.posted==r.binding.bytes && r.acknowledged==r.binding.bytes &&
    r.completed==r.binding.bytes && r.posts && r.posts==r.intents && r.acks==r.posts &&
    r.buffers && r.buffers==(r.binding.bytes+r.binding.chunk-1U)/r.binding.chunk &&
    r.status_polls && r.headers==r.posts+r.status_polls && r.start==sdm_source_clock_start && r.last>=r.start &&
    nano_sdm_input_same(&r.binding,&sdm_arm_probe.input) &&
    configuration.issued==FES_NANO_CONFIG_ISDONE && configuration.stage==FES_CONFIG_STATUS &&
    configuration.pending==0 && configuration.completed==configuration.bytes &&
    !fes_nano_sdm_source_validate() && !sdm_source_time(&sdm_arm_probe,&final_now) && sdm_input_final_scope())proof=1;
 if(!proof && status==0)status=4;
 char digest[97];static const char hex[]="0123456789abcdef";
 for(unsigned n=0;n<48;n++){digest[n*2]=hex[r.binding.digest384[n]>>4];digest[n*2+1]=hex[r.binding.digest384[n]&15U];}
 digest[96]=0;
 NOTICE("FES_NANO_INPUT_RESULT build=%s status=%u detail=%u proof=%u owner=%llx epoch=%llx generation=%llu bytes=%u accepted=%u posted=%u acknowledged=%u completed=%u posts=%u acks=%u buffers=%u pending=%u outstanding=%u configured=%u failed=%u first_error=%d service_error=%d source_failed=%u scope_reason=%u bad_offset=%x bad_value=%x arm_writes=%u elapsed_us=%llu sha384=%s retained=1 activated=0\n",
  FES_NANO_INPUT_BUILD,status,detail,proof,(unsigned long long)r.binding.owner,(unsigned long long)r.binding.epoch,
  (unsigned long long)r.binding.generation,r.binding.bytes,r.accepted,r.posted,r.acknowledged,r.completed,
  r.posts,r.acks,r.buffers,r.pending,r.outstanding,r.configured,r.failed,r.first_error,r.service_error,
  sdm_source_failed,sdm_probe_scope_reason,sdm_source_bad_offset,sdm_source_bad_value,sdm_arm_probe.writes,
  (unsigned long long)(proof ? final_now-r.start:r.elapsed),digest);
 if(proof){
  struct nano_retire_receipt retired={0};int error=fes_nano_sdm_retire_source(&retired);
  NOTICE("FES_NANO_RETIRE_RESULT build=%s owner=%llx epoch=%llx generation=%llu bytes=%u sha384=%s status=%d phase=%u retired=%u first_error=%d writes=%u producer=%u consumer=%u control=%u abort=%x elapsed_us=%llu bad_offset=%x bad_value=%x retained=1 reuse=0 activated=0\n",
   FES_NANO_INPUT_BUILD,(unsigned long long)retired.owner,(unsigned long long)retired.epoch,
   (unsigned long long)retired.generation,source_retirement.binding.bytes,digest,error,source_retirement.phase,retired.translation_retired,source_retirement.first_error,source_retirement.writes,
   retired.producer,retired.consumer,retired.control,retired.abort,(unsigned long long)retired.elapsed_us,
   source_retirement.bad_offset,source_retirement.bad_value);
  /* Informational timing only; no additional clock, owner or device call. */
  NOTICE("FES_NANO_RETIRE_TIMING build=%s owner=%llx epoch=%llx generation=%llu bytes=%u sha384=%s phase=%u timed=%u mask=%x start_us=%llu last_us=%llu observed_us=%llu guards=%u samples=%u owners=%u sources=%u validated=%u reads=%u writes=%u polls=%u first_us=%llx,%llx,%llx,%llx,%llx,%llx,%llx latest_us=%llx,%llx,%llx,%llx,%llx,%llx,%llx retained=1 reuse=0 activated=0\n",
   FES_NANO_INPUT_BUILD,(unsigned long long)r.binding.owner,(unsigned long long)r.binding.epoch,
   (unsigned long long)r.binding.generation,r.binding.bytes,digest,source_retirement.phase,source_retirement.timed,
   source_retirement.phase_seen,(unsigned long long)source_retirement.start,(unsigned long long)source_retirement.last,
   (unsigned long long)source_retirement.observed,source_retirement.guard_calls,source_retirement.clock_samples,
   source_retirement.owner_checks,source_retirement.source_checks,source_retirement.source_passes,
   source_retirement.reads,source_retirement.writes,source_retirement.polls,
   (unsigned long long)source_retirement.phase_first[0],(unsigned long long)source_retirement.phase_first[1],(unsigned long long)source_retirement.phase_first[2],(unsigned long long)source_retirement.phase_first[3],(unsigned long long)source_retirement.phase_first[4],(unsigned long long)source_retirement.phase_first[5],(unsigned long long)source_retirement.phase_first[6],
   (unsigned long long)source_retirement.phase_last[0],(unsigned long long)source_retirement.phase_last[1],(unsigned long long)source_retirement.phase_last[2],(unsigned long long)source_retirement.phase_last[3],(unsigned long long)source_retirement.phase_last[4],(unsigned long long)source_retirement.phase_last[5],(unsigned long long)source_retirement.phase_last[6]);
  /* Raw observations may precede a failed ownership/time/pointer check. They
   * are intentionally separate from the success-only retirement receipt. */
  struct nano_cmdq *q=&source_retirement.commands;
  NOTICE("FES_NANO_RETIRE_QUEUE build=%s owner=%llx epoch=%llx generation=%llu bytes=%u sha384=%s phase=%u stage=%u attempted=%u initialized=%u submitted=%u failed=%u error=%d timed=%u start_us=%llu last_us=%llu observed_us=%llu samples=%u slots=%u published=%u polls=%u reads=%u read_samples=%u last_offset=%x last_value=%x consumer_samples=%u consumer=%x consumer_read=%u consumer_phase=%u consumer_stage=%u consumer_poll=%u retained=1 reuse=0 activated=0\n",
   FES_NANO_INPUT_BUILD,(unsigned long long)r.binding.owner,(unsigned long long)r.binding.epoch,
   (unsigned long long)r.binding.generation,r.binding.bytes,digest,source_retirement.phase,
   q->stage,q->attempted,q->initialized,q->submitted,q->failed,q->first_error,q->timed,
   (unsigned long long)q->start,(unsigned long long)q->last,(unsigned long long)q->observed,
   q->clock_samples,q->slots_attempted,q->publish_attempted,q->polls,q->reads,
   source_retirement.observed_reads,source_retirement.observed_offset,source_retirement.observed_value,
   source_retirement.consumer_samples,source_retirement.observed_consumer,source_retirement.consumer_read,
   source_retirement.consumer_phase,source_retirement.consumer_stage,source_retirement.consumer_poll);
 }
 NOTICE("FES_NANO_INPUT_PARK retained=1 resume=0 activated=0\n");
 for(;;)FES_PROBE_WAIT();
}
