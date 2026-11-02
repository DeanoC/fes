static int s10_ops_write_init(struct fpga_manager *mgr,
			      struct fpga_image_info *info,
			      const char *buf, size_t count)
{
	struct s10_priv *priv = mgr->priv;
	struct device *dev = priv->client.dev;
	struct stratix10_svc_command_config_type ctype;
	uint i;
	int ret;

	/* A stopped worker is not a fence for an uncertain SDM transfer.
	 * Keep its buffers and reject even a fresh userspace loader instance.
	 */
	if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM) && priv->config_uncertain)
		return -EIO;

	ctype.flags = 0;
	if (info->flags & FPGA_MGR_PARTIAL_RECONFIG) {
		dev_dbg(dev, "Requesting partial reconfiguration.\n");
		ctype.flags |= BIT(COMMAND_RECONFIG_FLAG_PARTIAL);
	} else if (info->flags & FPGA_MGR_BITSTREAM_AUTHENTICATE) {
		if (priv->fw_version == INVALID_FIRMWARE_VERSION) {
			dev_err(dev, "FW doesn't support\n");
			return -EINVAL;
		}

		dev_dbg(dev, "Requesting bitstream authentication.\n");
		ctype.flags |= BIT(COMMAND_AUTHENTICATE_BITSTREAM);
	} else {
		dev_dbg(dev, "Requesting full reconfiguration.\n");
	}

	if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM)) {
		priv->config_uncertain = true;
		priv->config_active = true;
		priv->status = 0;
	}
	reinit_completion(&priv->status_return_completion);
	ret = s10_svc_send_msg(priv, COMMAND_RECONFIG,
			       &ctype, sizeof(ctype),
			       s10_receive_callback);
	if (ret < 0)
		goto init_error;

	ret = wait_for_completion_timeout(
		&priv->status_return_completion, S10_RECONFIG_TIMEOUT);
	if (!ret) {
		dev_err(dev, "timeout waiting for RECONFIG_REQUEST\n");
		ret = -ETIMEDOUT;
		goto init_error;
	}

	ret = 0;
	if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM) &&
	    test_and_clear_bit(SVC_STATUS_ERROR, &priv->status)) {
		ret = -EFAULT;
		goto init_error;
	}
	if (!test_and_clear_bit(SVC_STATUS_OK, &priv->status)) {
		ret = -ETIMEDOUT;
		goto init_error;
	}

	if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM))
		priv->write_deadline = jiffies + msecs_to_jiffies(30000);

	/* Init buffer lock */
	for (i = 0; i < NUM_SVC_BUFS; i++) {
		priv->svc_bufs[i].lock = 0;
	}

	goto init_done;

init_error:
	if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM))
		priv->config_active = false;
	stratix10_svc_done(priv->chan);
init_done:
	return ret;
}

