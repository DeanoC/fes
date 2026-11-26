// SPDX-License-Identifier: GPL-2.0
/* A computer's window in HPS DDR (fes.fabric.de25-hps-window 1.0).
 *
 * The owner allocates one contiguous span, publishes it to the window's
 * control block while the machine is held, and gives userspace a file it can
 * map at any page offset. No CPU or DMA address leaves the kernel.
 *
 * The control block answers SLVERR to anything it refuses, which on arm64
 * can raise an SError. So nothing here writes a register the block could
 * refuse: grant registers only while it reports not granted, GRANT only with
 * every precondition just read back, CLEAR only while held and drained.
 *
 * A latched fault is fatal in 1.0: the faulting port keeps its command
 * presented, never drains, and CLEAR is never accepted. Such a window stays
 * exposed, with its storage and the module pinned, until the FPGA is
 * reprogrammed, which the owner refuses while the window is leased.
 */
#include <linux/anon_inodes.h>
#include <linux/atomic.h>
#include <linux/build_bug.h>
#include <linux/dma-map-ops.h>
#include <linux/dma-mapping.h>
#include <linux/err.h>
#include <linux/file.h>
#include <linux/kref.h>
#include <linux/list.h>
#include <linux/mm.h>
#include <linux/module.h>
#include <linux/mutex.h>
#include <linux/sched/mm.h>
#include <linux/slab.h>
#include <linux/uaccess.h>
#include <linux/vmalloc.h>

#include "de25_dma_internal.h"
#include "generated/fes_de25_hps_window.h"

#define WINDOW_STATUS(name) FES_DE25_HPS_WINDOW_STATUS_##name

static_assert(sizeof(struct fes_de25_dma_window_info) == 56);
static_assert(FES_DE25_HPS_WINDOW_DMA_ADDRESS_BITS == 32);
static_assert(FES_DE25_HPS_WINDOW_SPAN % PAGE_SIZE == 0);
static_assert(FES_DE25_HPS_WINDOW_GRANT_ALIGNMENT <= PAGE_SIZE);

struct fes_de25_window_mm {
	struct list_head link;
	struct mm_struct *mm;
};

struct fes_de25_window {
	struct kref refs;
	struct mutex lock;
	struct mutex revoke_lock;
	struct list_head mms;
	struct device *dev;
	struct inode *inode;
	struct fes_de25_window_backend backend;
	void *cpu;
	dma_addr_t dma;
	phys_addr_t storage;
	bool mapped;
	u64 generation, epoch;
	u32 build_id[4];
	u32 status, fault;
	bool revoked, exposed, uncertain;
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	struct fes_de25_window_test_observer *observer;
#endif
};

static atomic64_t next_generation = ATOMIC64_INIT(0);

static u64 window_next_generation(void)
{
	s64 previous;
	u64 next;

	for (;;) {
		previous = atomic64_read(&next_generation);
		next = (u64)previous + 1;
		if (!next)
			return 0;
		if (atomic64_cmpxchg(&next_generation, previous, (s64)next) == previous)
			return next;
	}
}

/* The F2SDRAM firewall's region 0 less the vendor tree's service buffer,
 * where the tree pins the pool (scripts/de25_kernel_platform.py
 * WINDOW_POOL_RANGE). End exclusive.
 */
#define WINDOW_POOL_FIRST 0x82000000ULL
#define WINDOW_POOL_END 0xc0000000ULL

/* A reserved pool the page allocator never holds, of exactly one window: not
 * reusable (a migration could fail), not no-map (the device mapping would
 * then be an MMIO one, and the pages the DMA API maps would be missing).
 */
int fes_de25_window_pool_check(bool compatible, bool reusable, bool no_map, phys_addr_t base,
		phys_addr_t size)
{
	if (!compatible || reusable || no_map)
		return -EINVAL;
	if (size != FES_DE25_HPS_WINDOW_SPAN || !PAGE_ALIGNED(base) ||
	    base < WINDOW_POOL_FIRST || base > WINDOW_POOL_END - size)
		return -ERANGE;
	return 0;
}

