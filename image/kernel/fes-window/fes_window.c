// SPDX-License-Identifier: GPL-2.0
/*
 * fes-window: cached, revocable user mappings of the reserved HPS DDR window.
 *
 * fpga2sdram reads DDR directly, past the A9 L1 caches and the PL310. A
 * process draws into a cached grant mapping and cleans the rows it wrote
 * before the fabric reads them, so no CPU copy sits between drawing and
 * scanout. Revoking a grant removes every process's mapping and writes back
 * its lines, so no late eviction reaches memory a game core now owns.
 *
 * The window base and size are module parameters taken from the shared HPS
 * DDR contract. The module refuses a window that overlaps System RAM.
 */
#include <linux/err.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/io.h>
#include <linux/ioport.h>
#include <linux/miscdevice.h>
#include <linux/mm.h>
#include <linux/module.h>
#include <linux/mount.h>
#include <linux/pseudo_fs.h>
#include <linux/rwsem.h>
#include <linux/slab.h>
#include <linux/uaccess.h>
#include <asm/cacheflush.h>
#include <asm/outercache.h>

#include "fes_window.h"

static unsigned long window_base;
module_param(window_base, ulong, 0444);
MODULE_PARM_DESC(window_base, "Physical base of the reserved HPS DDR window");

static unsigned long window_size;
module_param(window_size, ulong, 0444);
MODULE_PARM_DESC(window_size, "Size in bytes of the reserved HPS DDR window");

#define FES_WINDOW_FS_MAGIC 0x46455357 /* "FESW" */

/* Each grant gets its own inode, so revoking one zaps only its mappings. */
static struct vfsmount *fes_window_mnt;

struct fes_grant {
	struct rw_semaphore lock; /* write: revoke; read: mmap, fault, clean */
	bool revoked;
	phys_addr_t phys;
	size_t size;
	void *kva; /* kernel alias with the user mappings' memory type */
};

/* Write back and invalidate one span through L1, then the PL310. */
static void fes_grant_writeback(struct fes_grant *g, size_t offset, size_t len)
{
	__cpuc_flush_dcache_area(g->kva + offset, len);
	outer_clean_range(g->phys + offset, g->phys + offset + len);
}

static void fes_grant_flush(struct fes_grant *g)
{
	__cpuc_flush_dcache_area(g->kva, g->size);
	outer_flush_range(g->phys, g->phys + g->size);
}

static vm_fault_t fes_grant_fault(struct vm_fault *vmf)
{
	struct fes_grant *g = vmf->vma->vm_private_data;
	vm_fault_t ret = VM_FAULT_SIGBUS;

	/*
	 * Holding the lock across the insert orders this fault against
	 * revoke: an insert either lands before revoke zaps the mapping or
	 * sees the grant revoked.
	 */
	down_read(&g->lock);
	if (!g->revoked && vmf->pgoff < (g->size >> PAGE_SHIFT))
		ret = vmf_insert_pfn(vmf->vma, vmf->address,
				     PHYS_PFN(g->phys) + vmf->pgoff);
	up_read(&g->lock);
	return ret;
}

static const struct vm_operations_struct fes_grant_vm_ops = {
	.fault = fes_grant_fault,
};

static int fes_grant_mmap(struct file *file, struct vm_area_struct *vma)
{
	struct fes_grant *g = file->private_data;
	unsigned long limit = g->size >> PAGE_SHIFT;
	int ret = 0;

	if (!(vma->vm_flags & VM_SHARED) || (vma->vm_flags & VM_EXEC))
		return -EINVAL;
	if (vma->vm_pgoff > limit || vma_pages(vma) > limit - vma->vm_pgoff)
		return -EINVAL;
	down_read(&g->lock);
	if (g->revoked)
		ret = -EACCES;
	up_read(&g->lock);
	if (ret)
		return ret;
	/* Pages are inserted on fault, after the mapping is linked, so revoke sees it. */
	vma->vm_flags |= VM_PFNMAP | VM_IO | VM_DONTEXPAND | VM_DONTDUMP | VM_DONTCOPY;
	vma->vm_flags &= ~VM_MAYEXEC;
	vma->vm_ops = &fes_grant_vm_ops;
	vma->vm_private_data = g;
	return 0;
}

