// SPDX-License-Identifier: GPL-2.0
/* Linux-owned, revocable DMA storage. DE25 hardware binding remains closed. */
#include <linux/anon_inodes.h>
#include <linux/atomic.h>
#include <linux/build_bug.h>
#include <linux/completion.h>
#include <linux/dma-mapping.h>
#include <linux/err.h>
#include <linux/kref.h>
#include <linux/list.h>
#include <linux/mm.h>
#include <linux/module.h>
#include <linux/mutex.h>
#include <linux/of.h>
#include <linux/platform_device.h>
#include <linux/slab.h>
#include <linux/uaccess.h>

#include "de25_dma_internal.h"

/* The pinned IOMMU DMA implementation maps page-array allocations with
 * vm_map_pages(), allowing page pins beyond PTE revocation. Request actual
 * contiguous backing so its DMA mmap path can install revocable I/O PFNs.
 * The mmap shape check remains mandatory; no driver-made PFN mapping.
 */
#define FES_DE25_DMA_ATTRIBUTES DMA_ATTR_FORCE_CONTIGUOUS

static_assert(FES_DE25_SCANOUT_DMA_ADDRESS_BITS == 32);
static_assert(FES_DE25_DMA_SLOTS == 2);
static_assert(FES_DE25_DMA_FRAME_BYTES <= FES_DE25_DMA_SLOT_BYTES);
static_assert(FES_DE25_DMA_ALLOCATION_BYTES == FES_DE25_DMA_SLOTS * FES_DE25_DMA_SLOT_BYTES);

struct fes_de25_dma_mm {
	struct list_head link;
	struct mm_struct *mm;
};

struct fes_de25_dma {
	struct kref refs;
	struct mutex lock;
	struct mutex revoke_lock;
	struct list_head mms;
	struct device *dev;
	struct inode *inode;
	struct fes_de25_dma_backend backend;
	void *cpu;
	dma_addr_t dma;
	u64 generation, epoch;
	u32 sequence, accepted_sequence, displayed_sequence, displayed_slot, pending_slot;
	u32 hardware_status, hardware_errors, underflows, outstanding;
	bool revoked, exposed, uncertain, pending;
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	struct fes_de25_dma_test_observer *observer;
#endif
};

static atomic64_t next_generation = ATOMIC64_INIT(0);

static u64 fes_de25_dma_next_generation(atomic64_t *counter)
{
	s64 previous;
	u64 next;

	for (;;) {
		previous = atomic64_read(counter);
		next = (u64)previous + 1;
		if (!next)
			return 0;
		if (atomic64_cmpxchg(counter, previous, (s64)next) == previous)
			return next;
	}
}

static void fes_de25_dma_destroy(struct kref *refs)
{
	struct fes_de25_dma *grant = container_of(refs, struct fes_de25_dma, refs);
	struct fes_de25_dma_mm *entry, *next;

	/* Exposed grants hold an independent reference and cannot get here. */
	WARN_ON(grant->exposed);
	dma_free_attrs(grant->dev, FES_DE25_DMA_ALLOCATION_BYTES, grant->cpu, grant->dma,
		FES_DE25_DMA_ATTRIBUTES);
	list_for_each_entry_safe(entry, next, &grant->mms, link) {
		mmdrop(entry->mm);
		kfree(entry);
	}
	if (grant->inode)
		iput(grant->inode);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (grant->observer)
		grant->observer->freed++;
#endif
	/* The provider may now retire its MMIO/context/domain resources. No
	 * callback or observer access is permitted after releasing the lease.
	 */
	grant->backend.release(grant->backend.context);
	put_device(grant->dev);
	kfree(grant);
}

/* A snapshot command atomically freezes the tuple in the pixel domain. The
 * serial handshake and recheck reject a delayed command or another owner.
 */
#define FES_DE25_SNAPSHOT_TRIES 128
struct fes_de25_snapshot {
	u64 generation, dma;
	u32 status, accepted, displayed, current_sequence, slots, errors, underflows;
	u32 outstanding, span;
};