/* The protection dma_mmap_attrs and the DMA API's kernel remap give a
 * contiguous allocation for this device: uncached where it is not coherent.
 */
static pgprot_t window_pgprot(struct device *dev, pgprot_t prot)
{
	return dev_is_dma_coherent(dev) ? prot : pgprot_dmacoherent(prot);
}

/* The owner's pool (fes,de25-window-pool) is reserved memory the page
 * allocator never holds, so taking it cannot fail on page migration as a
 * reusable pool's 256 MiB did on Kit D25. It keeps its struct pages, so it is
 * mapped exactly as the DMA API maps a contiguous allocation for this device
 * (iommu_dma_alloc_pages): an uncached kernel alias, the linear alias
 * cleaned and invalidated, the device address through dma_map_page with the
 * non-coherent protection, and user maps with window_pgprot. Zeroed for
 * every window, so no session sees another's memory.
 */
static int window_map_storage(struct fes_de25_window *window, phys_addr_t base)
{
	const unsigned long count = FES_DE25_HPS_WINDOW_SPAN >> PAGE_SHIFT;
	struct device *dev = window->dev;
	struct page **pages, *first;
	unsigned long i;

	if (!base || !PAGE_ALIGNED(base) || !pfn_valid(PHYS_PFN(base)) ||
	    !pfn_valid(PHYS_PFN(base + FES_DE25_HPS_WINDOW_SPAN - 1)))
		return -EINVAL;
	first = pfn_to_page(PHYS_PFN(base));
	pages = kvmalloc_array(count, sizeof(*pages), GFP_KERNEL);
	if (!pages)
		return -ENOMEM;
	for (i = 0; i < count; i++)
		pages[i] = nth_page(first, i);
	window->cpu = vmap(pages, count, VM_MAP, window_pgprot(dev, PAGE_KERNEL));
	kvfree(pages);
	if (!window->cpu)
		return -ENOMEM;
	/* Mapping for the device cleans the linear alias; the sync for the CPU
	 * then invalidates it, as arch_dma_prep_coherent does for an allocation.
	 */
	window->dma = dma_map_page_attrs(dev, first, 0, FES_DE25_HPS_WINDOW_SPAN, DMA_BIDIRECTIONAL, 0);
	if (dma_mapping_error(dev, window->dma))
		return -ENOMEM;
	window->mapped = true;
	dma_sync_single_for_cpu(dev, window->dma, FES_DE25_HPS_WINDOW_SPAN, DMA_BIDIRECTIONAL);
	memset(window->cpu, 0, FES_DE25_HPS_WINDOW_SPAN);
	wmb();
	window->storage = base;
	return 0;
}

static void window_destroy(struct kref *refs)
{
	struct fes_de25_window *window = container_of(refs, struct fes_de25_window, refs);
	struct fes_de25_window_mm *entry, *next;

	/* An exposed window holds its own reference and cannot get here. */
	WARN_ON(window->exposed);
	/* The storage is the owner's reserved pool: only its mappings end. */
	if (window->mapped)
		dma_unmap_page_attrs(window->dev, window->dma, FES_DE25_HPS_WINDOW_SPAN,
			DMA_BIDIRECTIONAL, DMA_ATTR_SKIP_CPU_SYNC);
	if (window->cpu)
		vunmap(window->cpu);
	list_for_each_entry_safe(entry, next, &window->mms, link) {
		mmdrop(entry->mm);
		kfree(entry);
	}
	if (window->inode)
		iput(window->inode);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (window->observer)
		window->observer->freed++;
#endif
	window->backend.release(window->backend.context);
	put_device(window->dev);
	kfree(window);
}