static int s10_ops_write(struct fpga_manager *mgr, const char *buf,
			 size_t count)
{
	struct s10_priv *priv = mgr->priv;
	struct device *dev = priv->client.dev;
	long wait_status;
	int sent = 0;
	int ret = 0;
	unsigned long deadline = priv->write_deadline;
	unsigned long remaining, now;
	bool bounded = IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM);

	if (bounded && !priv->config_active)
		return -EIO;

	/*
	 * Loop waiting for buffers to be returned.  When a buffer is returned,
	 * reuse it to send more data or free if if all data has been sent.
	 */
	while (true) {
		/* Bound total transfer time, including full queues and callbacks.
		 * This is not an SDM abort or proof that its DMA has drained.
		 */
		if (bounded && time_after_eq(jiffies, deadline)) {
			ret = -ETIMEDOUT;
			break;
		}
		reinit_completion(&priv->status_return_completion);

		if (count > 0) {
			sent = s10_send_buf(mgr, buf, count);
			if (sent < 0) {
				if (!bounded)
					continue;
				if (sent != -ENOBUFS) {
					ret = sent;
					break;
				}
				/* Backpressure waits for a callback instead of spinning. */
			} else {
				count -= sent;
				buf += sent;
			}
		} else {
			if (s10_get_unlocked_buffer_count(mgr) == NUM_SVC_BUFS)
				return 0;

			ret = s10_svc_send_msg(
				priv, COMMAND_RECONFIG_DATA_CLAIM,
				NULL, 0, s10_receive_callback);
			if (ret < 0)
				break;
		}

		remaining = S10_BUFFER_TIMEOUT;
		if (bounded) {
			now = jiffies;
			if (time_after_eq(now, deadline)) {
				ret = -ETIMEDOUT;
				break;
			}
			remaining = min(remaining, deadline - now);
		}
		wait_status = wait_for_completion_timeout(
				&priv->status_return_completion, remaining);

		/* An error must win over a simultaneous progress notification. */
		if (bounded && test_and_clear_bit(SVC_STATUS_ERROR, &priv->status)) {
			ret = -EFAULT;
			break;
		}

		if (test_and_clear_bit(SVC_STATUS_BUFFER_DONE, &priv->status) ||
		    test_and_clear_bit(SVC_STATUS_BUFFER_SUBMITTED,
				       &priv->status)) {
			ret = 0;
			continue;
		}

		if (test_and_clear_bit(SVC_STATUS_ERROR, &priv->status)) {
			dev_err(dev, "ERROR - giving up - SVC_STATUS_ERROR\n");
			ret = -EFAULT;
			break;
		}

		if (!wait_status) {
			dev_err(dev, "timeout waiting for svc layer buffers\n");
			ret = -ETIMEDOUT;
			break;
		}
	}

	if (ret < 0) {
		if (bounded)
			priv->config_active = false;
		stratix10_svc_done(priv->chan);
	}

	return ret;
}

static int s10_ops_write_complete(struct fpga_manager *mgr,
				  struct fpga_image_info *info)
{
	struct s10_priv *priv = mgr->priv;
	struct device *dev = priv->client.dev;
	unsigned long timeout, deadline, now;
	bool bounded = IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM);
	bool completed = false;
	int ret;

	if (bounded && !priv->config_active)
		return -EIO;

	/* The time taken to process this is close to 600ms
	 * This MUST be increased over 1 second
	 */
	timeout = S10_RECONFIG_TIMEOUT;
	deadline = jiffies + timeout;

	do {
		if (bounded) {
			now = jiffies;
			if (time_after_eq(now, deadline)) {
				ret = -ETIMEDOUT;
				break;
			}
			timeout = deadline - now;
		}
		reinit_completion(&priv->status_return_completion);

		ret = s10_svc_send_msg(priv, COMMAND_RECONFIG_STATUS,
				       NULL, 0, s10_receive_callback);
		if (ret < 0)
			break;

		if (bounded) {
			now = jiffies;
			if (time_after_eq(now, deadline)) {
				ret = -ETIMEDOUT;
				break;
			}
			timeout = deadline - now;
		}
		ret = wait_for_completion_timeout(
			&priv->status_return_completion, timeout);
		if (!ret) {
			dev_err(dev,
				"timeout waiting for RECONFIG_COMPLETED\n");
			ret = -ETIMEDOUT;
			break;
		}
		/* Not error or timeout, so ret is # of jiffies until timeout */
		if (!bounded)
			timeout = ret;
		ret = 0;

		if (bounded && test_and_clear_bit(SVC_STATUS_ERROR, &priv->status)) {
			ret = -EFAULT;
			break;
		}
		if (test_and_clear_bit(SVC_STATUS_COMPLETED, &priv->status)) {
			if (bounded && s10_get_unlocked_buffer_count(mgr) != NUM_SVC_BUFS)
				ret = -EIO;
			else
				completed = true;
			break;
		}

		if (test_and_clear_bit(SVC_STATUS_ERROR, &priv->status)) {
			dev_err(dev, "ERROR - giving up - SVC_STATUS_ERROR\n");
			ret = -EFAULT;
			break;
		}
	} while (1);

	stratix10_svc_done(priv->chan);
	if (bounded) {
		priv->config_active = false;
		/* The joined worker may have delivered a final error callback. */
		if (!ret) {
			if (test_and_clear_bit(SVC_STATUS_ERROR, &priv->status))
				ret = -EFAULT;
			else if (!completed || s10_get_unlocked_buffer_count(mgr) != NUM_SVC_BUFS)
				ret = -EIO;
			else
				priv->config_uncertain = false;
		}
	}

	return ret;
}
