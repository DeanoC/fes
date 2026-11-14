/* SPDX-License-Identifier: BSD-3-Clause */
/* Included with the actual retained engines, under the existing SiP lock.
 * This supplier never writes a table/register, clears an error or opens DATA.
 * Selection stays disabled until the fresh-generation rearm path is complete. */
#if defined(FES_NANO_SDM_SOURCE_RELEASE) && FES_NANO_SDM_SOURCE_RELEASE == 1
static struct {
 struct nano_sdm_input_binding binding;
 uint64_t target,start,last;
 unsigned attempted,active,failed,released;
} sdm_release;
static int sdm_release_clock(void)
{
 if(!sdm_release.attempted || !sdm_release.active || sdm_release.failed ||
    sdm_release.released || sdm_source_failed || sdm_source_released ||
    !FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U ||
    !nano_sdm_input_same(&sdm_release.binding,&sdm_arm_probe.input) ||
    configuration.owner!=sdm_release.binding.owner ||
    configuration.generation!=sdm_release.binding.generation ||
    configuration.source_release_attempted!=1 || configuration.source_released ||
    configuration.source_release_source!=sdm_release.binding.generation ||
    configuration.source_release_generation!=sdm_release.target)return -13;
 uint64_t now=FES_PROBE_COUNTER()/400U;
 if(now<sdm_release.last || now-sdm_release.start>=NANO_RETIRE_US)return -110;
 sdm_release.last=now;return 0;
}
static int release_observer_owns(void *context)
{
 return context==&sdm_arm_probe.input && retained_source_owned(&sdm_arm_probe.input,2)==1;
}
static int release_observer_read(void *context,uint32_t offset,uint32_t *out)
{
 if(!out || !fes_nano_smmu_offset(offset) || release_observer_owns(context)!=1)return -1;
 FES_PROBE_BARRIER();*out=FES_PROBE_READ32(UINT64_C(0x16000000)+offset);FES_PROBE_BARRIER();
 return release_observer_owns(context)==1 ? 0:-1;
}
int fes_nano_sdm_source_release_ready(const struct nano_sdm_input_binding *binding,unsigned tables)
{
 struct fes_nano_smmu_observation observed={0};
 struct fes_nano_smmu_probe probe={.io={&sdm_arm_probe.input,release_observer_owns,release_observer_read}};
 return binding && tables<=1 && nano_sdm_input_same(binding,&sdm_release.binding) &&
  retained_source_ready(binding,tables,2)==1 &&
  !fes_nano_smmu_observe(&probe,&observed) && nano_sdm_reader_observed(&observed)==1 &&
  release_observer_owns(&sdm_arm_probe.input)==1;
}
static int source_release_stop(void)
{
 sdm_release.failed=1;sdm_release.active=0;sdm_source_stop(35);return -13;
}
int fes_nano_platform_release_source(struct fes_nano_configuration *c,uint64_t owner,uint64_t generation)
{
 if(c!=&configuration || sdm_release.attempted)return -13;
 sdm_release.attempted=1;sdm_release.binding=sdm_arm_probe.input;sdm_release.target=generation;
 if(!owner || owner!=sdm_release.binding.owner || generation<=sdm_release.binding.generation ||
    generation>UINT64_MAX-2 || !FES_PROBE_CPU() || FES_PROBE_FREQUENCY()!=400000000U ||
    nano_sdm_input_validate(&sdm_release.binding))return source_release_stop();
 /* A new owned operation, not an extension of the original transfer or
  * retirement lifetime. Long-running sessions do not expire this clock. */
 sdm_release.start=sdm_release.last=FES_PROBE_COUNTER()/400U;sdm_release.active=1;
 if(sdm_release_clock() ||
    fes_nano_sdm_transfer_release_source(&sdm_release.binding) || sdm_release_clock() ||
    fes_nano_sdm_transfer_source_released(&sdm_release.binding)!=1)return source_release_stop();
 sdm_release.released=1;sdm_release.active=0;sdm_source_released=1;
 NOTICE("FES_NANO_SOURCE_RELEASE status=0 owner=%llx source_generation=%llu generation=%llu reuse_authorized=1 elapsed_us=%llu\n",
  (unsigned long long)owner,(unsigned long long)sdm_release.binding.generation,
  (unsigned long long)generation,(unsigned long long)(sdm_release.last-sdm_release.start));
 return 1;
}
#else
static int sdm_release_clock(void){return -13;}
int fes_nano_sdm_source_release_ready(const struct nano_sdm_input_binding *binding,unsigned tables)
{(void)binding;(void)tables;return 0;}
int fes_nano_platform_release_source(struct fes_nano_configuration *c,uint64_t owner,uint64_t generation)
{(void)c;(void)owner;(void)generation;return 0;}
#endif
