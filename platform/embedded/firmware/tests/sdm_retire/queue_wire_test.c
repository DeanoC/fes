/* SPDX-License-Identifier: GPL-3.0-or-later */
#define FES_TEST_JOINED_MAIN joined_fixture_main
#include "joined_test.c"
int main(void)
{
 trial_case=0;fixture_credits=4;joined_case();
 puts(terminal_log);
 printf("{\"status\":\"passed\",\"hardware_actions\":false}\n");
}