static int fes_de25_snapshot(struct fes_de25_dma *grant, struct fes_de25_snapshot *snapshot)
{
	struct fes_de25_dma_backend *io = &grant->backend;
	u32 old, serial, lo, hi;
	unsigned int attempt;
	int ret;

	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET, &old);
	if (ret)
		return ret;
	ret = io->write32(io->context, FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_SNAPSHOT);
	if (ret)
		return ret;
	ret = io->io_fence(io->context);
	if (ret)
		return ret;
	for (attempt = 0; attempt < FES_DE25_SNAPSHOT_TRIES; attempt++) {
		ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET, &serial);
		if (ret)
			return ret;
		if (serial == old + 1U)
			break;
		if (serial != old)
			return -EPROTO;
		cpu_relax();
	}
	if (attempt == FES_DE25_SNAPSHOT_TRIES)
		return -ETIMEDOUT;
#define READ_SNAPSHOT(member, reg) do { \
	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_##reg##_OFFSET, &snapshot->member); \
	if (ret) return ret; \
} while (0)
	READ_SNAPSHOT(status, STATUS);
	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_GENERATION_LO_OFFSET, &lo);
	if (ret) return ret;
	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_GENERATION_HI_OFFSET, &hi);
	if (ret) return ret;
	snapshot->generation = (u64)hi << 32 | lo;
	READ_SNAPSHOT(accepted, ACCEPTED_SEQUENCE);
	READ_SNAPSHOT(displayed, DISPLAYED_SEQUENCE);
	READ_SNAPSHOT(current_sequence, CURRENT_SEQUENCE);
	READ_SNAPSHOT(slots, SLOTS);
	READ_SNAPSHOT(errors, ERRORS);
	READ_SNAPSHOT(underflows, UNDERFLOWS);
	READ_SNAPSHOT(outstanding, OUTSTANDING);
	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_GRANT_LO_OFFSET, &lo);
	if (ret) return ret;
	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_GRANT_HI_OFFSET, &hi);
	if (ret) return ret;
	snapshot->dma = (u64)hi << 32 | lo;
	READ_SNAPSHOT(span, GRANT_SPAN);
#undef READ_SNAPSHOT
	ret = io->read32(io->context, FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET, &lo);
	if (ret)
		return ret;
	return lo == serial ? 0 : -EPROTO;
}

static void fes_de25_dma_info_locked(struct fes_de25_dma *grant, struct fes_de25_dma_info *info)
{
	memset(info, 0, sizeof(*info));
	info->generation = grant->generation;
	info->state = (grant->revoked ? FES_DE25_DMA_REVOKED : 0) |
		(grant->exposed ? FES_DE25_DMA_EXPOSED : 0) |
		(grant->uncertain ? FES_DE25_DMA_UNCERTAIN : 0) |
		(grant->pending ? FES_DE25_DMA_PENDING : 0);
	info->version = FES_DE25_DMA_ABI_VERSION;
	info->allocation_bytes = FES_DE25_DMA_ALLOCATION_BYTES;
	info->slot_bytes = FES_DE25_DMA_SLOT_BYTES;
	info->frame_bytes = FES_DE25_DMA_FRAME_BYTES;
	info->width = FES_DE25_DMA_WIDTH;
	info->height = FES_DE25_DMA_HEIGHT;
	info->stride = FES_DE25_DMA_STRIDE;
	info->slots = FES_DE25_DMA_SLOTS;
	info->accepted_sequence = grant->accepted_sequence;
	info->displayed_sequence = grant->displayed_sequence;
	info->displayed_slot = grant->displayed_slot;
	info->pending_sequence = grant->pending ? grant->sequence : 0;
	info->pending_slot = grant->pending ? grant->pending_slot : FES_DE25_DMA_NO_SLOT;
	info->back_slot = grant->pending || grant->uncertain || grant->revoked ?
		FES_DE25_DMA_NO_SLOT : grant->displayed_slot ^ 1U;
	info->hardware_status = grant->hardware_status;
	info->hardware_errors = grant->hardware_errors;
	info->underflows = grant->underflows;
	info->outstanding = grant->outstanding;
}