/* Status and first fault, with the configuration proof on both sides. */
static int window_observe(struct fes_de25_window *window, u32 *status, u32 *fault)
{
	struct fes_de25_window_backend *io = &window->backend;
	int ret;

	if (io->epoch(io->context) != window->epoch)
		return -ESTALE;
	ret = io->read32(io->context, FES_DE25_HPS_WINDOW_STATUS_OFFSET, status);
	if (!ret)
		ret = io->read32(io->context, FES_DE25_HPS_WINDOW_FAULT_OFFSET, fault);
	if (!ret && (*status & ~FES_DE25_HPS_WINDOW_STATUS_MASK))
		ret = -EPROTO;
	if (!ret && io->epoch(io->context) != window->epoch)
		ret = -ESTALE;
	if (!ret) {
		window->status = *status;
		window->fault = *fault;
	}
	return ret;
}

static bool window_quiet(u32 status)
{
	return (status & (WINDOW_STATUS(HELD) | WINDOW_STATUS(DRAINED) | WINDOW_STATUS(FAULT))) ==
		(WINDOW_STATUS(HELD) | WINDOW_STATUS(DRAINED));
}

/* CLEAR only after reading held, drained and no fault; confirm not granted. */
static int window_clear(struct fes_de25_window *window)
{
	struct fes_de25_window_backend *io = &window->backend;
	u32 status, fault;
	int ret;

	ret = window_observe(window, &status, &fault);
	if (ret)
		return ret;
	if (status & WINDOW_STATUS(FAULT))
		return -EIO;
	if (!window_quiet(status))
		return -EAGAIN;
	ret = io->write32(io->context, FES_DE25_HPS_WINDOW_COMMAND_OFFSET,
			FES_DE25_HPS_WINDOW_COMMAND_CLEAR);
	if (!ret)
		ret = io->io_fence(io->context);
	if (!ret)
		ret = window_observe(window, &status, &fault);
	if (!ret && (status & (WINDOW_STATUS(GRANTED) | WINDOW_STATUS(FAULT))))
		ret = -EIO;
	return ret;
}

int fes_de25_window_revoke_owned(struct fes_de25_window *window)
{
	struct fes_de25_window_mm *entry;
	bool drop = false;
	int ret = 0;

	mutex_lock(&window->revoke_lock);
	mutex_lock(&window->lock);
	window->revoked = true;
	mutex_unlock(&window->lock);
	/* As for the scanout grant: synchronize past any mmap that installed
	 * PTEs before its VMA was linked, then zap every mapping.
	 */
	list_for_each_entry(entry, &window->mms, link) {
		mmap_write_lock(entry->mm);
		mmap_write_unlock(entry->mm);
	}
	if (window->inode)
		unmap_mapping_range(window->inode->i_mapping, 0, 0, 1);
	mutex_lock(&window->lock);
	if (!window->exposed)
		goto out;
	ret = window_clear(window);
	if (ret) {
		window->uncertain = true;
		goto out;
	}
	window->exposed = false;
	window->uncertain = false;
	drop = true;
out:
	mutex_unlock(&window->lock);
	mutex_unlock(&window->revoke_lock);
	if (drop) {
		kref_put(&window->refs, window_destroy);
		module_put(THIS_MODULE);
	}
	return ret;
}

struct fes_de25_window *fes_de25_window_get(struct file *file)
{
	struct fes_de25_window *window = file->private_data;

	__module_get(THIS_MODULE);
	kref_get(&window->refs);
	return window;
}

void fes_de25_window_put(struct fes_de25_window *window)
{
	kref_put(&window->refs, window_destroy);
	module_put(THIS_MODULE);
}

int fes_de25_window_info(struct file *file, struct fes_de25_dma_window_info *info)
{
	struct fes_de25_window *window = file->private_data;
	u32 status, fault;
	int ret;

	if (info->version != FES_DE25_DMA_WINDOW_VERSION || info->reserved)
		return -EINVAL;
	mutex_lock(&window->lock);
	memset(info, 0, sizeof(*info));
	ret = window->exposed ? window_observe(window, &status, &fault) : 0;
	/* Any failed observation of an exposed window leaves no trustworthy
	 * hardware state: no new mapping until a revoke proves the clear.
	 */
	if (ret)
		window->uncertain = true;
	info->generation = window->generation;
	info->version = FES_DE25_DMA_WINDOW_VERSION;
	info->contract = FES_DE25_HPS_WINDOW_VERSION;
	info->span = FES_DE25_HPS_WINDOW_SPAN;
	info->status = window->status;
	info->fault = window->fault;
	info->state = (window->revoked ? FES_DE25_DMA_REVOKED : 0) |
		(window->exposed ? FES_DE25_DMA_EXPOSED : 0) |
		(window->uncertain ? FES_DE25_DMA_UNCERTAIN : 0);
	memcpy(info->build_id, window->build_id, sizeof(info->build_id));
	mutex_unlock(&window->lock);
	return ret == -ESTALE ? 0 : ret;
}

