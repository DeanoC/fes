static int s10_ops_write(struct fpga_manager *mgr, const char *buf,
			 size_t count)
{
	struct s10_priv *priv = mgr->priv;
	struct device *dev = priv->client.dev;
	long wait_status;
	int sent = 0;
	int ret = 0;
	unsigned long deadline = jiffies + msecs_to_jiffies(30000);
	unsigned long remaining, now;
	bool bounded = IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM);

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

	if (ret < 0)
		stratix10_svc_done(priv->chan);

	return ret;
}