void fes_de25_dma_info(struct file *file, struct fes_de25_dma_info *info)
{
	struct fes_de25_dma *grant = file->private_data;

	mutex_lock(&grant->lock);
	fes_de25_dma_info_locked(grant, info);
	mutex_unlock(&grant->lock);
}

static int fes_de25_dma_identity(struct fes_de25_dma *grant, const struct fes_de25_snapshot *snapshot)
{
	if (grant->backend.epoch(grant->backend.context) != grant->epoch ||
	    snapshot->generation != grant->generation)
		return -ESTALE;
	if ((snapshot->status & ~FES_DE25_SCANOUT_STATUS_MASK) ||
	    !(snapshot->status & FES_DE25_SCANOUT_STATUS_CONFIGURED) ||
	    snapshot->dma != (u64)grant->dma || snapshot->span != FES_DE25_DMA_ALLOCATION_BYTES)
		return -EPROTO;
	return 0;
}

/* Acceptance owns the new slot but does not release the old front slot. Only
 * an exact full-frame displayed acknowledgement releases that previous slot.
 */
static int fes_de25_dma_update(struct fes_de25_dma *grant, const struct fes_de25_snapshot *snapshot)
{
	u32 displayed_slot = (snapshot->slots >> FES_DE25_SCANOUT_DISPLAYED_SLOT_SHIFT) & FES_DE25_SCANOUT_SLOT_MASK;
	u32 current_slot = (snapshot->slots >> FES_DE25_SCANOUT_CURRENT_SLOT_SHIFT) & FES_DE25_SCANOUT_SLOT_MASK;
	int ret = fes_de25_dma_identity(grant, snapshot);

	if (ret)
		return ret;
	grant->hardware_status = snapshot->status;
	grant->hardware_errors = snapshot->errors;
	grant->underflows = snapshot->underflows;
	grant->outstanding = snapshot->outstanding;
	if (snapshot->errors || (snapshot->status &
	    (FES_DE25_SCANOUT_STATUS_FAULT | FES_DE25_SCANOUT_STATUS_CLOCK_FAULT)))
		return -EIO;
	if (!(snapshot->status & FES_DE25_SCANOUT_STATUS_RUNNING) ||
	    snapshot->accepted != grant->sequence || (snapshot->slots & ~FES_DE25_SCANOUT_SLOTS_MASK))
		return -EPROTO;
	if (snapshot->displayed == grant->displayed_sequence) {
		if (displayed_slot != grant->displayed_slot)
			return -EPROTO;
	} else if (grant->pending && snapshot->displayed == grant->sequence) {
		if ((snapshot->status & FES_DE25_SCANOUT_STATUS_PENDING) ||
		    displayed_slot != grant->pending_slot ||
		    snapshot->current_sequence != grant->sequence || current_slot != grant->pending_slot)
			return -EPROTO;
	} else {
		return -EPROTO;
	}
	if (snapshot->current_sequence == grant->displayed_sequence) {
		if (current_slot != grant->displayed_slot)
			return -EPROTO;
	} else if (grant->pending && snapshot->current_sequence == grant->sequence) {
		if (current_slot != grant->pending_slot)
			return -EPROTO;
	} else {
		return -EPROTO;
	}
	if (grant->pending && snapshot->displayed != grant->sequence) {
		if (!(snapshot->status & FES_DE25_SCANOUT_STATUS_PENDING) ||
		    ((snapshot->slots >> FES_DE25_SCANOUT_PENDING_SLOT_SHIFT) & FES_DE25_SCANOUT_SLOT_MASK) != grant->pending_slot)
			return -EPROTO;
	} else if (snapshot->status & FES_DE25_SCANOUT_STATUS_PENDING) {
		return -EPROTO;
	}
	grant->accepted_sequence = snapshot->accepted;
	if (grant->pending && snapshot->displayed == grant->sequence) {
		grant->displayed_sequence = grant->sequence;
		grant->displayed_slot = grant->pending_slot;
		grant->pending = false;
	}
	return 0;
}

