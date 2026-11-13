static int capability_read(void *context,uint32_t address,uint32_t *value)
{
    unsigned i;
    for (i=0;i<FES_NANO_CAPABILITY_WORDS;i++)
        if (address==fes_nano_capability_address(i)) break;
    if (i==FES_NANO_CAPABILITY_WORDS || smmu_owns(context)!=1) return -1;
    dsbsy();*value=mmio_read_32(address);dsbsy();
    return smmu_owns(context)==1 ? 0 : -1;
}
static int capability_report(struct fes_nano_cold *s)
{
    static unsigned reported;
    static int result=-1;
    static struct fes_nano_capability_probe probe;
    struct fes_nano_capability_snapshot v={0};
    if (reported) return result;
    reported=1;
    probe.io=(struct fes_nano_capability_io){s,smmu_owns,capability_read};
    result=fes_nano_capability_observe(&probe,&v);
    NOTICE("FES_NANO_CAPABILITY status=%d reads=%u owner=%llx epoch=%llx idr0=%x idr1=%x idr3=%x idr5=%x cr1=%x cr2=%x sdm_ctrl=%x sdm_ids=%x sdm_ar=%x sdm_aw=%x por1=%x mac_ctrl=%x mac_ids=%x access=unqualified\n",
        result,probe.reads,(unsigned long long)s->record.boot.owner,
        (unsigned long long)s->record.boot.epoch,
        v.word[0],v.word[1],v.word[2],v.word[3],v.word[4],v.word[5],
        v.word[6],v.word[7],v.word[8],v.word[9],v.word[10],v.word[11],v.word[12]);
    return result;
}
