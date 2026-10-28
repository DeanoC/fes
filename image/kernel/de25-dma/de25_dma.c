// SPDX-License-Identifier: GPL-2.0
/* Linux-owned, revocable DMA storage. DE25 hardware binding remains closed. */
#include <linux/anon_inodes.h>
#include <linux/atomic.h>
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
	u32 sequence;
	bool revoked, exposed, uncertain;
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	struct fes_de25_dma_test_observer *observer;
#endif
};

static atomic64_t next_generation = ATOMIC64_INIT(0);

static u64 fes_de25_dma_next_generation(void)
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

static void fes_de25_dma_destroy(struct kref *refs)
{
	struct fes_de25_dma *grant = container_of(refs, struct fes_de25_dma, refs);
	struct fes_de25_dma_mm *entry, *next;

	/* Exposed grants hold an independent reference and cannot get here. */
	WARN_ON(grant->exposed);
	dma_free_coherent(grant->dev, FES_DE25_DMA_ALLOCATION_BYTES, grant->cpu, grant->dma);
	list_for_each_entry_safe(entry, next, &grant->mms, link) {
		mmdrop(entry->mm);
		kfree(entry);
	}
	if (grant->inode)
		iput(grant->inode);
	put_device(grant->dev);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (grant->observer)
		grant->observer->freed++;
#endif
	kfree(grant);
}

void fes_de25_dma_info(struct file *file, struct fes_de25_dma_info *info)
{
	struct fes_de25_dma *grant = file->private_data;

	memset(info, 0, sizeof(*info));
	mutex_lock(&grant->lock);
	info->generation = grant->generation;
	info->state = (grant->revoked ? FES_DE25_DMA_REVOKED : 0) |
		(grant->exposed ? FES_DE25_DMA_EXPOSED : 0) |
		(grant->uncertain ? FES_DE25_DMA_UNCERTAIN : 0);
	mutex_unlock(&grant->lock);
	info->version = FES_DE25_DMA_ABI_VERSION;
	info->allocation_bytes = FES_DE25_DMA_ALLOCATION_BYTES;
	info->slot_bytes = FES_DE25_DMA_SLOT_BYTES;
	info->frame_bytes = FES_DE25_DMA_FRAME_BYTES;
	info->width = FES_DE25_DMA_WIDTH;
	info->height = FES_DE25_DMA_HEIGHT;
	info->stride = FES_DE25_DMA_STRIDE;
	info->slots = FES_DE25_DMA_SLOTS;
}

int fes_de25_dma_publish(struct file *file, const struct fes_de25_dma_publish *publish)
{
	struct fes_de25_dma *grant = file->private_data;
	struct fes_de25_dma_descriptor descriptor;
	int ret = 0;

	mutex_lock(&grant->lock);
	if (publish->reserved || publish->generation != grant->generation ||
	    !publish->sequence || publish->sequence <= grant->sequence ||
	    publish->slot >= FES_DE25_DMA_SLOTS) {
		ret = -EINVAL;
		goto out;
	}
	if (grant->revoked || grant->exposed) {
		ret = -EBUSY;
		goto out;
	}
	if (grant->backend.epoch(grant->backend.context) != grant->epoch) {
		ret = -ESTALE;
		goto out;
	}
	if (!try_module_get(THIS_MODULE)) {
		ret = -ENODEV;
		goto out;
	}
	/* Pin before the first callback: a failed start may already have mutated. */
	kref_get(&grant->refs);
	grant->exposed = true;
	grant->sequence = publish->sequence;
	descriptor.dma = grant->dma;
	descriptor.span = FES_DE25_DMA_ALLOCATION_BYTES;
	descriptor.offset = publish->slot * FES_DE25_DMA_SLOT_BYTES;
	descriptor.bytes = FES_DE25_DMA_FRAME_BYTES;
	descriptor.generation = grant->generation;
	descriptor.sequence = publish->sequence;
	dma_wmb();
	ret = grant->backend.start(grant->backend.context, &descriptor);
	if (ret || grant->backend.epoch(grant->backend.context) != grant->epoch) {
		grant->uncertain = true;
		if (!ret)
			ret = -ESTALE;
	}
out:
	mutex_unlock(&grant->lock);
	return ret;
}

int fes_de25_dma_revoke(struct file *file)
{
	struct fes_de25_dma *grant = file->private_data;
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
	ret = grant->backend.stop(grant->backend.context, grant->generation);
	if (!ret)
		ret = grant->backend.drain(grant->backend.context, grant->generation);
	if (!ret && grant->backend.epoch(grant->backend.context) != grant->epoch)
		ret = -ESTALE;
	if (ret)
		goto uncertain;
	grant->exposed = false;
	grant->uncertain = false;
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
	bool found = false;
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
	}
	ret = dma_mmap_coherent(grant->dev, vma, grant->cpu, grant->dma,
			FES_DE25_DMA_ALLOCATION_BYTES);
	if (!ret) {
		/* Do not replace an architecture's fault-driven DMA mapping policy. */
		if (vma->vm_ops) {
			ret = -EOPNOTSUPP;
			goto out;
		}
		vm_flags_set(vma, VM_DONTEXPAND | VM_DONTDUMP | VM_DONTCOPY);
		vm_flags_clear(vma, VM_MAYEXEC);
		vma->vm_ops = &fes_de25_dma_vm_ops;
	}
out:
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
	.compat_ioctl = compat_ptr_ioctl,
	.release = fes_de25_dma_release,
	.llseek = noop_llseek,
};

struct file *fes_de25_dma_create(struct device *dev, const struct fes_de25_dma_backend *backend)
{
	struct fes_de25_dma *grant;
	struct file *file;
	u64 epoch, generation;
	int ret;

	if (!backend || !backend->epoch || !backend->start || !backend->stop || !backend->drain)
		return ERR_PTR(-EINVAL);
	epoch = backend->epoch(backend->context);
	if (!epoch)
		return ERR_PTR(-EOPNOTSUPP);
	ret = dma_set_mask_and_coherent(dev, DMA_BIT_MASK(32));
	if (ret)
		return ERR_PTR(ret);
	generation = fes_de25_dma_next_generation();
	if (!generation)
		return ERR_PTR(-EOVERFLOW);
	grant = kzalloc(sizeof(*grant), GFP_KERNEL);
	if (!grant)
		return ERR_PTR(-ENOMEM);
	grant->dev = get_device(dev);
	grant->backend = *backend;
	grant->generation = generation;
	grant->epoch = epoch;
	kref_init(&grant->refs);
	mutex_init(&grant->lock);
	mutex_init(&grant->revoke_lock);
	INIT_LIST_HEAD(&grant->mms);
	grant->cpu = dma_alloc_coherent(dev, FES_DE25_DMA_ALLOCATION_BYTES, &grant->dma, GFP_KERNEL);
	if (!grant->cpu) {
		put_device(grant->dev);
		kfree(grant);
		return ERR_PTR(-ENOMEM);
	}
	if (!IS_ALIGNED(grant->dma, 32) ||
	    (u64)grant->dma + FES_DE25_DMA_ALLOCATION_BYTES > (1ULL << 32)) {
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
void fes_de25_dma_test_observe(struct file *file, struct fes_de25_dma_test_observer *observer)
{
	struct fes_de25_dma *grant = file->private_data;

	grant->observer = observer;
}
#endif

MODULE_DESCRIPTION("DE25 Linux DMA allocation and lifetime diagnostic; hardware binding closed");
MODULE_LICENSE("GPL");