int fes_de25_dma_poll(struct file *file, struct fes_de25_dma_info *info)
{
	struct fes_de25_dma *grant = file->private_data;
	struct fes_de25_snapshot snapshot;
	int ret = 0;

	mutex_lock(&grant->lock);
	if (info->version != FES_DE25_DMA_ABI_VERSION || info->reserved ||
	    info->generation != grant->generation) {
		ret = -EINVAL;
		goto out;
	}
	if (grant->revoked || grant->uncertain) {
		ret = -EBUSY;
		goto out;
	}
	if (grant->exposed) {
		ret = fes_de25_snapshot(grant, &snapshot);
		if (!ret)
			ret = fes_de25_dma_update(grant, &snapshot);
		if (ret)
			grant->uncertain = true;
	}
	if (!ret)
		fes_de25_dma_info_locked(grant, info);
out:
	mutex_unlock(&grant->lock);
	return ret;
}

int fes_de25_dma_publish(struct file *file, const struct fes_de25_dma_publish *publish)
{
	struct fes_de25_dma *grant = file->private_data;
	struct fes_de25_dma_backend *io = &grant->backend;
	struct fes_de25_snapshot snapshot;
	int ret = 0;

	mutex_lock(&grant->lock);
	if (publish->version != FES_DE25_DMA_ABI_VERSION || publish->reserved ||
	    publish->generation != grant->generation || !publish->sequence ||
	    publish->sequence <= grant->sequence || publish->slot >= FES_DE25_DMA_SLOTS) {
		ret = -EINVAL;
		goto out;
	}
	if (grant->revoked || grant->uncertain || grant->pending) {
		ret = -EBUSY;
		goto out;
	}
	if (publish->slot == grant->displayed_slot) {
		ret = -EINVAL;
		goto out;
	}
	if (io->epoch(io->context) != grant->epoch) {
		if (grant->exposed)
			grant->uncertain = true;
		ret = -ESTALE;
		goto out;
	}
	if (grant->exposed) {
		ret = fes_de25_snapshot(grant, &snapshot);
		if (!ret)
			ret = fes_de25_dma_update(grant, &snapshot);
		if (ret)
			goto uncertain;
	}
#define WRITE_STAGE(reg, value) do { \
	ret = io->write32(io->context, FES_DE25_SCANOUT_##reg##_OFFSET, (value)); \
	if (ret) goto out; \
} while (0)
	WRITE_STAGE(GRANT_LO, lower_32_bits(grant->dma));
	WRITE_STAGE(GRANT_HI, upper_32_bits((u64)grant->dma));
	WRITE_STAGE(GRANT_SPAN, FES_DE25_DMA_ALLOCATION_BYTES);
	WRITE_STAGE(GENERATION_LO, lower_32_bits(grant->generation));
	WRITE_STAGE(GENERATION_HI, upper_32_bits(grant->generation));
	WRITE_STAGE(SEQUENCE, publish->sequence);
	WRITE_STAGE(SLOT, publish->slot);
#undef WRITE_STAGE
	dma_wmb();
	ret = io->io_fence(io->context);
	if (ret)
		goto out;
	if (!grant->exposed) {
		if (!try_module_get(THIS_MODULE)) {
			ret = -ENODEV;
			goto out;
		}
		/* Pin before SUBMIT: even a failed write may already enable fetch. */
		kref_get(&grant->refs);
		grant->exposed = true;
	}
	grant->sequence = publish->sequence;
	grant->pending_slot = publish->slot;
	grant->pending = true;
	ret = io->write32(io->context, FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_SUBMIT);
	if (!ret)
		ret = io->io_fence(io->context);
	if (!ret)
		ret = fes_de25_snapshot(grant, &snapshot);
	if (!ret)
		ret = fes_de25_dma_update(grant, &snapshot);
	if (ret)
		goto uncertain;
	goto out;
uncertain:
	grant->uncertain = true;
out:
	mutex_unlock(&grant->lock);
	return ret;
}

