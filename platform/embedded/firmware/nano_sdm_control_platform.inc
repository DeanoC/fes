/* SPDX-License-Identifier: BSD-3-Clause */
/* A control reply is a new owned operation after native completion. It does
 * not extend the expired transfer/retirement clocks, reopen either engine,
 * release storage or authorize an FPGA DATA operation. */
static int retained_source_owned(const struct nano_sdm_input_binding *binding,unsigned reader)
{
 const struct nano_retire *r=&source_retirement;
 const struct nano_retire_io *i=&r->io;
 return reader<=2 && binding && !nano_sdm_input_validate(binding) &&
  (reader!=2 || !sdm_release_clock()) &&
  nano_sdm_input_same(binding,&sdm_arm_probe.input) && nano_sdm_input_same(binding,&r->binding) &&
  !sdm_source_failed && sdm_input_phase==2 && sdm_input_callbacks() &&
  r->attempted==1 && r->retired==1 && r->continued==1 && r->continuation_attempted==1 &&
  !r->failed && !r->first_error && r->phase==RET_COMPLETE &&
  !r->commands.failed && r->command_receipt.commands_completed==1 &&
  r->command_receipt.baseline==r->queue_baseline &&
  r->command_receipt.producer==r->queue_target && r->command_receipt.consumer==r->queue_target &&
  i->context==r && i->owns==retire_owned && i->source_valid==retire_valid &&
  i->time_us==retire_time && i->read32==retire_read && i->write32==retire_write &&
  i->store_command==retire_store && i->barrier==retire_barrier &&
  i->prepared==&sdm_arm_probe.command && i->initial_abort==sdm_arm_probe.abort && r->prepared_engine==i->prepared &&
  !memcmp(i->prepared,&r->prepared_snapshot,sizeof(r->prepared_snapshot)) &&
  sdm_input_source(&sdm_arm_probe,&sdm_arm_probe.input)==1 &&
  sdm_probe_owns_mode(&sdm_arm_probe,&sdm_arm_probe.binding,reader==2 ? 3:reader ? 2:1)==1;
}
static int retained_source_view(const struct nano_sdm_input_binding *binding,unsigned reader)
{
 return retained_source_owned(binding,reader);
}
static int retained_source_ready(const struct nano_sdm_input_binding *binding,unsigned tables,unsigned reader)
{
 return tables<=1 && retained_source_view(binding,reader);
}

int fes_nano_sdm_source_control_ready(const struct nano_sdm_input_binding *binding,unsigned tables)
{return retained_source_ready(binding,tables,0);}
