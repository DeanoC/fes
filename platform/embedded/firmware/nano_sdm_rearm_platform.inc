/* SPDX-License-Identifier: BSD-3-Clause */
/* A generation handoff under the same owned SiP lock. Only the actual released
 * source and retired private engines admit a new slot. No clock, failed engine,
 * hardware queue or abort state is reset to make another attempt possible. */
#if defined(FES_NANO_SDM_SOURCE_RELEASE) && FES_NANO_SDM_SOURCE_RELEASE == 1
static int sdm_source_secure_scope(void)
{
 const uint32_t rows[][2]={{0x8004,0x80000018},{0x8020,0},{0x8024,0},{0x803c,0},{0x8044,0},{0x8048,0}};
 for(unsigned n=0;n<sizeof(rows)/sizeof(rows[0]);n++){
  FES_PROBE_BARRIER();uint32_t value=FES_PROBE_READ32(UINT64_C(0x16000000)+rows[n][0]);FES_PROBE_BARRIER();
  if(value!=rows[n][1])return 0;
 }
 return 1;
}
static int sdm_rearm_previous(unsigned slot)
{
 if(slot>=2)return 0;
 const struct nano_probe_setup *p=&sdm_arm_probes[slot];
 const struct nano_retire *r=&source_retirements[slot];
 const struct nano_retire_io *i=&r->io;
 return !sdm_source_failed && sdm_release.attempted==1 && sdm_release.released==1 &&
  !sdm_release.failed && !sdm_release.active &&
  nano_sdm_input_same(&sdm_release.binding,&p->input) && nano_sdm_input_same(&p->input,&r->binding) &&
  fes_nano_sdm_transfer_source_released(&p->input)==1 &&
  p->attempted==1 && p->initialized==1 && p->prepared==1 && p->armed==1 && !p->failed && !p->first_error &&
  p->io.context==p && p->io.owns==sdm_input_owns && p->io.time_us==sdm_source_time &&
  p->io.read32==sdm_probe_read && p->io.write32==sdm_probe_write && p->io.clear==sdm_probe_clear &&
  p->io.store64==sdm_probe_store && p->io.load64==sdm_probe_load &&
  p->io.store_block==sdm_probe_store_block && p->io.load_block==sdm_probe_load_block && p->io.barrier==sdm_probe_barrier &&
  p->input_selected==1 && p->input_scope==sdm_input_source &&
  r->attempted==1 && r->retired==1 && r->continued==1 && r->continuation_attempted==1 &&
  r->phase==RET_COMPLETE && !r->failed && !r->first_error &&
  i->context==r && i->owns==retire_owned && i->source_valid==retire_valid && i->time_us==retire_time &&
  i->read32==retire_read && i->write32==retire_write && i->store_command==retire_store && i->barrier==retire_barrier &&
  i->prepared==&p->command && i->initial_abort==p->abort && r->prepared_engine==&p->command &&
  !memcmp(&p->command,&r->prepared_snapshot,sizeof(p->command)) &&
  !r->commands.failed && !r->commands.first_error && r->commands.stage==CMDQ_COMPLETE &&
  r->command_receipt.commands_completed==1 && r->queue_target<=15 &&
  r->command_receipt.producer==r->queue_target && r->command_receipt.consumer==r->queue_target;
}
static int sdm_source_rearm_returned(uint64_t owner,uint64_t generation)
{
 return sdm_rearm.admitted==1 && sdm_rearm.generation==generation &&
  configuration_return_generation(owner,generation)==1;
}
static int sdm_source_rearm_begin(void)
{
 const unsigned old=sdm_source_slot,next=1-old;
 const struct nano_retire *r=&source_retirements[old];
 const struct nano_probe_setup *p=&sdm_arm_probes[next];
 const struct nano_retire *spare=&source_retirements[next];
 if(sdm_source_released!=1 || sdm_input_phase!=2 || !sdm_rearm_previous(old) ||
    configuration.source_release_attempted!=1 || configuration.source_released!=1 ||
    configuration.source_release_source!=r->binding.generation || configuration.source_release_generation!=sdm_release.target ||
    configuration.generation!=sdm_release.target || configuration.rearmed!=1 ||
    configuration.owner!=r->binding.owner || configuration.failed || !configuration.prepared ||
    !configuration.started || configuration.finished || configuration.issued!=FES_NANO_CONFIG_START ||
    configuration.stage!=FES_CONFIG_START || configuration.completed || configuration.pending ||
    configuration.generation<=r->binding.generation || configuration.generation>UINT64_MAX-2 ||
    !configuration_return_generation(configuration.owner,configuration.generation) ||
    p->failed || p->first_error || spare->failed || spare->first_error ||
    (p->attempted && (!p->armed || !spare->retired || !spare->continued ||
     spare->binding.generation>=r->binding.generation)) ||
    fes_nano_sdm_vendor_source_idle(&r->binding)!=1 ||
    !fes_nano_sdm_control_mailbox_idle() || !sdm_rearm_previous(old) ||
    fes_nano_sdm_vendor_source_idle(&r->binding)!=1)return 0;
 /* The preceding engine remains at its real address. Reuse only the older
  * successful slot, after the latest completion and release were consumed. */
 sdm_rearm.previous_slot=old;sdm_rearm.previous_generation=r->binding.generation;
 sdm_rearm.generation=configuration.generation;sdm_rearm.returned=fabric_generation+1;sdm_rearm.admitted=1;
 sdm_source_slot=next;memset(&sdm_arm_probe,0,sizeof(sdm_arm_probe));memset(&source_retirement,0,sizeof(source_retirement));
 sdm_source_released=0;sdm_input_phase=0;sdm_source_clock_started=0;
 return 1;
}
static const struct nano_retire *sdm_source_previous_retirement(void)
{return &source_retirements[sdm_rearm.previous_slot];}
static int sdm_source_rearm_scope(void *context,const struct nano_retire *previous,const struct nano_sdm_input_binding *input)
{
 return context==&sdm_arm_probe && previous==sdm_source_previous_retirement() && sdm_rearm.admitted==1 &&
  input && input->generation==sdm_rearm.generation && previous->binding.generation==sdm_rearm.previous_generation &&
  sdm_release.target==input->generation && sdm_rearm_previous(sdm_rearm.previous_slot) &&
  sdm_source_rearm_returned(input->owner,input->generation) &&
  fes_nano_sdm_vendor_source_idle(&previous->binding)==1 &&
  fes_nano_sdm_control_mailbox_idle()==1;
}
static void sdm_source_rearm_prepared(void)
{
 /* The last warm guard already consumed the actual old release. A failed
  * preparation never reaches this point or makes either slot available. */
 memset(&sdm_release,0,sizeof(sdm_release));
}
#else
static int sdm_source_secure_scope(void){return 0;}
static int sdm_source_rearm_begin(void){return 0;}
static int sdm_source_rearm_returned(uint64_t owner,uint64_t generation)
{(void)owner;(void)generation;return 0;}
static const struct nano_retire *sdm_source_previous_retirement(void){return 0;}
static int sdm_source_rearm_scope(void *context,const struct nano_retire *previous,const struct nano_sdm_input_binding *input)
{(void)context;(void)previous;(void)input;return 0;}
static void sdm_source_rearm_prepared(void){}
#endif