static long fes_grant_clean(struct fes_grant *g, struct fes_window_clean __user *arg)
{
	struct fes_window_clean c;
	u64 span;
	u32 row;
	long ret = 0;

	if (copy_from_user(&c, arg, sizeof(c)))
		return -EFAULT;
	if (c.reserved || !c.width || !c.height || (c.height > 1 && c.stride < c.width))
		return -EINVAL;
	span = c.height > 1 ? (u64)(c.height - 1) * c.stride + c.width : c.width;
	if (c.offset > g->size || span > g->size - c.offset)
		return -EINVAL;

	down_read(&g->lock);
	if (g->revoked) {
		ret = -EACCES;
	} else if (c.height == 1 || c.stride == c.width) {
		fes_grant_writeback(g, c.offset, span);
	} else {
		for (row = 0; row < c.height; row++)
			fes_grant_writeback(g, c.offset + (u64)row * c.stride, c.width);
	}
	up_read(&g->lock);
	return ret;
}

static void fes_grant_revoke(struct fes_grant *g, struct file *file)
{
	down_write(&g->lock);
	if (!g->revoked) {
		g->revoked = true;
		unmap_mapping_range(file->f_mapping, 0, 0, 1);
		fes_grant_flush(g);
	}
	up_write(&g->lock);
}

static long fes_grant_ioctl(struct file *file, unsigned int cmd, unsigned long arg)
{
	struct fes_grant *g = file->private_data;

	switch (cmd) {
	case FES_WINDOW_CLEAN:
		return fes_grant_clean(g, (struct fes_window_clean __user *)arg);
	case FES_WINDOW_REVOKE:
		fes_grant_revoke(g, file);
		return 0;
	default:
		return -ENOTTY;
	}
}

/* The last mapping holds the file, so nothing maps the grant by now. */
static int fes_grant_release(struct inode *inode, struct file *file)
{
	struct fes_grant *g = file->private_data;

	if (!g->revoked)
		fes_grant_flush(g);
	memunmap(g->kva);
	kfree(g);
	return 0;
}

static const struct file_operations fes_grant_fops = {
	.owner = THIS_MODULE,
	.mmap = fes_grant_mmap,
	.unlocked_ioctl = fes_grant_ioctl,
	.release = fes_grant_release,
	.llseek = noop_llseek,
};

static long fes_window_grant(struct fes_window_grant __user *arg)
{
	struct fes_window_grant req;
	struct fes_grant *g;
	struct inode *inode;
	struct file *file;
	int fd;

	if (copy_from_user(&req, arg, sizeof(req)))
		return -EFAULT;
	if (req.reserved || !req.length || !PAGE_ALIGNED(req.offset) ||
	    !PAGE_ALIGNED(req.length))
		return -EINVAL;
	if (req.offset > window_size || req.length > window_size - req.offset)
		return -EINVAL;

	g = kzalloc(sizeof(*g), GFP_KERNEL);
	if (!g)
		return -ENOMEM;
	init_rwsem(&g->lock);
	g->phys = window_base + req.offset;
	g->size = req.length;
	g->kva = memremap(g->phys, g->size, MEMREMAP_WB);
	if (!g->kva) {
		kfree(g);
		return -ENOMEM;
	}

	/* alloc_file_pseudo takes no module reference, but the file's last fput drops one. */
	if (!try_module_get(THIS_MODULE)) {
		memunmap(g->kva);
		kfree(g);
		return -ENODEV;
	}
	inode = alloc_anon_inode(fes_window_mnt->mnt_sb);
	if (IS_ERR(inode)) {
		module_put(THIS_MODULE);
		memunmap(g->kva);
		kfree(g);
		return PTR_ERR(inode);
	}
	file = alloc_file_pseudo(inode, fes_window_mnt, "fes-window-grant",
				 O_RDWR, &fes_grant_fops);
	if (IS_ERR(file)) {
		iput(inode);
		module_put(THIS_MODULE);
		memunmap(g->kva);
		kfree(g);
		return PTR_ERR(file);
	}
	/* From here the file owns the grant; fput frees it through release. */
	file->private_data = g;

	fd = get_unused_fd_flags(O_CLOEXEC);
	if (fd < 0) {
		fput(file);
		return fd;
	}
	req.fd = fd;
	if (copy_to_user(arg, &req, sizeof(req))) {
		put_unused_fd(fd);
		fput(file);
		return -EFAULT;
	}
	fd_install(fd, file);
	return 0;
}