static vm_fault_t window_fault(struct vm_fault *vmf)
{
	/* The DMA API installed every PTE; none is recreated after a zap. */
	return VM_FAULT_SIGBUS;
}

static const struct vm_operations_struct window_vm_ops = {
	.fault = window_fault,
};

static int window_mmap(struct file *file, struct vm_area_struct *vma)
{
	struct fes_de25_window *window = file->private_data;
	struct fes_de25_window_mm *entry;
	bool found = false, added = false;
	int ret = 0;

	if (!(vma->vm_flags & VM_SHARED) || (vma->vm_flags & VM_EXEC) ||
	    vma->vm_pgoff >= FES_DE25_HPS_WINDOW_SPAN / PAGE_SIZE ||
	    vma_pages(vma) > FES_DE25_HPS_WINDOW_SPAN / PAGE_SIZE - vma->vm_pgoff)
		return -EINVAL;
	mutex_lock(&window->lock);
	if (window->revoked || !window->exposed || window->uncertain) {
		ret = -EACCES;
		goto out;
	}
	list_for_each_entry(entry, &window->mms, link)
		if (entry->mm == vma->vm_mm) {
			found = true;
			break;
		}
	if (!found) {
		entry = kzalloc(sizeof(*entry), GFP_KERNEL);
		if (!entry) {
			ret = -ENOMEM;
			goto out;
		}
		mmgrab(vma->vm_mm);
		entry->mm = vma->vm_mm;
		list_add_tail(&entry->link, &window->mms);
		added = true;
	}
	/* vm_pgoff pages into the pool, with the DMA API's mmap protection. */
	vma->vm_ops = NULL;
	vma->vm_page_prot = window_pgprot(window->dev, vma->vm_page_prot);
	ret = remap_pfn_range(vma, vma->vm_start, PHYS_PFN(window->storage) + vma->vm_pgoff,
			vma->vm_end - vma->vm_start, vma->vm_page_prot);
	if (!ret && (vma->vm_ops ||
	    (vma->vm_flags & (VM_IO | VM_PFNMAP)) != (VM_IO | VM_PFNMAP)))
		ret = -EOPNOTSUPP;  /* only an eager I/O PFN mapping can be revoked */
	if (!ret) {
		vm_flags_set(vma, VM_DONTEXPAND | VM_DONTDUMP | VM_DONTCOPY);
		vm_flags_clear(vma, VM_MAYEXEC);
		vma->vm_ops = &window_vm_ops;
	}
out:
	if (ret && added) {
		list_del(&entry->link);
		mmdrop(entry->mm);
		kfree(entry);
	}
	mutex_unlock(&window->lock);
	return ret;
}

static long window_ioctl(struct file *file, unsigned int cmd, unsigned long arg)
{
	struct fes_de25_dma_window_info info;
	int ret;

	switch (cmd) {
	case FES_DE25_DMA_WINDOW_INFO:
		if (copy_from_user(&info, (void __user *)arg, sizeof(info)))
			return -EFAULT;
		ret = fes_de25_window_info(file, &info);
		if (ret)
			return ret;
		return copy_to_user((void __user *)arg, &info, sizeof(info)) ? -EFAULT : 0;
	case FES_DE25_DMA_WINDOW_REVOKE:
		if (arg)
			return -EINVAL;
		return fes_de25_window_revoke_owned(file->private_data);
	default:
		return -ENOTTY;
	}
}