int fes_de25_dma_revoke_owned(struct fes_de25_dma *grant)
{
	struct fes_de25_dma_mm *entry;
	bool drop_exposure = false;
	int ret = 0;

	mutex_lock(&grant->revoke_lock);
	mutex_lock(&grant->lock);
	grant->revoked = true;
	mutex_unlock(&grant->lock);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (grant->observer && grant->observer->revoked)
		complete(grant->observer->revoked);
#endif
	/* New mappings now fail. An earlier mmap callback can have installed PTEs
	 * before mmap_region links its VMA into inode->i_mapping. Synchronize past
	 * that link under each recorded mm's write lock, without the grant lock:
	 * mmap takes mm->mmap_lock before grant->lock, so reversing it deadlocks.
	 * Entries cannot be added after revoked and live until the final kref.
	 */
	list_for_each_entry(entry, &grant->mms, link) {
		mmap_write_lock(entry->mm);
		mmap_write_unlock(entry->mm);
	}
	unmap_mapping_range(grant->inode->i_mapping, 0, 0, 1);
	mutex_lock(&grant->lock);
	if (!grant->exposed)
		goto out;
	if (grant->backend.epoch(grant->backend.context) != grant->epoch) {
		ret = -ESTALE;
		goto uncertain;
	}
	ret = grant->backend.write32(grant->backend.context, FES_DE25_SCANOUT_COMMAND_OFFSET,
			FES_DE25_SCANOUT_COMMAND_STOP);
	if (!ret)
		ret = grant->backend.io_fence(grant->backend.context);
	if (!ret) {
		struct fes_de25_snapshot snapshot;

		ret = fes_de25_snapshot(grant, &snapshot);
		if (!ret)
			ret = fes_de25_dma_identity(grant, &snapshot);
		if (!ret && (!(snapshot.status & FES_DE25_SCANOUT_STATUS_DRAINED) ||
		    (snapshot.status & (FES_DE25_SCANOUT_STATUS_RUNNING | FES_DE25_SCANOUT_STATUS_PENDING)) ||
		    snapshot.outstanding))
			ret = -EAGAIN;
	}
	if (ret)
		goto uncertain;
	/* Consume the old generation's drain proof before CLEAR destroys it.
	 * Only this owner may now clear for a later, distinct allocation lease.
	 */
	ret = grant->backend.write32(grant->backend.context, FES_DE25_SCANOUT_COMMAND_OFFSET,
			FES_DE25_SCANOUT_COMMAND_CLEAR);
	if (!ret)
		ret = grant->backend.io_fence(grant->backend.context);
	if (!ret) {
		struct fes_de25_snapshot snapshot;

		ret = fes_de25_snapshot(grant, &snapshot);
		if (!ret && (snapshot.status != FES_DE25_SCANOUT_STATUS_DRAINED ||
		    snapshot.generation || snapshot.dma || snapshot.span || snapshot.accepted ||
		    snapshot.displayed || snapshot.current_sequence || snapshot.slots || snapshot.errors ||
		    snapshot.outstanding || grant->backend.epoch(grant->backend.context) != grant->epoch))
			ret = -ESTALE;
	}
	if (ret)
		goto uncertain;
	grant->exposed = false;
	grant->uncertain = false;
	grant->pending = false;
	drop_exposure = true;
	goto out;
uncertain:
	grant->uncertain = true;
out:
	mutex_unlock(&grant->lock);
	mutex_unlock(&grant->revoke_lock);
	if (drop_exposure) {
		kref_put(&grant->refs, fes_de25_dma_destroy);
		module_put(THIS_MODULE);
	}
	return ret;
}

