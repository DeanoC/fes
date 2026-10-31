// SPDX-License-Identifier: GPL-2.0
/* Run only on an exclusively claimed, freshly booted owner-candidate kernel,
 * before any FPGA overlay is applied. No frame is published or MMIO mapped.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <linux/capability.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/sysmacros.h>
#include <sys/utsname.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../de25-dma/de25_dma.h"

static int failure(const char *message)
{
	fprintf(stderr, "owner-unconfigured: %s\n", message);
	return 1;
}

static int read_text(const char *path, char *buffer, size_t size)
{
	int fd = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
	ssize_t count;
	if (fd < 0)
		return -1;
	count = read(fd, buffer, size - 1);
	close(fd);
	if (count < 0 || (size_t)count == size - 1)
		return -1;
	buffer[count] = 0;
	while (count && (buffer[count - 1] == '\n' || buffer[count - 1] == 0))
		buffer[--count] = 0;
	return 0;
}

static int refused(int fd, unsigned long command, unsigned long argument, int expected)
{
	int result;
	errno = 0;
	result = ioctl(fd, command, argument);
	if (result == -1 && errno == expected)
		return 0;
	fprintf(stderr, "owner-unconfigured: ioctl 0x%lx: result %d errno %d; expected %d\n",
		command, result, errno, expected);
	/* A successful ALLOC is ours: EBUSY cannot return a new fd. Reap only this
	 * unexpected new grant; never reap an existing owner's EBUSY allocation.
	 */
	if (command == FES_DE25_DMA_OWNER_ALLOC && result >= 0) {
		if (ioctl(fd, FES_DE25_DMA_OWNER_REAP, 0UL))
			fprintf(stderr, "owner-unconfigured: unexpected grant reap failed: %s\n", strerror(errno));
		close(result);
	}
	return -1;
}

int main(int argc, char **argv)
{
	struct utsname identity;
	struct stat device;
	char boot[64], model[128], board[32], sysdev[64], extra;
	unsigned int device_major, device_minor;
	int fd = -1, lock = -1, status, flags, result = 1;
	pid_t child;

	if (argc != 3 || strcmp(argv[1], "--boot-id"))
		return failure("usage: owner_unconfigured --boot-id <observed candidate boot UUID>");
	if (strlen(argv[2]) != 36 || strspn(argv[2], "0123456789abcdef-") != 36)
		return failure("invalid expected boot UUID");
	if (uname(&identity) || strcmp(identity.machine, "aarch64") ||
	    strcmp(identity.release, "6.12.11-fes-de25-owner"))
		return failure("requires the AArch64 owner-candidate board kernel");
	if (read_text("/etc/fes-board", board, sizeof(board)) || strcmp(board, "de25-nano") ||
	    read_text("/proc/device-tree/model", model, sizeof(model)) ||
	    strcmp(model, "SoCFPGA Agilex5 Terasic DE25-Nano") ||
	    read_text("/proc/sys/kernel/random/boot_id", boot, sizeof(boot)) || strcmp(boot, argv[2]))
		return failure("board or boot identity differs");
	if (geteuid())
		return failure("requires root with CAP_SYS_RAWIO");
	lock = open("/run/fes-de25-computer.lock", O_RDWR | O_CREAT | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (lock < 0 || flock(lock, LOCK_EX | LOCK_NB)) {
		failure("maintenance lease unavailable");
		goto out;
	}
	fd = open("/dev/fes-de25-scanout", O_RDWR | O_CLOEXEC | O_NOFOLLOW);
	if (fd < 0 || fstat(fd, &device) || !S_ISCHR(device.st_mode) ||
	    (device.st_mode & 07777) != 0600 || device.st_uid || device.st_gid ||
	    read_text("/sys/class/misc/fes-de25-scanout/dev", sysdev, sizeof(sysdev)) ||
	    sscanf(sysdev, "%u:%u%c", &device_major, &device_minor, &extra) != 2 ||
	    major(device.st_rdev) != device_major || minor(device.st_rdev) != device_minor ||
	    (flags = fcntl(fd, F_GETFD)) < 0 || !(flags & FD_CLOEXEC)) {
		failure("owner control device identity, permissions or descriptor flags differ");
		goto out;
	}
	if (refused(fd, _IO(0xD5, 0x7f), 0, ENOTTY) ||
	    refused(fd, FES_DE25_DMA_OWNER_ALLOC, 1, EINVAL) ||
	    refused(fd, FES_DE25_DMA_OWNER_REAP, 1, EINVAL) ||
	    refused(fd, FES_DE25_DMA_OWNER_ALLOC, 0, ENODEV))
		goto out;
	child = fork();
	if (child < 0) {
		failure("fork failed");
		goto out;
	}
	if (!child) {
		struct __user_cap_header_struct header = { .version = _LINUX_CAPABILITY_VERSION_3 };
		struct __user_cap_data_struct capabilities[2] = {{0}, {0}};
		if (syscall(SYS_capset, &header, capabilities))
			_exit(2);
		_exit(refused(fd, FES_DE25_DMA_OWNER_ALLOC, 0, EPERM) ||
		      refused(fd, FES_DE25_DMA_OWNER_REAP, 0, EPERM) ||
		      refused(fd, _IO(0xD5, 0x7f), 0, EPERM) ? 3 : 0);
	}
	if (waitpid(child, &status, 0) != child || !WIFEXITED(status) || WEXITSTATUS(status)) {
		failure("capability-dropped child was not refused");
		goto out;
	}
	/* The refused calls and child close must not change the unconfigured state. */
	if (refused(fd, FES_DE25_DMA_OWNER_ALLOC, 0, ENODEV) ||
	    read_text("/proc/sys/kernel/random/boot_id", boot, sizeof(boot)) || strcmp(boot, argv[2]))
		goto out;
	puts("DE25_OWNER_UNCONFIGURED_USERSPACE_PASS");
	result = 0;
out:
	if (fd >= 0)
		close(fd);
	if (lock >= 0)
		close(lock);
	return result;
}