static int window_release(struct inode *inode, struct file *file)
{
	struct fes_de25_window *window = file->private_data;

	/* A window that cannot be cleared keeps its exposure reference. */
	fes_de25_window_revoke_owned(window);
	kref_put(&window->refs, window_destroy);
	return 0;
}

static const struct file_operations window_fops = {
	.owner = THIS_MODULE,
	.mmap = window_mmap,
	.unlocked_ioctl = window_ioctl,
#ifdef CONFIG_COMPAT
	.compat_ioctl = compat_ptr_ioctl,
#endif
	.release = window_release,
	.llseek = noop_llseek,
};

/* Publish the grant: every precondition is read back first, so no write can
 * be refused. A grant that may have reached the block pins the window.
 */
static int window_publish(struct fes_de25_window *window)
{
	struct fes_de25_window_backend *io = &window->backend;
	const u32 expected[] = { lower_32_bits(window->dma), upper_32_bits((u64)window->dma),
		FES_DE25_HPS_WINDOW_SPAN, lower_32_bits(window->generation),
		upper_32_bits(window->generation) };
	const u32 offsets[] = { FES_DE25_HPS_WINDOW_GRANT_LO_OFFSET, FES_DE25_HPS_WINDOW_GRANT_HI_OFFSET,
		FES_DE25_HPS_WINDOW_GRANT_SPAN_OFFSET, FES_DE25_HPS_WINDOW_GENERATION_LO_OFFSET,
		FES_DE25_HPS_WINDOW_GENERATION_HI_OFFSET };
	u32 status, fault, value;
	unsigned int i;
	int ret;

	ret = window_observe(window, &status, &fault);
	if (ret)
		return ret;
	if (status & (WINDOW_STATUS(GRANTED) | WINDOW_STATUS(FAULT)) || !window_quiet(status))
		return -EBUSY;
	for (i = 0; i < ARRAY_SIZE(offsets) && !ret; i++)
		ret = io->write32(io->context, offsets[i], expected[i]);
	if (!ret)
		ret = io->io_fence(io->context);
	for (i = 0; i < ARRAY_SIZE(offsets) && !ret; i++) {
		ret = io->read32(io->context, offsets[i], &value);
		if (!ret && value != expected[i])
			ret = -EIO;
	}
	if (!ret)
		ret = window_observe(window, &status, &fault);
	if (!ret && (status & (WINDOW_STATUS(GRANTED) | WINDOW_STATUS(FAULT)) || !window_quiet(status)))
		ret = -EBUSY;
	if (ret)
		return ret;
	/* From here the fabric may issue: pin before GRANT, as the scanout
	 * pins before SUBMIT.
	 */
	if (!try_module_get(THIS_MODULE))
		return -ENODEV;
	kref_get(&window->refs);
	window->exposed = true;
	dma_wmb();
	ret = io->write32(io->context, FES_DE25_HPS_WINDOW_COMMAND_OFFSET,
			FES_DE25_HPS_WINDOW_COMMAND_GRANT);
	if (!ret)
		ret = io->io_fence(io->context);
	if (!ret)
		ret = window_observe(window, &status, &fault);
	if (!ret && (!(status & WINDOW_STATUS(GRANTED)) || (status & WINDOW_STATUS(FAULT))))
		ret = -EIO;
	if (ret)
		window->uncertain = true;
	return ret;
}

