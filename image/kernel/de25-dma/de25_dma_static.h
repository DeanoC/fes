/* SPDX-License-Identifier: GPL-2.0 */
/* Private helpers, included only by the permanent owner's implementation. */
#define STATIC_PROFILE "fes,de25-linux-static-expansion"
#define STATIC_SLOT "fes-expansion"

static void owner_clear_static(struct fes_de25_owner *owner)
{
	of_node_put(owner->static_slot);
	owner->static_slot = NULL;
	owner->static_started = false;
	owner->static_pending = false;
}

static int owner_arm_guard(struct fes_de25_owner *owner)
{
	int ret;

	if (owner->next_epoch == U32_MAX)
		return -EOVERFLOW;
	owner->guard = kzalloc(sizeof(*owner->guard), GFP_KERNEL);
	if (!owner->guard)
		return -ENOMEM;
	ret = fes_de25_guard_init(owner->guard, owner->base, FES_DE25_SCANOUT_CONTROL_SPAN);
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (owner->test_fixture && owner->test_guard_template) {
		owner->guard->test_read = owner->test_guard_template->test_read;
		owner->guard->test_write = owner->test_guard_template->test_write;
		owner->guard->test_context = owner->test_guard_template->test_context;
	}
#endif
	if (!ret)
		ret = fes_de25_guard_claim(owner->guard, ++owner->next_epoch);
	if (ret)
		owner_retire_guard(owner);
	else
		owner->ready = true;
	return ret;
}

static bool static_profile(struct device_node *overlay)
{
	struct device_node *profile;

	if (!overlay)
		return false;
	profile = of_get_compatible_child(overlay, STATIC_PROFILE);
	if (!profile)
		return false;
	of_node_put(profile);
	return true;
}

static bool static_properties(struct device_node *node, const char *first, const char *second)
{
	struct property *property;
	u32 timeout;

	for_each_property_of_node(node, property) {
		/* Retain the ordinary runtime's fixed completion budget, never a
		 * caller-supplied bridge/configuration timeout override.
		 */
		if (!strcmp(first, "firmware-name") &&
		    !strcmp(property->name, "config-complete-timeout-us") &&
		    property->length == sizeof(u32) &&
		    !of_property_read_u32(node, property->name, &timeout) && timeout == 30000000)
			continue;
		if (!strcmp(property->name, "name") || !strcmp(property->name, "phandle") ||
		    !strcmp(property->name, "linux,phandle") || !strcmp(property->name, first) ||
		    (second && !strcmp(property->name, second)))
			continue;
		return false;
	}
	return true;
}

static bool static_base_valid(struct device_node *overlay)
{
	struct device_node *slot, *profile;
	const char *firmware;
	bool valid;

	if (!static_properties(overlay, "firmware-name", NULL) ||
	    of_property_count_strings(overlay, "firmware-name") != 1 ||
	    of_property_read_string(overlay, "firmware-name", &firmware) || !*firmware ||
	    of_get_child_count(overlay) != 2)
		return false;
	profile = of_get_compatible_child(overlay, STATIC_PROFILE);
	if (!profile)
		return false;
	valid = static_properties(profile, "compatible", NULL) &&
		of_property_count_strings(profile, "compatible") == 1 && !of_get_child_count(profile);
	of_node_put(profile);
	if (!valid)
		return false;
	slot = of_get_child_by_name(overlay, STATIC_SLOT);
	if (!slot)
		return false;
	valid = of_device_is_compatible(slot, "fpga-region") &&
		of_property_count_strings(slot, "compatible") == 1 &&
		!of_get_child_count(slot) && static_properties(slot, "compatible", NULL);
	of_node_put(slot);
	return valid;
}

static bool static_persona_valid(struct device_node *overlay)
{
	const char *firmware;
	struct property *partial;

	if (!overlay)
		return false;
	partial = of_find_property(overlay, "partial-fpga-config", NULL);
	return partial && !partial->length &&
		static_properties(overlay, "firmware-name", "partial-fpga-config") &&
		!of_get_child_count(overlay) &&
		of_property_count_strings(overlay, "firmware-name") == 1 &&
		!of_property_read_string(overlay, "firmware-name", &firmware) && *firmware;
}

/* Configure one initial persona before any guard epoch or DMA grant exists.
 * Overlay callbacks are serialized by OF. A failed PRE/POST leaves this base
 * unavailable; removing the persona does not permit a second attempt. A new
 * expansion selection requires a new full base configuration.
 */
static int owner_static_notify(struct fes_de25_owner *owner,
		unsigned long action, struct of_overlay_notify_data *nd)
{
	switch (action) {
	case OF_OVERLAY_PRE_APPLY:
		if (owner->static_started || owner->ready || owner->guard)
			return -EIO;
		if (!static_persona_valid(nd->overlay))
			return -EINVAL;
		owner->static_started = true;
		owner->static_pending = true;
		break;
	case OF_OVERLAY_POST_APPLY:
		if (!owner->static_pending || owner->ready || owner->guard)
			return -EIO;
		owner->static_pending = false;
		return owner_arm_guard(owner);
	case OF_OVERLAY_PRE_REMOVE:
		owner->static_pending = false;
		owner->ready = false;
		owner_retire_guard(owner);
		break;
	}
	return 0;
}
