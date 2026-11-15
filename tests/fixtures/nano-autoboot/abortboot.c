// SPDX-License-Identifier: GPL-2.0+
/*
 * (C) Copyright 2000
 * Wolfgang Denk, DENX Software Engineering, wd@denx.de.
 */

static int abortboot_single_key(int bootdelay)
{
	int abort = 0;
	unsigned long ts;

	printf("Hit any key to stop autoboot: %2d ", bootdelay);

	/*
	 * Check if key already pressed
	 */
	if (tstc()) {	/* we got a key press	*/
		getchar();	/* consume input	*/
		puts("\b\b\b 0");
		abort = 1;	/* don't auto boot	*/
	}

	while ((bootdelay > 0) && (!abort)) {
		--bootdelay;
		/* delay 1000 ms */
		ts = get_timer(0);
		do {
			if (tstc()) {	/* we got a key press	*/
				int key;

				abort  = 1;	/* don't auto boot	*/
				bootdelay = 0;	/* no more delay	*/
				key = getchar();/* consume input	*/
				if (IS_ENABLED(CONFIG_AUTOBOOT_USE_MENUKEY))
					menukey = key;
				break;
			}
			udelay(10000);
		} while (!abort && get_timer(ts) < 1000);

		printf("\b\b\b%2d ", bootdelay);
	}

	putc('\n');

	return abort;
}