struct file *fes_de25_window_create(struct device *dev, const struct fes_de25_window_backend *backend,
		phys_addr_t storage, struct fes_de25_window **pinned)
{
	struct fes_de25_window *window;
	struct fes_de25_window_backend io;
	struct file *file;
	u32 identity, version;
	int ret;

	if (pinned)
		*pinned = NULL;
	if (!dev || !backend)
		return ERR_PTR(-EINVAL);
	io = *backend;
	if (!io.acquire || !io.release || !io.epoch || !io.read32 || !io.write32 ||
	    !io.io_fence || !io.build_id)
		return ERR_PTR(-EINVAL);
	window = kzalloc(sizeof(*window), GFP_KERNEL);
	if (!window)
		return ERR_PTR(-ENOMEM);
	window->dev = get_device(dev);
	window->backend = io;
	kref_init(&window->refs);
	mutex_init(&window->lock);
	mutex_init(&window->revoke_lock);
	INIT_LIST_HEAD(&window->mms);
	ret = io.acquire(io.context);
	if (ret)
		goto fail_unowned;
	window->epoch = io.epoch(io.context);
	if (!window->epoch) {
		ret = -EOPNOTSUPP;
		goto fail_lease;
	}
	window->generation = window_next_generation();
	if (!window->generation) {
		ret = -EOVERFLOW;
		goto fail_lease;
	}
	ret = io.read32(io.context, FES_DE25_HPS_WINDOW_ID_OFFSET, &identity);
	if (!ret)
		ret = io.read32(io.context, FES_DE25_HPS_WINDOW_VERSION_OFFSET, &version);
	if (!ret && (identity != FES_DE25_HPS_WINDOW_IDENTITY || version != FES_DE25_HPS_WINDOW_VERSION))
		ret = -EPROTONOSUPPORT;
	if (!ret)
		ret = io.build_id(io.context, window->build_id);
	if (!ret && !(window->build_id[0] | window->build_id[1] | window->build_id[2] | window->build_id[3]))
		ret = -EPROTONOSUPPORT;
	if (!ret)
		ret = dma_set_mask_and_coherent(dev, DMA_BIT_MASK(FES_DE25_HPS_WINDOW_DMA_ADDRESS_BITS));
	if (ret)
		goto fail_lease;
	/* From here the reference owns the lease and the storage's mappings. */
	ret = window_map_storage(window, storage);
	if (ret) {
		kref_put(&window->refs, window_destroy);
		return ERR_PTR(ret);
	}
	if (!IS_ALIGNED(window->dma, FES_DE25_HPS_WINDOW_GRANT_ALIGNMENT) ||
	    (u64)window->dma > (1ULL << FES_DE25_HPS_WINDOW_DMA_ADDRESS_BITS) - FES_DE25_HPS_WINDOW_SPAN) {
		kref_put(&window->refs, window_destroy);
		return ERR_PTR(-ERANGE);
	}
	/* Publish before any file exists, so a refusal is released here and
	 * now, not by a deferred final fput.
	 */
	mutex_lock(&window->lock);
	ret = window_publish(window);
	if (ret && window->exposed && !window_clear(window)) {
		window->exposed = false;
		kref_put(&window->refs, window_destroy);  /* the exposure pin; ours remains */
		module_put(THIS_MODULE);
	}
	mutex_unlock(&window->lock);
	if (ret) {
		/* A grant that could not be cleared keeps its exposure pin, and
		 * the owner gets our reference so its REAP can retry the clear.
		 */
		if (window->exposed && pinned) {
			__module_get(THIS_MODULE);
			*pinned = window;
		} else {
			kref_put(&window->refs, window_destroy);
		}
		return ERR_PTR(ret);
	}
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	file = fes_de25_window_test_fail_file ? ERR_PTR(-ENFILE) :
#else
	file =
#endif
		anon_inode_create_getfile("fes-de25-window", &window_fops, window, O_RDWR, NULL);
	if (IS_ERR(file)) {
		/* As for a failed publish: a grant that cannot be cleared goes to
		 * the owner, so its REAP can retry.
		 */
		if (fes_de25_window_revoke_owned(window) && window->exposed && pinned) {
			__module_get(THIS_MODULE);
			*pinned = window;
		} else {
			kref_put(&window->refs, window_destroy);
		}
		return file;
	}
	window->inode = igrab(file_inode(file));
	i_size_write(window->inode, FES_DE25_HPS_WINDOW_SPAN);
	return file;
fail_lease:
	io.release(io.context);
fail_unowned:
	put_device(window->dev);
	kfree(window);
	return ERR_PTR(ret);
}

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
bool fes_de25_window_test_fail_file;

void fes_de25_window_test_observe(struct file *file, struct fes_de25_window_test_observer *observer)
{
	((struct fes_de25_window *)file->private_data)->observer = observer;
}
#endif
