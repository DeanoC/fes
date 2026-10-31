// SPDX-License-Identifier: GPL-2.0
/* Permanent platform owner and early OF-overlay exclusion. Default-off candidate. */
#include <linux/capability.h>
#include <linux/file.h>
#include <linux/fs.h>
#include <linux/io.h>
#include <linux/iommu.h>
#include <linux/miscdevice.h>
#include <linux/module.h>
#include <linux/mutex.h>
#include <linux/notifier.h>
#include <linux/of.h>
#include <linux/of_platform.h>
#include <linux/platform_device.h>
#include <linux/slab.h>

#include "de25_dma_internal.h"
#include "de25_dma_guard.h"
#include "de25_dma_owner.h"

#if IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM)
/* Deliberately pinned-kernel, built-in-only API. The owner must inspect the
 * actual managed supplier graph, not guess dependencies from DT properties.
 */
#include "../../base/base.h"
#endif

struct fes_de25_owner {
	struct device *dev;
	struct iommu_domain *domain;
	struct device_node *region;
	void __iomem *base;
	struct mutex lock, allocation_lock;
	struct notifier_block notifier;
	struct miscdevice misc;
	struct fes_de25_dma_guard *guard;
	struct fes_de25_dma *active;
	u32 next_epoch;
	bool ready, leased;
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	bool test_fixture;
	struct device_node test_region;
#endif
};

static int owner_acquire(void *context)
{
	struct fes_de25_owner *owner = context;
	int ret = 0;

	mutex_lock(&owner->lock);
	if (owner->leased)
		ret = -EBUSY;
	else if (!owner->ready)
		ret = -ENODEV;
	else
		owner->leased = true;
	mutex_unlock(&owner->lock);
	return ret;
}

static void owner_release(void *context)
{
	struct fes_de25_owner *owner = context;

	mutex_lock(&owner->lock);
	owner->leased = false;
	mutex_unlock(&owner->lock);
}

static u64 owner_epoch(void *context)
{
	struct fes_de25_owner *owner = context;

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (owner->test_fixture)
		return fes_de25_guard_epoch(owner->guard);
#endif
	if (iommu_get_domain_for_dev(owner->dev) != owner->domain)
		return 0;
	return fes_de25_guard_epoch(owner->guard);
}

static int owner_read(void *context, u32 offset, u32 *value)
{
	return fes_de25_guard_read(((struct fes_de25_owner *)context)->guard, offset, value);
}

static int owner_write(void *context, u32 offset, u32 value)
{
	return fes_de25_guard_write(((struct fes_de25_owner *)context)->guard, offset, value);
}

static int owner_fence(void *context)
{
	return fes_de25_guard_fence(((struct fes_de25_owner *)context)->guard);
}

static struct file *owner_allocate(struct fes_de25_owner *owner)
{
	struct fes_de25_dma_backend backend = {
		.acquire = owner_acquire, .release = owner_release, .epoch = owner_epoch,
		.read32 = owner_read, .write32 = owner_write, .io_fence = owner_fence,
		.context = owner,
	};
	struct file *file;

	mutex_lock(&owner->allocation_lock);
	if (owner->active) {
		file = ERR_PTR(-EBUSY);
		goto out;
	}
	file = fes_de25_dma_create(owner->dev, &backend);
	if (!IS_ERR(file))
		owner->active = fes_de25_dma_get(file);
out:
	mutex_unlock(&owner->allocation_lock);
	return file;
}

static int owner_reap(struct fes_de25_owner *owner)
{
	int ret = 0;

	mutex_lock(&owner->allocation_lock);
	if (owner->active) {
		ret = fes_de25_dma_revoke_owned(owner->active);
		if (!ret) {
			fes_de25_dma_put(owner->active);
			owner->active = NULL;
		}
	}
	mutex_unlock(&owner->allocation_lock);
	return ret;
}

