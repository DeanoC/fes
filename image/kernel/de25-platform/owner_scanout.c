// SPDX-License-Identifier: GPL-2.0
/* Explicit allocated-buffer diagnostic. Never installed or run by boot. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <sys/utsname.h>
#include <time.h>
#include <unistd.h>
#include "../de25-dma/de25_dma.h"
#include "../de25-dma/generated/fes_de25_scanout.h"

static int fail(const char *stage)
{
	fprintf(stderr, "owner-scanout: %s (errno=%d); no retry or owner REAP after uncertainty\n", stage, errno);
	return 1;
}

static int geometry(const struct fes_de25_dma_info *i)
{
	return i->version == FES_DE25_DMA_ABI_VERSION && i->generation && !i->reserved &&
		(i->build_id[0] | i->build_id[1] | i->build_id[2] | i->build_id[3]) &&
		i->allocation_bytes == FES_DE25_SCANOUT_ALLOCATION_BYTES &&
		i->slot_bytes == FES_DE25_SCANOUT_SLOT_BYTES &&
		i->frame_bytes == FES_DE25_SCANOUT_FRAME_BYTES &&
		i->width == FES_DE25_SCANOUT_FRAME_WIDTH && i->height == FES_DE25_SCANOUT_FRAME_HEIGHT &&
		i->stride == FES_DE25_SCANOUT_FRAME_STRIDE && i->slots == FES_DE25_SCANOUT_SLOT_COUNT &&
		!(i->state & ~(FES_DE25_DMA_EXPOSED | FES_DE25_DMA_PENDING)) &&
		!(i->hardware_status & (~FES_DE25_SCANOUT_STATUS_MASK |
		  FES_DE25_SCANOUT_STATUS_FAULT | FES_DE25_SCANOUT_STATUS_CLOCK_FAULT)) &&
		!i->hardware_errors && !i->underflows;
}

static uint64_t now_ms(void)
{
	struct timespec t;
	if (clock_gettime(CLOCK_MONOTONIC, &t))
		return UINT64_MAX;
	return (uint64_t)t.tv_sec * 1000 + t.tv_nsec / 1000000;
}

static int run_scanout(int owner, bool allocation_only)
{
	const uint32_t colors[] = {0x00ff0000, 0x0000ff00, 0x000000ff};
	struct fes_de25_dma_info info = {0};
	uint64_t generation;
	uint32_t build_id[4];
	int grant, result = 1, flags;
	if (ioctl(owner, FES_DE25_DMA_OWNER_ABI, 0UL) != FES_DE25_DMA_ABI_VERSION)
		return fail("owner ABI before allocation");
	grant = ioctl(owner, FES_DE25_DMA_OWNER_ALLOC, 0UL);
	if (grant < 0)
		return fail("ALLOC");
	flags = fcntl(grant, F_GETFD);
	if (flags < 0 || !(flags & FD_CLOEXEC) ||
	    ioctl(grant, FES_DE25_DMA_INFO, &info) || !geometry(&info) ||
	    info.state || info.outstanding || info.accepted_sequence || info.displayed_sequence || info.displayed_slot != 0 ||
	    info.pending_sequence || info.pending_slot != FES_DE25_DMA_NO_SLOT || info.back_slot != 1) {
		fail("initial grant identity");
		goto out;
	}
	memcpy(build_id, info.build_id, sizeof(build_id));
	printf("DE25_OWNER_BUILD_ID=%08x%08x%08x%08x\n",
		build_id[3], build_id[2], build_id[1], build_id[0]);
	if (allocation_only) {
		/* Unexposed allocation: no mapping, publication, POLL or frame DMA. */
		if (ioctl(owner, FES_DE25_DMA_OWNER_REAP, 0UL)) {
			fail("unexposed owner reap");
			goto out;
		}
		puts("DE25_OWNER_ALLOCATION_ONLY_PASS publication_requested=false allocation_reaped=true");
		result = 0;
		goto out;
	}
	generation = info.generation;
	for (uint32_t sequence = 1; sequence <= 3; ++sequence) {
		uint32_t slot = info.back_slot;
		struct fes_de25_dma_publish publish = {
			.generation = generation, .version = FES_DE25_DMA_ABI_VERSION,
			.sequence = sequence, .slot = slot,
		};
		uint64_t start, deadline;
		uint8_t *pixels;
		if (slot >= FES_DE25_SCANOUT_SLOT_COUNT || slot == info.displayed_slot) {
			fail("back-slot identity");
			goto out;
		}
		pixels = mmap(NULL, info.slot_bytes, PROT_READ | PROT_WRITE, MAP_SHARED,
			grant, (off_t)slot * info.slot_bytes);
		if (pixels == MAP_FAILED) {
			fail("back-slot mapping");
			goto out;
		}
		memset(pixels, 0, info.slot_bytes);
		for (uint32_t y = 0; y < info.height; ++y) {
			uint32_t *row = (uint32_t *)(pixels + (size_t)y * info.stride);
			for (uint32_t x = 0; x < info.width; ++x)
				row[x] = x < 64 && y < 64 ? 0x00ffffff :
					(x >= info.width / 2 && y >= info.height / 2 ?
					 colors[sequence % 3] : colors[sequence - 1]);
		}
		if (munmap(pixels, info.slot_bytes)) {
			fail("back-slot unmap");
			goto out;
		}
		/* The kernel owns DMA addresses, ordering, publication and drain. */
		if (ioctl(grant, FES_DE25_DMA_PUBLISH, &publish)) {
			fail("PUBLISH");
			goto out;
		}
		start = now_ms();
		if (start == UINT64_MAX || start > UINT64_MAX - 5000) {
			fail("monotonic clock");
			goto out;
		}
		deadline = start + 5000;
		for (;;) {
			info = (struct fes_de25_dma_info){.generation = generation,
				.version = FES_DE25_DMA_ABI_VERSION};
			if (ioctl(grant, FES_DE25_DMA_POLL, &info) || !geometry(&info) ||
			    memcmp(info.build_id, build_id, sizeof(build_id)) ||
			    info.generation != generation || info.accepted_sequence != sequence ||
			    !(info.state & FES_DE25_DMA_EXPOSED)) {
				fail("POLL identity or hardware fault");
				goto out;
			}
			if (info.displayed_sequence == sequence) {
				if (info.displayed_slot != slot || (info.state & FES_DE25_DMA_PENDING) ||
				    info.pending_sequence || info.pending_slot != FES_DE25_DMA_NO_SLOT ||
				    info.back_slot != (slot ^ 1U)) {
					fail("complete-frame acknowledgement");
					goto out;
				}
				break;
			}
			if (info.displayed_sequence != sequence - 1 ||
			    !(info.state & FES_DE25_DMA_PENDING) || info.pending_sequence != sequence ||
			    info.pending_slot != slot || info.back_slot != FES_DE25_DMA_NO_SLOT ||
			    now_ms() >= deadline) {
				fail("frame completion timeout or ordering");
				goto out;
			}
			struct timespec delay = {.tv_nsec = 10000000};
			if (nanosleep(&delay, NULL)) {
				fail("completion wait interrupted");
				goto out;
			}
		}
		printf("FRAME_DISPLAYED sequence=%u slot=%u generation=%llu\n", sequence, slot,
			(unsigned long long)generation);
		fflush(stdout);
		struct timespec visible = {.tv_sec = 2};
		if (nanosleep(&visible, NULL)) {
			fail("display observation interrupted");
			goto out;
		}
	}
	/* Observe the final display before asking the owner to prove STOP/drain. */
	if (ioctl(grant, FES_DE25_DMA_POLL, &info) || !geometry(&info) ||
	    memcmp(info.build_id, build_id, sizeof(build_id)) || info.generation != generation || info.state != FES_DE25_DMA_EXPOSED ||
	    info.displayed_sequence != 3 || info.accepted_sequence != 3 ||
	    info.pending_sequence || info.pending_slot != FES_DE25_DMA_NO_SLOT ||
	    info.displayed_slot != 1 || info.back_slot != 0) {
		fail("final display health");
		goto out;
	}
	if (ioctl(owner, FES_DE25_DMA_OWNER_REAP, 0UL)) {
		fail("owner drain/reap");
		goto out;
	}
	puts("DE25_OWNER_SCANOUT_PASS frames=3 allocation_reaped=true");
	result = 0;
