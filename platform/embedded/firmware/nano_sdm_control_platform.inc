/* SPDX-License-Identifier: BSD-3-Clause */
/* A control reply is a new owned operation after native completion. It does
 * not extend the expired transfer/retirement clocks, reopen either engine,
 * release storage or authorize an FPGA DATA operation. */
static int retained_control_owned(const struct nano_sdm_input_binding *binding)
{
 const struct nano_retire *r=&source_retirement;
 const struct nano_retire_io *i=&r->io;
 return binding && !nano_sdm_input_validate(binding) &&
  nano_sdm_input_same(binding,&sdm_arm_probe.input) && nano_sdm_input_same(binding,&r->binding) &&
  !sdm_source_failed && sdm_input_phase==2 && sdm_input_callbacks() &&
  r->attempted==1 && r->retired==1 && r->continued==1 && r->continuation_attempted==1 &&
  !r->failed && !r->first_error && r->phase==RET_COMPLETE &&
  !r->commands.failed && r->command_receipt.commands_completed==1 &&
  r->command_receipt.producer==12 && r->command_receipt.consumer==12 &&
  i->context==r && i->owns==retire_owned && i->source_valid==retire_valid &&
  i->time_us==retire_time && i->read32==retire_read && i->write32==retire_write &&
  i->store_command==retire_store && i->barrier==retire_barrier &&
  sdm_input_source(&sdm_arm_probe,&sdm_arm_probe.input)==1 &&
  sdm_probe_owns_mode(&sdm_arm_probe,&sdm_arm_probe.binding,1)==1;
}
static int retained_control_view(const struct nano_sdm_input_binding *binding)
{
 static const uint32_t rows[][2]={
  {0,0x080f7e3f},{4,0x0e739d18},{0x14,0x00400075},{0x20,0},{0x24,0},
  {0x28,0x820},{0x2c,4},{0x44,0x100000},{0x48,0},{0x50,0},{0x54,0},{0x60,0},{0x64,0},
  {0x80,0x94000000},{0x84,0},{0x88,8},{0x90,0x94008003},{0x94,0},
  {0x98,12},{0x9c,12},{0xa0,0x94009003},{0xa4,0},{0x100a8,0},{0x100ac,0}
 };
 if(!retained_control_owned(binding))return 0;
 for(unsigned n=0;n<sizeof(rows)/sizeof(rows[0]);n++){
  FES_PROBE_BARRIER();uint32_t value=FES_PROBE_READ32(UINT64_C(0x16000000)+rows[n][0]);FES_PROBE_BARRIER();
  if(value!=rows[n][1])return 0;
 }
 return retained_control_owned(binding);
}
int fes_nano_sdm_source_control_ready(const struct nano_sdm_input_binding *binding,unsigned tables)
{
 if(tables>1 || !retained_control_view(binding))return 0;
 if(tables){
  uint64_t expected[NANO_PROBE_MEMORY_WORDS];
  for(uint32_t page=0;page<0xc000;page+=NANO_RETIRE_TABLE_GUARD_BYTES){
   if(!nano_sdm_input_table_range(page,NANO_RETIRE_TABLE_GUARD_BYTES))continue;
   if(!retained_control_owned(binding))return 0;
   for(uint32_t at=page;at<page+NANO_RETIRE_TABLE_GUARD_BYTES;at+=NANO_PROBE_MEMORY_WORDS*8){
    if(nano_sdm_input_words(binding,at,expected,NANO_PROBE_MEMORY_WORDS))return 0;
    for(unsigned n=0;n<NANO_PROBE_MEMORY_WORDS;n++){
     FES_PROBE_BARRIER();uint64_t value=FES_PROBE_LOAD64(NANO_PROBE_BASE+at+n*8);FES_PROBE_BARRIER();
     if(value!=expected[n])return 0;
    }
   }
   if(!retained_control_owned(binding))return 0;
  }
 }
 return retained_control_view(binding);
}