static long owner_ioctl(struct file *control, unsigned int command, unsigned long argument)
{
	struct fes_de25_owner *owner = container_of(control->private_data,
			struct fes_de25_owner, misc);
	struct file *file;
	int fd;

	if (!capable(CAP_SYS_RAWIO))
		return -EPERM;
	if (argument)
		return -EINVAL;
	if (command == FES_DE25_DMA_OWNER_REAP)
		return owner_reap(owner);
	if (command != FES_DE25_DMA_OWNER_ALLOC)
		return -ENOTTY;
	fd = get_unused_fd_flags(O_CLOEXEC);
	if (fd < 0)
		return fd;
	file = owner_allocate(owner);
	if (IS_ERR(file)) {
		put_unused_fd(fd);
		return PTR_ERR(file);
	}
	fd_install(fd, file);
	return fd;
}

static const struct file_operations owner_fops = {
	.owner = THIS_MODULE,
	.unlocked_ioctl = owner_ioctl,
#ifdef CONFIG_COMPAT
	.compat_ioctl = owner_ioctl,
#endif
	.llseek = noop_llseek,
};

static bool ancestor(const struct device_node *parent, const struct device_node *node)
{
	for (; node; node = node->parent)
		if (node == parent)
			return true;
	return false;
}

static void owner_retire_guard(struct fes_de25_owner *owner)
{
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (!owner->test_fixture)
#endif
		kfree(owner->guard);
	owner->guard = NULL;
}

static bool supplier_target(struct fes_de25_owner *owner, struct device_node *target)
{
	struct device_node *node;
	bool overlaps;
	int i;

	node = of_parse_phandle(owner->dev->of_node, "iommus", 0);
	overlaps = node && (ancestor(target, node) || ancestor(node, target));
	of_node_put(node);
	if (overlaps)
		return true;
	node = of_parse_phandle(owner->region, "fpga-mgr", 0);
	overlaps = node && (ancestor(target, node) || ancestor(node, target));
	of_node_put(node);
	if (overlaps)
		return true;
	for (i = 0; ; i++) {
		node = of_parse_phandle(owner->region, "fpga-bridges", i);
		if (!node)
			break;
		overlaps = ancestor(target, node) || ancestor(node, target);
		of_node_put(node);
		if (overlaps)
			return true;
	}
	return false;
}

static bool topology_override(struct device_node *overlay)
{
	static const char * const properties[] = {
		"fpga-bridges", "fpga-mgr", "compatible", "status", "ranges",
	};
	unsigned int i;

	for (i = 0; i < ARRAY_SIZE(properties); i++)
		if (of_find_property(overlay, properties[i], NULL))
			return true;
	return false;
}

static int owner_notify(struct notifier_block *nb, unsigned long action, void *data)
{
	struct fes_de25_owner *owner = container_of(nb, struct fes_de25_owner, notifier);
	struct of_overlay_notify_data *nd = data;
	struct device_node *profile;
	const char *firmware;
	int ret = 0;

	/* Dedicated overlays target the region itself. No overlay may mutate the
	 * permanent owner, its ancestors, or region ancestors behind this owner.
	 */
	if (nd->target != owner->region) {
		if ((action == OF_OVERLAY_PRE_APPLY || action == OF_OVERLAY_PRE_REMOVE) &&
		    (ancestor(nd->target, owner->region) ||
		     ancestor(nd->target, owner->dev->of_node) ||
		     supplier_target(owner, nd->target)))
			return notifier_from_errno(-EBUSY);
		/* Descendant overlay changes may remove a live persona's devices. */
		if (!ancestor(owner->region, nd->target))
			return NOTIFY_OK;
	}
	mutex_lock(&owner->lock);
	switch (action) {
	case OF_OVERLAY_PRE_APPLY:
	case OF_OVERLAY_PRE_REMOVE:
		/* The admitted bridge topology is permanent. A per-image override
		 * could introduce an unprotected supplier after probe validation.
		 */
		if (action == OF_OVERLAY_PRE_APPLY && nd->overlay &&
		    topology_override(nd->overlay)) {
			ret = -EINVAL;
			break;
		}
		if (owner->leased) {
			ret = -EBUSY;
			break;
		}
		/* Reserve exclusion before the stock region notifier disables bridges
		 * or the OF core detaches devices. A later failure stays unavailable;
		 * no POST event or old register contents can restore the old profile.
		 */
		owner->ready = false;
		owner_retire_guard(owner);
		break;
	case OF_OVERLAY_POST_APPLY:
		if (nd->target != owner->region || owner->leased || owner->ready ||
		    !nd->overlay || of_property_read_string(nd->overlay, "firmware-name", &firmware) ||
		    !*firmware)
			break;
		profile = of_get_compatible_child(nd->overlay, "fes,de25-linux-dma-scanout");
		if (!profile)
			break;
		of_node_put(profile);
		if (owner->next_epoch == U32_MAX) {
			ret = -EOVERFLOW;
			break;
		}
		owner->guard = kzalloc(sizeof(*owner->guard), GFP_KERNEL);
		if (!owner->guard) {
			ret = -ENOMEM;
			break;
		}
		ret = fes_de25_guard_init(owner->guard, owner->base, FES_DE25_SCANOUT_CONTROL_SPAN);
		if (!ret)
			ret = fes_de25_guard_claim(owner->guard, ++owner->next_epoch);
		if (ret)
			owner_retire_guard(owner);
		else
			owner->ready = true;
		break;
	}
	mutex_unlock(&owner->lock);
	return notifier_from_errno(ret);
}

