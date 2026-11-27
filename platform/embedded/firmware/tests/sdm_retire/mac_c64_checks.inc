/* SPDX-License-Identifier: BSD-3-Clause */
static void test_c64_window(void)
{
 unsigned before=fetch_reads;
 if(test_problem==8){
    lifecycle_registers[FES_EMBEDDED_REG_BUILD1/4]^=1;
    assert(!fes_nano_menu_mac_admit() && !fes_nano_c64_mac_scope(0,17,11) && fetch_reads==before);_exit(0);
 }
 if(test_problem==5){configuration_program.header[FES_NANO_PROGRAM_PACKAGE]^=1;
    assert(!fes_nano_menu_mac_admit() && fetch_reads==before && !mac_window.active);_exit(0);}
 assert(fes_nano_menu_mac_admit()==1 && fetch_reads==before);
 assert(fes_nano_c64_mac_scope(0,17,11)==1 && !fes_nano_c64_mac_scope(0,18,11) && fetch_reads==before);
 uint64_t result=invoke(FES_EMBEDDED_MAC_ACQUIRE,6);
 if(test_problem==6){assert(result==FES_EMBEDDED_MAC_ERROR && !mac_window.active && fetch_reads>before);_exit(0);}
 assert(!result && menu_mac.vmid==5 && !mac_window.displayed && fetch_reads>=before+2);
 fes_mac_fixture_driver_start();assert(!invoke(FES_EMBEDDED_MAC_CONFIRM,7));
 before=fetch_reads;
 assert(fes_nano_c64_mac_scope(8,17,11)==1 && fetch_reads==before);
 assert(!fes_nano_c64_mac_scope(5,17,11) && !fes_nano_c64_mac_scope(8,18,11) &&
        !fes_nano_c64_mac_scope(8,17,12) && !fes_nano_owned_mac.failed && fetch_reads==before);
 if(test_problem==7){gp_error=1;assert(fes_nano_c64_mac_scope(8,17,11)<0 &&
        fes_nano_owned_mac.failed && !fes_nano_menu_mac_source_safe() && fetch_reads==before);_exit(0);}
 fes_mac_fixture_driver_stop();assert(!invoke(FES_EMBEDDED_MAC_STOP,8));
 assert(fes_nano_menu_mac_source_safe() && !mac_window.active && !fes_nano_c64_mac_scope(8,17,11) &&
        c64_containment(11,1) && fetch_reads==before && registers[0x98/4]==8);
}