struct fes_de25_dma *fes_de25_dma_get(struct file *file)
{
	struct fes_de25_dma *grant = file->private_data;

	/* The caller holds a live file reference, so refs cannot already be zero. */
	__module_get(THIS_MODULE);
	kref_get(&grant->refs);
	return grant;
}

void fes_de25_dma_put(struct fes_de25_dma *grant)
{
	kref_put(&grant->refs, fes_de25_dma_destroy);
	module_put(THIS_MODULE);
}

int fes_de25_dma_revoke(struct file *file)
{
	return fes_de25_dma_revoke_owned(file->private_data);
}

static vm_fault_t fes_de25_dma_fault(struct vm_fault *vmf)
{
	/* DMA API installed the initial PTEs. They are never recreated after zap. */
	return VM_FAULT_SIGBUS;
}

static const struct vm_operations_struct fes_de25_dma_vm_ops = {
	.fault = fes_de25_dma_fault,
};

static int fes_de25_dma_mmap(struct file *file, struct vm_area_struct *vma)
{
	struct fes_de25_dma *grant = file->private_data;
	struct fes_de25_dma_mm *entry;
	bool found = false, added = false;
	int ret = 0;

	if (!(vma->vm_flags & VM_SHARED) || (vma->vm_flags & VM_EXEC) ||
	    vma->vm_pgoff > FES_DE25_DMA_ALLOCATION_BYTES / PAGE_SIZE ||
	    vma_pages(vma) > FES_DE25_DMA_ALLOCATION_BYTES / PAGE_SIZE - vma->vm_pgoff)
		return -EINVAL;
	mutex_lock(&grant->lock);
	if (grant->revoked) {
		ret = -EACCES;
		goto out;
	}
	list_for_each_entry(entry, &grant->mms, link)
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
		list_add_tail(&entry->link, &grant->mms);
		added = true;
	}
	/* vma_init supplied the kernel's empty generic dummy operations. This
	 * fresh file mmap callback owns vm_ops; clear that sentinel before the
	 * DMA mapper so any actual architecture-installed operations are visible
	 * to the strict check below. No DMA-specific operations are discarded.
	 */
	vma->vm_ops = NULL;
	ret = dma_mmap_attrs(grant->dev, vma, grant->cpu, grant->dma,
			FES_DE25_DMA_ALLOCATION_BYTES, FES_DE25_DMA_ATTRIBUTES);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	/* Refuse a successful mapping without changing the installed PTEs'
	 * PFNMAP semantics: the mmap error path must unmap them safely.
	 */
	if (!ret && grant->observer && grant->observer->mmap_refuse) {
		ret = -EOPNOTSUPP;
		goto out;
	}
#endif
	if (!ret) {
		/* Only an eager I/O PFN mapping is currently revocable here. An
		 * IOMMU's vm_map_pages mapping can also have no vm_ops, but permits
		 * page pins/aliases that unmap_mapping_range cannot revoke. Never
		 * manufacture PFNMAP flags or infer PFNs from a DMA address.
		 */
		if (vma->vm_ops ||
		    (vma->vm_flags & (VM_IO | VM_PFNMAP)) != (VM_IO | VM_PFNMAP)) {
			ret = -EOPNOTSUPP;
			goto out;
		}
		vm_flags_set(vma, VM_DONTEXPAND | VM_DONTDUMP | VM_DONTCOPY);
		vm_flags_clear(vma, VM_MAYEXEC);
		vma->vm_ops = &fes_de25_dma_vm_ops;
	}
out:
	if (ret && added) {
		list_del(&entry->link);
		mmdrop(entry->mm);
		kfree(entry);
	}
	mutex_unlock(&grant->lock);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	/* Pause only the test callback, after PTE installation but before the
	 * caller links its VMA. This reproduces the actual revoke race window.
	 */
	if (!ret && grant->observer && grant->observer->mmap_entered) {
		complete(grant->observer->mmap_entered);
		if (!wait_for_completion_timeout(grant->observer->mmap_release, 5 * HZ))
			return -ETIMEDOUT;
	}