/* Only a permanent base-tree device and protected built-in suppliers can own
 * the translation lifetime. get_device/devm references cannot replace binding.
 * Candidate kernels must disable sysfs unbind on these supplier drivers; no DT
 * property or module parameter can waive these checks.
 */
static bool permanent_node(struct device_node *node)
{
	if (!node)
		return false;
	for (; node; node = node->parent)
		if (of_node_check_flag(node, OF_DYNAMIC) || of_node_check_flag(node, OF_OVERLAY))
			return false;
	return true;
}

#if IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM)
static int protected_dependencies(struct device *dev, struct device *probe,
		unsigned int depth, unsigned int *remaining, bool supplier)
{
	struct device_driver *driver;
	struct device_link *link;
	int ret = 0;

	if (!dev)
		return 0;
	if (depth == 32 || !*remaining)
		return -ELOOP;
	--*remaining;
	if (dev->of_node && !permanent_node(dev->of_node))
		return -EOPNOTSUPP;
	if (dev != probe && !device_trylock(dev))
		return -EPROBE_DEFER;
	driver = dev->driver;
	if (supplier && !driver)
		ret = -EPROBE_DEFER;
	else if (driver && (driver->owner || !driver->suppress_bind_attrs))
		ret = -EOPNOTSUPP;
	if (dev != probe)
		device_unlock(dev);
	if (ret)
		return ret;
	list_for_each_entry_rcu(link, &dev->links.suppliers, c_node, device_links_read_lock_held()) {
		ret = protected_dependencies(link->supplier, probe, depth + 1, remaining, true);
		if (ret)
			return ret;
	}
	ret = protected_dependencies(dev->parent, probe, depth + 1, remaining, false);
	if (ret)
		return ret;
	/* Reject a binding changed while its own dependencies were checked. */
	if (dev != probe && !device_trylock(dev))
		return -EPROBE_DEFER;
	ret = dev->driver == driver ? 0 : -EPROBE_DEFER;
	if (dev != probe)
		device_unlock(dev);
	return ret;
}
#endif

static int protected_driver_chain(struct device *dev, struct device *probe)
{
#if IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM)
	unsigned int remaining = 256;
	int index, ret;

	/* Probe already holds its own device lock. Never wait on another device
	 * under it: teardown can take the reverse order. SRCU retains link nodes
	 * while walking parents and transitive suppliers; busy bindings defer.
	 */
	index = device_links_read_lock();
	ret = protected_dependencies(dev, probe, 0, &remaining, false);
	device_links_read_unlock(index);
	return ret;
#else
	return -EOPNOTSUPP;
#endif
}