out:
	if (result) {
		/* INFO returns cached kernel metadata, without issuing hardware
		 * commands. Last close may safely STOP/drain/CLEAR and erase the
		 * original hardware fault, so retain these details first.
		 */
		struct fes_de25_dma_info cached = {0};
		int saved = errno;
		int observed = ioctl(grant, FES_DE25_DMA_INFO, &cached);
		fprintf(stderr, "CACHED_FAILURE_INFO result=%d generation=%llu state=%u "
			"status=%u errors=%u underflows=%u outstanding=%u accepted=%u displayed=%u pending=%u\n",
			observed, (unsigned long long)cached.generation, cached.state,
			cached.hardware_status, cached.hardware_errors, cached.underflows,
			cached.outstanding, cached.accepted_sequence,
			cached.displayed_sequence, cached.pending_sequence);
		errno = saved;
	}
	/* An uncertain grant remains pinned by the permanent kernel owner. */
	close(grant);
	return result;
}

static int read_text(const char *path, char *value, size_t size)
{
	int fd = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
	ssize_t n;
	if (fd < 0)
		return -1;
	n = read(fd, value, size - 1);
	close(fd);
	if (n < 0 || (size_t)n == size - 1)
		return -1;
	value[n] = 0;
	while (n && (value[n - 1] == '\n' || value[n - 1] == 0))
		value[--n] = 0;
	return 0;
}