#endif
	return ret;
}

static long fes_de25_dma_ioctl(struct file *file, unsigned int cmd, unsigned long arg)
{
	struct fes_de25_dma_publish publish;
	struct fes_de25_dma_info info;

	switch (cmd) {
	case FES_DE25_DMA_INFO:
		fes_de25_dma_info(file, &info);
		return copy_to_user((void __user *)arg, &info, sizeof(info)) ? -EFAULT : 0;
	case FES_DE25_DMA_PUBLISH:
		if (copy_from_user(&publish, (void __user *)arg, sizeof(publish)))
			return -EFAULT;
		return fes_de25_dma_publish(file, &publish);
	case FES_DE25_DMA_POLL:
		if (copy_from_user(&info, (void __user *)arg, sizeof(info)))
			return -EFAULT;
		{
			int ret = fes_de25_dma_poll(file, &info);
			if (ret)
				return ret;
		}
		return copy_to_user((void __user *)arg, &info, sizeof(info)) ? -EFAULT : 0;
	case FES_DE25_DMA_REVOKE:
		return fes_de25_dma_revoke(file);
	default:
		return -ENOTTY;
	}
}

static int fes_de25_dma_release(struct inode *inode, struct file *file)
{
	struct fes_de25_dma *grant = file->private_data;

	/* Failed stop/drain leaves the independent exposure reference and module
	 * pin intact, even when every client closes. Never free on a timeout.
	 */
	fes_de25_dma_revoke(file);
	kref_put(&grant->refs, fes_de25_dma_destroy);
	return 0;
}

static const struct file_operations fes_de25_dma_fops = {
	.owner = THIS_MODULE,
	.mmap = fes_de25_dma_mmap,
	.unlocked_ioctl = fes_de25_dma_ioctl,
#ifdef CONFIG_COMPAT
	.compat_ioctl = compat_ptr_ioctl,
#endif
	.release = fes_de25_dma_release,
	.llseek = noop_llseek,
};

struct file *fes_de25_dma_create(struct device *dev, const struct fes_de25_dma_backend *backend)
{
	struct fes_de25_dma *grant;
	struct fes_de25_dma_backend selected;
	struct file *file;
	u64 epoch, generation;
	struct fes_de25_snapshot snapshot;
	u32 identity, version;
	int ret;