static int fes_window_open(struct inode *inode, struct file *file)
{
	return capable(CAP_SYS_RAWIO) ? 0 : -EPERM;
}

static long fes_window_ioctl(struct file *file, unsigned int cmd, unsigned long arg)
{
	if (cmd != FES_WINDOW_GRANT)
		return -ENOTTY;
	return fes_window_grant((struct fes_window_grant __user *)arg);
}

static const struct file_operations fes_window_fops = {
	.owner = THIS_MODULE,
	.open = fes_window_open,
	.unlocked_ioctl = fes_window_ioctl,
	.llseek = noop_llseek,
};

static struct miscdevice fes_window_dev = {
	.minor = MISC_DYNAMIC_MINOR,
	.name = "fes-window",
	.fops = &fes_window_fops,
	.mode = 0600,
};

static int fes_window_init_fs_context(struct fs_context *fc)
{
	return init_pseudo(fc, FES_WINDOW_FS_MAGIC) ? 0 : -ENOMEM;
}

static struct file_system_type fes_window_fs_type = {
	.name = "fes_window",
	.init_fs_context = fes_window_init_fs_context,
	.kill_sb = kill_anon_super,
};

static int __init fes_window_init(void)
{
	int ret;

	if (!window_size || !PAGE_ALIGNED(window_base) || !PAGE_ALIGNED(window_size) ||
	    window_base + window_size - 1 < window_base) {
		pr_err("fes-window: window_base and window_size must give a page-aligned window\n");
		return -EINVAL;
	}
	if (region_intersects(window_base, window_size, IORESOURCE_SYSTEM_RAM,
			      IORES_DESC_NONE) != REGION_DISJOINT) {
		pr_err("fes-window: window %#lx+%#lx overlaps System RAM\n",
		       window_base, window_size);
		return -EINVAL;
	}
	if (!request_mem_region(window_base, window_size, "fes-window")) {
		pr_err("fes-window: window %#lx+%#lx is already claimed\n",
		       window_base, window_size);
		return -EBUSY;
	}
	fes_window_mnt = kern_mount(&fes_window_fs_type);
	if (IS_ERR(fes_window_mnt)) {
		ret = PTR_ERR(fes_window_mnt);
		goto release_region;
	}
	ret = misc_register(&fes_window_dev);
	if (ret)
		goto unmount;
	return 0;

unmount:
	kern_unmount(fes_window_mnt);
release_region:
	release_mem_region(window_base, window_size);
	return ret;
}

static void __exit fes_window_exit(void)
{
	misc_deregister(&fes_window_dev);
	kern_unmount(fes_window_mnt);
	release_mem_region(window_base, window_size);
}

module_init(fes_window_init);
module_exit(fes_window_exit);

MODULE_LICENSE("GPL");
MODULE_VERSION(FES_WINDOW_VERSION);
MODULE_DESCRIPTION("Cached, revocable user mappings of the FES HPS DDR window");