int main(int argc, char **argv)
{
	struct utsname u;
	struct stat device;
	char boot[64], board[32], model[128], sysdev[64], extra;
	unsigned int device_major, device_minor;
	int lease, owner, result;
	if ((argc != 3 && argc != 4) || (argc == 4 && strcmp(argv[3], "--allocation-only")) ||
	    strcmp(argv[1], "--boot-id") || strlen(argv[2]) != 36 ||
	    strspn(argv[2], "0123456789abcdef-") != 36)
		return fail("usage: owner_scanout --boot-id <verified boot UUID> [--allocation-only]");
	if (geteuid() || uname(&u) || strcmp(u.machine, "aarch64") ||
	    strcmp(u.release, "6.12.11-fes-de25-owner") ||
	    read_text("/etc/fes-board", board, sizeof(board)) || strcmp(board, "de25-nano") ||
	    read_text("/proc/device-tree/model", model, sizeof(model)) ||
	    strcmp(model, "SoCFPGA Agilex5 Terasic DE25-Nano") ||
	    read_text("/proc/sys/kernel/random/boot_id", boot, sizeof(boot)) || strcmp(boot, argv[2]))
		return fail("board/kernel/boot identity");
	lease = open("/run/fes-de25-computer.lock", O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (lease < 0 || flock(lease, LOCK_EX | LOCK_NB))
		return fail("exclusive maintenance lease");
	owner = open("/dev/fes-de25-scanout", O_RDWR | O_CLOEXEC | O_NOFOLLOW);
	if (owner < 0 || fstat(owner, &device) || !S_ISCHR(device.st_mode) ||
	    (device.st_mode & 07777) != 0600 || device.st_uid || device.st_gid ||
	    read_text("/sys/class/misc/fes-de25-scanout/dev", sysdev, sizeof(sysdev)) ||
	    sscanf(sysdev, "%u:%u%c", &device_major, &device_minor, &extra) != 2 ||
	    major(device.st_rdev) != device_major || minor(device.st_rdev) != device_minor) {
		result = fail("owner device identity");
	} else {
		alarm(30);
		result = run_scanout(owner, argc == 4);
		alarm(0);
	}
	if (owner >= 0)
		close(owner);
	close(lease);
	return result;
}