	if (!dev || !backend)
		return ERR_PTR(-EINVAL);
	selected = *backend;
	if (!selected.acquire || !selected.release || !selected.epoch ||
	    !selected.read32 || !selected.write32 || !selected.io_fence)
		return ERR_PTR(-EINVAL);
	grant = kzalloc(sizeof(*grant), GFP_KERNEL);
	if (!grant)
		return ERR_PTR(-ENOMEM);
	grant->dev = get_device(dev);
	grant->backend = selected;
	ret = selected.acquire(selected.context);
	if (ret)
		goto fail_unowned;
	epoch = selected.epoch(selected.context);
	if (!epoch) {
		ret = -EOPNOTSUPP;
		goto fail_lease;
	}
	ret = dma_set_mask_and_coherent(dev, DMA_BIT_MASK(FES_DE25_SCANOUT_DMA_ADDRESS_BITS));
	if (ret)
		goto fail_lease;
	generation = fes_de25_dma_next_generation(&next_generation);
	if (!generation) {
		ret = -EOVERFLOW;
		goto fail_lease;
	}
	grant->generation = generation;
	grant->epoch = epoch;
	ret = selected.read32(selected.context, FES_DE25_SCANOUT_ID_OFFSET, &identity);
	if (!ret)
		ret = selected.read32(selected.context, FES_DE25_SCANOUT_VERSION_OFFSET, &version);
	if (!ret && (identity != FES_DE25_SCANOUT_IDENTITY || version != FES_DE25_SCANOUT_VERSION))
		ret = -EPROTONOSUPPORT;
	if (!ret)
		ret = fes_de25_snapshot(grant, &snapshot);
	if (!ret && (snapshot.status != FES_DE25_SCANOUT_STATUS_DRAINED || snapshot.generation ||
	    snapshot.accepted || snapshot.displayed || snapshot.current_sequence || snapshot.slots ||
	    snapshot.outstanding || snapshot.errors || snapshot.dma || snapshot.span ||
	    selected.epoch(selected.context) != epoch))
		ret = -EBUSY;
	if (ret)
		goto fail_lease;
	kref_init(&grant->refs);
	mutex_init(&grant->lock);
	mutex_init(&grant->revoke_lock);
	INIT_LIST_HEAD(&grant->mms);
	grant->cpu = dma_alloc_attrs(dev, FES_DE25_DMA_ALLOCATION_BYTES, &grant->dma, GFP_KERNEL,
		FES_DE25_DMA_ATTRIBUTES);
	if (!grant->cpu) {
		ret = -ENOMEM;
		goto fail_lease;
	}
	if (!IS_ALIGNED(grant->dma, FES_DE25_SCANOUT_GRANT_ALIGNMENT) ||
	    (u64)grant->dma > (1ULL << FES_DE25_SCANOUT_DMA_ADDRESS_BITS) - FES_DE25_DMA_ALLOCATION_BYTES) {
		kref_put(&grant->refs, fes_de25_dma_destroy);
		return ERR_PTR(-ERANGE);
	}
	memset(grant->cpu, 0, FES_DE25_DMA_ALLOCATION_BYTES);
	file = anon_inode_create_getfile("fes-de25-dma", &fes_de25_dma_fops, grant, O_RDWR, NULL);
	if (IS_ERR(file)) {
		kref_put(&grant->refs, fes_de25_dma_destroy);
		return file;
	}
	grant->inode = igrab(file_inode(file));
	i_size_write(grant->inode, FES_DE25_DMA_ALLOCATION_BYTES);
	return file;
fail_lease:
	selected.release(selected.context);
fail_unowned:
	put_device(grant->dev);
	kfree(grant);
	return ERR_PTR(ret);
}

int fes_de25_dma_binding(struct device *dev)
{
	/* No actual fabric device/SID, translation, physical firewall and boot
	 * epoch binding has been qualified. No module/DT/UAPI flag can waive it.
	 */
	return -EOPNOTSUPP;
}

static int fes_de25_dma_probe(struct platform_device *pdev)
{
	return fes_de25_dma_binding(&pdev->dev);
}

static const struct of_device_id fes_de25_dma_match[] = {
	{ .compatible = "fes,de25-dma" },
	{ }
};
MODULE_DEVICE_TABLE(of, fes_de25_dma_match);

static struct platform_driver fes_de25_dma_driver = {
	.probe = fes_de25_dma_probe,
	.driver = {
		.name = "fes-de25-dma",
		.of_match_table = fes_de25_dma_match,
	},
};
module_platform_driver(fes_de25_dma_driver);

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
u64 fes_de25_dma_test_next_generation(atomic64_t *counter)
{
	return fes_de25_dma_next_generation(counter);
}

void fes_de25_dma_test_observe(struct file *file, struct fes_de25_dma_test_observer *observer)
{
	struct fes_de25_dma *grant = file->private_data;

	grant->observer = observer;
}

unsigned int fes_de25_dma_test_mmap_count(struct file *file)
{
	struct fes_de25_dma *grant = file->private_data;
	struct fes_de25_dma_mm *entry;
	unsigned int count = 0;

	mutex_lock(&grant->lock);
	list_for_each_entry(entry, &grant->mms, link)
		count++;
	mutex_unlock(&grant->lock);
	return count;
}
#endif

MODULE_DESCRIPTION("DE25 Linux DMA allocation and lifetime diagnostic; hardware binding closed");
MODULE_LICENSE("GPL");
