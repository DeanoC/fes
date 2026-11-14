/* SPDX-License-Identifier: BSD-3-Clause */
/* Trusted terminal path only. No SMMU write/read, source release, queue reset,
 * error acknowledgement, retry or return-to-dispatch interface. */
#define NANO_RECOVERY_SCOPE_US 1000000U
static int sdm_recovery_source(void)
{
 struct nano_sdm_transfer_result r;fes_nano_sdm_transfer_result(&r);
 const struct nano_sdm_input_binding *b=&sdm_arm_probe.input;
 return !nano_sdm_input_validate(b) && nano_sdm_input_same(&r.binding,b) &&
  nano_sdm_input_same(&source_retirement.binding,b) && sdm_input_source(&sdm_arm_probe,b)==1 &&
  r.prepared==1 && r.initialized==1 && !r.failed && r.configured==1 &&
  !r.first_error && !r.service_error && !r.pending && !r.outstanding &&
  r.accepted==b->bytes && r.posted==b->bytes && r.acknowledged==b->bytes && r.completed==b->bytes &&
  r.posts && r.posts==r.intents && r.acks==r.posts && r.buffers &&
  r.buffers==(b->bytes+b->chunk-1U)/b->chunk && r.status_polls && r.headers==r.posts+r.status_polls &&
  r.start==sdm_source_clock_start && r.last>=r.start &&
  source_retirement.attempted==1 && source_retirement.failed==1 &&
  source_retirement.first_error<0 && !source_retirement.continued &&
  !sdm_source_released &&
  ((!configuration.source_release_attempted && !configuration.rearmed && !sdm_rearm.admitted) ||
   (configuration.source_release_attempted==1 && configuration.source_released==1 &&
    configuration.rearmed==1 && sdm_rearm.admitted==1 &&
    configuration.source_release_source==sdm_rearm.previous_generation &&
    configuration.source_release_generation==b->generation && sdm_rearm.generation==b->generation &&
    sdm_rearm.previous_generation<b->generation)) &&
  configuration.initialized && !configuration.failed && configuration.prepared &&
  configuration.started && !configuration.finished && configuration.bridges==&fes_nano_owned_bridges &&
  configuration.issued==FES_NANO_CONFIG_ISDONE && configuration.stage==FES_CONFIG_STATUS &&
  !configuration.pending && configuration.completed==configuration.bytes;
}
static int sdm_recovery_cached(uint64_t owner,uint64_t epoch,uint64_t generation)
{
 const struct fes_nano_bridges *b=&fes_nano_owned_bridges;
 return FES_PROBE_CPU() && FES_PROBE_FREQUENCY()==400000000U &&
  fes_nano_mmu_platform_scope() && sdm_source_slot<2 && sdm_input_phase==2 &&
  provider.valid && !provider.failed && !provider.control_expected && !provider.ddr_expected &&
  owner==provider.record.boot.owner && epoch==provider.record.boot.epoch &&
  owner==sdm_arm_probe.input.owner && epoch==sdm_arm_probe.input.epoch &&
  generation==sdm_arm_probe.input.generation && b->initialized && b->failed==1 && !b->device_lease &&
  b->state.owner==owner && b->epoch==epoch && b->state.generation==generation &&
  b->state.reset==FES_NANO_BRIDGES_ALL && b->state.masters_contained==1 &&
  !sd_platform.failed && sd_platform.initialized && !sd_platform.active && !sd_platform.prepared &&
  FES_PROBE_SD_IO_BINDING_VALID() &&
  usb_platform.initialized && !usb_platform.failed && usb_platform.active<=1 &&
  usb_platform.active==usb_platform.confirmed && (!usb_platform.active || usb_platform.generation==2) &&
  !fes_nano_owned_mac.attempted && !fes_nano_owned_mac.initialized && !fes_nano_owned_mac.failed &&
  fes_nano_usb_master_recovery_binding(&usb_master,b,&usb_platform)==1 && sdm_recovery_source();
}
static int sdm_recovery_scope(void *context,uint64_t owner,uint64_t epoch,uint64_t generation)
{
 uint64_t first=0,last=0;
 if(sdm_source_slot>=2 || context!=&sdm_arm_probe || !sdm_recovery_cached(owner,epoch,generation) ||
    sdm_probe_time(context,&first))return 0;
 /* This provider is independent of the revoked ordinary boot object. Its
  * paired cold samples and USB PIO/clock/DMA/idle observation are live. Bridge
  * reset containment does not claim independent fabric/interconnect drain. */
 if(fes_nano_cold_owns(&provider,owner,epoch)!=1 ||
    !sdm_recovery_cached(owner,epoch,generation) ||
    fes_nano_cold_owns(&provider,owner,epoch)!=1 ||
    sdm_probe_time(context,&last) || last<first || last-first>=NANO_RECOVERY_SCOPE_US ||
    !sdm_recovery_cached(owner,epoch,generation))return 0;
 return 1;
}
__attribute__((noinline)) int fes_nano_sdm_source_recover(void)
{
 if(sdm_source_slot>=2){
  struct fes_nano_reset_ops absent={0};
  return fes_nano_bridge_platform_cold_reset_held(0,0,0,absent);
 }
 struct fes_nano_reset_ops ops={&sdm_arm_probe,sdm_recovery_scope};
 return fes_nano_bridge_platform_cold_reset_held(sdm_arm_probe.input.owner,
  sdm_arm_probe.input.epoch,sdm_arm_probe.input.generation,ops);
}