static int protected_supplier(struct device_node *node)
{
	struct platform_device *pdev;
	int ret;

	if (!permanent_node(node))
		return -EOPNOTSUPP;
	pdev = of_find_device_by_node(node);
	if (!pdev)
		return -EPROBE_DEFER;
	ret = protected_driver_chain(&pdev->dev, NULL);
	/* Recheck the supplier after its ancestor bindings have been checked. */
	if (!ret) {
		if (!device_trylock(&pdev->dev))
			ret = -EPROBE_DEFER;
		else {
			ret = pdev->dev.driver ? 0 : -EPROBE_DEFER;
			device_unlock(&pdev->dev);
		}
	}
	put_device(&pdev->dev);
	return ret;
}

static int owner_group_member(struct device *dev, void *expected)
{
	return dev == expected ? 0 : -EOPNOTSUPP;
}

static int owner_validate(struct platform_device *pdev, struct fes_de25_owner *owner)
{
	struct device *dev = &pdev->dev;
	struct of_phandle_args spec;
	struct iommu_group *group;
	struct device_node *node;
	int count, i, ret;

	if (!permanent_node(dev->of_node) || !dev->driver || !dev->driver->suppress_bind_attrs)
		return -EOPNOTSUPP;
	ret = protected_driver_chain(dev, dev);
	if (ret)
		return ret;
	owner->domain = iommu_get_domain_for_dev(dev);
	if (!owner->domain || (owner->domain->type != IOMMU_DOMAIN_DMA &&
	    owner->domain->type != IOMMU_DOMAIN_DMA_FQ))
		return -EOPNOTSUPP;
	group = iommu_group_get(dev);
	if (!group)
		return -EOPNOTSUPP;
	ret = iommu_group_for_each_dev(group, dev, owner_group_member);
	iommu_group_put(group);
	if (ret)
		return ret;
	if (of_count_phandle_with_args(dev->of_node, "iommus", "#iommu-cells") != 1)
		return -EINVAL;
	ret = of_parse_phandle_with_args(dev->of_node, "iommus", "#iommu-cells", 0, &spec);
	if (ret)
		return ret;
	ret = spec.args_count != 1 || spec.args[0] != FES_DE25_SCANOUT_SMMU_STREAM_ID ||
		!of_device_is_compatible(spec.np, "arm,smmu-v3") ? -EINVAL : protected_supplier(spec.np);
	of_node_put(spec.np);
	if (ret)
		return ret;
	owner->region = of_parse_phandle(dev->of_node, "fes,fpga-region", 0);
	if (!owner->region)
		return -EINVAL;
	if (!of_device_is_compatible(owner->region, "fpga-region") ||
	    !owner->region->parent || owner->region->parent->parent)
		return -EOPNOTSUPP;
	ret = protected_supplier(owner->region);
	if (ret)
		return ret;
	/* This first owner supports a direct, permanent region/manager topology. */
	node = of_parse_phandle(owner->region, "fpga-mgr", 0);
	if (!node)
		return -EINVAL;
	ret = protected_supplier(node);
	of_node_put(node);
	if (ret)
		return ret;
	count = of_count_phandle_with_args(owner->region, "fpga-bridges", NULL);
	/* The Terasic Agilex5 base region has no Linux bridge list; its manager
	 * delegates configuration sequencing to SDM. Preserve that topology.
	 */
	if (count == -ENOENT)
		count = 0;
	if (count < 0)
		return count;
	for (i = 0; i < count; i++) {
		node = of_parse_phandle(owner->region, "fpga-bridges", i);
		ret = protected_supplier(node);
		of_node_put(node);
		if (ret)
			return ret;
	}
	return 0;
}

