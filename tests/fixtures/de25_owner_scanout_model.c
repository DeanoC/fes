/* Exercise the actual diagnostic control flow with a kernel-ABI model.
 * Does not model FPGA memory, SMMU translation, DMA or physical drain. */
#define main diagnostic_entry
#include "../../image/kernel/de25-platform/owner_scanout.c"
#undef main
#include <assert.h>
#include <stdarg.h>

static const char *scenario;
static struct fes_de25_dma_info current;
static unsigned allocations, maps, unmaps, publishes, polls, reaps, closes, seen_slots;
static unsigned pending_polls;
static uint64_t ticks;
static int mapped;
static unsigned char backing[FES_DE25_SCANOUT_ALLOCATION_BYTES];
static int is(const char *name) { return !strcmp(scenario, name); }
static int error(void) { errno = EIO; return -1; }

int __wrap_fcntl(int fd, int cmd, ...)
{
	assert(fd == 101 && cmd == F_GETFD);
	return is("fcntl-error") ? -1 : is("no-cloexec") ? 0 : FD_CLOEXEC;
}
int __wrap_close(int fd) { assert(fd == 101); ++closes; return 0; }
int __wrap_clock_gettime(clockid_t clock, struct timespec *t)
{
	assert(clock == CLOCK_MONOTONIC);
	if (is("clock-error")) return error();
	t->tv_sec = ticks / 1000; t->tv_nsec = (ticks % 1000) * 1000000;
	return 0;
}
int __wrap_nanosleep(const struct timespec *t, struct timespec *unused)
{
	(void)unused;
	ticks += (uint64_t)t->tv_sec * 1000 + t->tv_nsec / 1000000;
	return 0;
}
void *__wrap_mmap(void *address, size_t size, int protection, int flags, int fd, off_t offset)
{
	assert(!address && size == FES_DE25_SCANOUT_SLOT_BYTES &&
		protection == (PROT_READ | PROT_WRITE) && flags == MAP_SHARED && fd == 101);
	assert(offset == (off_t)((size_t)current.back_slot * size) && current.back_slot != current.displayed_slot);
	assert(!mapped);
	if (is("mmap-error")) { errno = ENOMEM; return MAP_FAILED; }
	mapped = 1; ++maps; seen_slots |= 1U << current.back_slot;
	return backing + offset;
}
int __wrap_munmap(void *address, size_t size)
{
	const uint32_t colors[] = {0x00ff0000, 0x0000ff00, 0x000000ff};
	uint32_t *pixels = address;
	assert(mapped && size == FES_DE25_SCANOUT_SLOT_BYTES);
	assert(pixels[0] == 0x00ffffff);
	assert(pixels[100] == colors[publishes]);
	assert(pixels[400 * (FES_DE25_SCANOUT_FRAME_STRIDE / 4) + 1000] == colors[(publishes + 1) % 3]);
	assert(pixels[FES_DE25_SCANOUT_FRAME_BYTES / 4] == 0);
	mapped = 0; ++unmaps;
	return 0;
}
int __wrap_ioctl(int fd, unsigned long command, ...)
{
	va_list ap;
	void *argument;
    if (command == FES_DE25_DMA_OWNER_ABI) {
        assert(fd == 100);
        return is("abi-error") ? error() : is("old-abi") ? 2 : FES_DE25_DMA_ABI_VERSION;
    }
	if (command == FES_DE25_DMA_OWNER_ALLOC) {
		assert(fd == 100 && !allocations++);
		return is("alloc-error") ? error() : 101;
	}
	if (command == FES_DE25_DMA_OWNER_REAP) {
		assert(fd == 100 && !mapped);
		assert(is("allocation-only") ? (!publishes && !maps && !polls && !current.state) :
		       (publishes == 3 && current.displayed_sequence == 3));
		++reaps;
		return is("reap-error") ? error() : 0;
	}
	assert(fd == 101);
	va_start(ap, command); argument = va_arg(ap, void *); va_end(ap);
	if (command == FES_DE25_DMA_INFO) {
		*(struct fes_de25_dma_info *)argument = current;
		if (is("bad-version")) ((struct fes_de25_dma_info *)argument)->version++;
		if (is("bad-geometry")) ((struct fes_de25_dma_info *)argument)->stride++;
		if (is("zero-build")) memset(((struct fes_de25_dma_info *)argument)->build_id, 0, sizeof(current.build_id));
		if (is("zero-generation")) ((struct fes_de25_dma_info *)argument)->generation = 0;
		if (is("initial-outstanding")) ((struct fes_de25_dma_info *)argument)->outstanding = 1;
		return 0;
	}
	if (command == FES_DE25_DMA_PUBLISH) {
		struct fes_de25_dma_publish *p = argument;
		assert(!mapped && p->generation == current.generation && p->version == current.version && !p->reserved);
		assert(p->sequence == ++publishes && p->slot == current.back_slot && p->slot != current.displayed_slot);
		if (is("publish-error")) return error();
		current.accepted_sequence = current.pending_sequence = p->sequence;
		current.pending_slot = p->slot;
		current.back_slot = FES_DE25_DMA_NO_SLOT;
		current.state = FES_DE25_DMA_EXPOSED | FES_DE25_DMA_PENDING;
		pending_polls = 0;
		return 0;
	}
	assert(command == FES_DE25_DMA_POLL);
	struct fes_de25_dma_info *p = argument;
	assert(p->generation == current.generation && p->version == current.version && !p->reserved);
	++polls;
	if (is("poll-error")) return error();
	if (current.state & FES_DE25_DMA_PENDING) {
		if (is("timeout")) ticks += 1000;
		else if (++pending_polls > 1) {
			current.displayed_sequence = current.accepted_sequence;
			current.displayed_slot = current.pending_slot;
			current.back_slot = current.displayed_slot ^ 1U;
			current.pending_sequence = 0; current.pending_slot = FES_DE25_DMA_NO_SLOT;
			current.state = FES_DE25_DMA_EXPOSED;
		}
	}
	*p = current;
	if (is("wrong-ack") && current.displayed_sequence) p->displayed_slot ^= 1;
	if (is("changed-generation")) p->generation++;
	if (is("changed-build")) p->build_id[0] ^= 1;
	if (is("underflow")) p->underflows = 1;
	if (is("hardware-fault")) p->hardware_status = FES_DE25_SCANOUT_STATUS_FAULT;
	if (is("uncertain")) p->state |= FES_DE25_DMA_UNCERTAIN;
	if (is("late-fault") && publishes == 3 && pending_polls == 2 && polls > 6) p->hardware_errors = 1;
	return 0;
}
int main(int argc, char **argv)
{
	assert(argc == 2); scenario = argv[1];
	current = (struct fes_de25_dma_info){.generation = 7, .version = FES_DE25_DMA_ABI_VERSION,
		.allocation_bytes = FES_DE25_SCANOUT_ALLOCATION_BYTES, .slot_bytes = FES_DE25_SCANOUT_SLOT_BYTES,
		.frame_bytes = FES_DE25_SCANOUT_FRAME_BYTES, .width = FES_DE25_SCANOUT_FRAME_WIDTH,
		.height = FES_DE25_SCANOUT_FRAME_HEIGHT, .stride = FES_DE25_SCANOUT_FRAME_STRIDE,
		.build_id = {0x76543210, 0xfedcba98, 0x89abcdef, 0x01234567},
		.slots = 2, .pending_slot = FES_DE25_DMA_NO_SLOT, .back_slot = 1};
	int result = run_scanout(100, is("allocation-only"));
	printf("{\"result\":%d,\"allocations\":%u,\"maps\":%u,\"unmaps\":%u,\"publishes\":%u,\"polls\":%u,\"reaps\":%u,\"closes\":%u,\"seen_slots\":%u}\n",
		result, allocations, maps, unmaps, publishes, polls, reaps, closes, seen_slots);
	return 0;
}
