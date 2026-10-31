static void svc_thread_cmd_data_claim_bounded(struct stratix10_svc_controller *ctrl,
				      struct stratix10_svc_data *p_data,
				      struct stratix10_svc_cb_data *cb_data)
{
	struct arm_smccc_res res;
	unsigned long timeout = msecs_to_jiffies(FPGA_CONFIG_DATA_CLAIM_TIMEOUT_MS);
	unsigned long deadline = jiffies + timeout;
	void *buf_claim_addr[4] = {NULL};
	int buf_claim_count = 0;
	int previous_count;

	pr_debug("%s: claim back the submitted buffer\n", __func__);

	do {
		if (kthread_should_stop())
			return;
		previous_count = buf_claim_count;
		ctrl->invoke_fn(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,
				0, 0, 0, 0, 0, 0, 0, &res);
		if (kthread_should_stop())
			return;

		if (res.a0 == INTEL_SIP_SMC_STATUS_OK) {
			if (!res.a1) {
				cb_data->status = BIT(SVC_STATUS_BUFFER_DONE);
				cb_data->kaddr1 = buf_claim_addr[0];
				cb_data->kaddr2 = buf_claim_addr[1];
				cb_data->kaddr3 = buf_claim_addr[2];
				cb_data->kaddr4 = buf_claim_addr[3];
				p_data->chan->scl->receive_cb(p_data->chan->scl,
							      cb_data);
				return;
			}
			if (buf_claim_count < 4)
				buf_claim_addr[buf_claim_count++] = svc_pa_to_va(res.a1);
			if (res.a2 && buf_claim_count < 4)
				buf_claim_addr[buf_claim_count++] = svc_pa_to_va(res.a2);
			if (res.a3 && buf_claim_count < 4)
				buf_claim_addr[buf_claim_count++] = svc_pa_to_va(res.a3);
			if (buf_claim_count > previous_count)
				deadline = jiffies + timeout;
		} else if (res.a0 != INTEL_SIP_SMC_STATUS_BUSY) {
			break;
		}

		/* Busy firmware must not monopolize a CPU or prevent stop. */
		usleep_range(1000, 2000);
	} while (time_before(jiffies, deadline) && !kthread_should_stop());

	/* A stopped client has given up: do not publish a late callback. */
	if (kthread_should_stop())
		return;

	pr_err("%s: buffer claim failed, status 0x%lx\n", __func__, res.a0);
	cb_data->status = BIT(SVC_STATUS_ERROR);
	cb_data->kaddr1 = NULL;
	cb_data->kaddr2 = NULL;
	cb_data->kaddr3 = NULL;
	cb_data->kaddr4 = NULL;
	p_data->chan->scl->receive_cb(p_data->chan->scl, cb_data);
}


static void svc_thread_cmd_data_claim(struct stratix10_svc_controller *ctrl,
				      struct stratix10_svc_data *p_data,
				      struct stratix10_svc_cb_data *cb_data)
{
	struct arm_smccc_res res;
	unsigned long timeout;
	void *buf_claim_addr[4] = {NULL};
	int buf_claim_count = 0;

	if (IS_ENABLED(CONFIG_FES_DE25_DMA_PLATFORM)) {
		svc_thread_cmd_data_claim_bounded(ctrl, p_data, cb_data);
		return;
	}

	reinit_completion(&ctrl->complete_status);
	timeout = msecs_to_jiffies(FPGA_CONFIG_DATA_CLAIM_TIMEOUT_MS);

	pr_debug("%s: claim back the submitted buffer\n", __func__);
	do {
		ctrl->invoke_fn(INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE,
				0, 0, 0, 0, 0, 0, 0, &res);

		if (res.a0 == INTEL_SIP_SMC_STATUS_OK) {
			if (!res.a1) {
				/* Transaction of 4 blocks are now done */
				complete(&ctrl->complete_status);
				cb_data->status = BIT(SVC_STATUS_BUFFER_DONE);
				cb_data->kaddr1 = buf_claim_addr[0];
				cb_data->kaddr2 = buf_claim_addr[1];
				cb_data->kaddr3 = buf_claim_addr[2];
				cb_data->kaddr4 = buf_claim_addr[3];
				p_data->chan->scl->receive_cb(p_data->chan->scl,
				cb_data);
				break;
			}
			if (buf_claim_count < 4) {
				buf_claim_addr[buf_claim_count]
				= svc_pa_to_va(res.a1);
				buf_claim_count++;
			}
			if ((res.a2) && (buf_claim_count < 4)) {
				buf_claim_addr[buf_claim_count]
				= svc_pa_to_va(res.a2);
				buf_claim_count++;
			}
			if ((res.a3) && (buf_claim_count < 4)) {
				buf_claim_addr[buf_claim_count]
				= svc_pa_to_va(res.a3);
				buf_claim_count++;
			}
		}
	} while (res.a0 == INTEL_SIP_SMC_STATUS_OK ||
		 res.a0 == INTEL_SIP_SMC_STATUS_BUSY ||
		 wait_for_completion_timeout(&ctrl->complete_status, timeout));
}