int fes_de25_owner_probe(struct platform_device *pdev)
{
	struct fes_de25_owner *owner;
	struct resource *resource;
	int ret;

	if (!IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM))
		return -EOPNOTSUPP;
	owner = devm_kzalloc(&pdev->dev, sizeof(*owner), GFP_KERNEL);
	if (!owner)
		return -ENOMEM;
	owner->dev = &pdev->dev;
	mutex_init(&owner->lock);
	mutex_init(&owner->allocation_lock);
	ret = owner_validate(pdev, owner);
	if (ret)
		goto put_region;
	resource = platform_get_resource(pdev, IORESOURCE_MEM, 0);
	if (!resource || resource->start != FES_DE25_SCANOUT_ARCHITECTURAL_LW_BASE +
	    FES_DE25_SCANOUT_PERIPHERAL_OFFSET || resource_size(resource) != FES_DE25_SCANOUT_CONTROL_SPAN) {
		ret = -EINVAL;
		goto put_region;
	}
	/* Mapping reserves the resource but performs no fabric access at probe. */
	owner->base = devm_ioremap_resource(&pdev->dev, resource);
	if (IS_ERR(owner->base)) {
		ret = PTR_ERR(owner->base);
		goto put_region;
	}
	owner->notifier.notifier_call = owner_notify;
	owner->notifier.priority = 100; /* before stock of-fpga-region (priority 0) */
	ret = of_overlay_notifier_register(&owner->notifier);
	if (ret)
		goto put_region;
	owner->misc = (struct miscdevice) {
		.minor = MISC_DYNAMIC_MINOR, .name = "fes-de25-scanout", .fops = &owner_fops,
		.parent = &pdev->dev, .mode = 0600,
	};
	ret = misc_register(&owner->misc);
	if (ret) {
		of_overlay_notifier_unregister(&owner->notifier);
		goto put_region;
	}
	platform_set_drvdata(pdev, owner);
	return 0;
put_region:
	of_node_put(owner->region);
	return ret;
}

void fes_de25_owner_remove(struct platform_device *pdev)
{
	struct fes_de25_owner *owner = platform_get_drvdata(pdev);

	/* Permanent base nodes, no unbind attributes, built-in protected suppliers,
	 * and module pins on files/owner/exposure prevent supported active removal.
	 */
	WARN_ON(owner->leased || owner->active);
	misc_deregister(&owner->misc);
	of_overlay_notifier_unregister(&owner->notifier);
	owner_retire_guard(owner);
	of_node_put(owner->region);
}

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
struct fes_de25_owner *fes_de25_owner_test_create(struct device *dev,
		struct fes_de25_dma_guard *guard)
{
	struct fes_de25_owner *owner = kzalloc(sizeof(*owner), GFP_KERNEL);

	if (!owner)
		return NULL;
	owner->dev = dev;
	owner->test_fixture = true;
	owner->region = &owner->test_region;
	owner->guard = guard;
	owner->ready = !!guard;
	mutex_init(&owner->lock);
	mutex_init(&owner->allocation_lock);
	return owner;
}

struct file *fes_de25_owner_test_allocate(struct fes_de25_owner *owner)
{
	return owner_allocate(owner);
}

int fes_de25_owner_test_reap(struct fes_de25_owner *owner)
{
	return owner_reap(owner);
}

int fes_de25_owner_test_notify(struct fes_de25_owner *owner, unsigned long action)
{
	struct of_overlay_notify_data data = { .target = owner->region };

	return notifier_to_errno(owner_notify(&owner->notifier, action, &data));
}

int fes_de25_owner_test_override(struct fes_de25_owner *owner, const char *name)
{
	struct property property = { .name = (char *)name };
	struct device_node overlay = { .properties = &property };
	struct of_overlay_notify_data data = { .target = owner->region, .overlay = &overlay };

	return notifier_to_errno(owner_notify(&owner->notifier, OF_OVERLAY_PRE_APPLY, &data));
}

int fes_de25_owner_test_ancestor(struct fes_de25_owner *owner, unsigned long action)
{
	struct device_node parent = { };
	struct of_overlay_notify_data data = { .target = &parent };
	int ret;

	owner->test_region.parent = &parent;
	ret = notifier_to_errno(owner_notify(&owner->notifier, action, &data));
	owner->test_region.parent = NULL;
	return ret;
}

int fes_de25_owner_test_dependencies(struct device *dev)
{
	return protected_driver_chain(dev, NULL);
}

void fes_de25_owner_test_destroy(struct fes_de25_owner *owner)
{
	WARN_ON(owner->leased || owner->active);
	kfree(owner);
}
#endif
