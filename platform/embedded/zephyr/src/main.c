/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Hardware-independent base-services experiment. No FPGA programming path. */
#include <zephyr/device.h>
#include <zephyr/fs/fs.h>
#include <zephyr/kernel.h>
#include <zephyr/net/socket.h>
#include <zephyr/net/net_if.h>
#include <zephyr/storage/disk_access.h>
#include <ff.h>

#include <errno.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define SERVICE_PORT 4242
#define REQUEST_BYTES 128
#define RESPONSE_BYTES 768
#define CONFIG_BYTES 128

static FATFS fat_fs;
static struct fs_mount_t mount = {
    .type = FS_FATFS,
    .fs_data = &fat_fs,
    .mnt_point = "/nvme0n0:",
    .flags = FS_MOUNT_FLAG_READ_ONLY | FS_MOUNT_FLAG_NO_FORMAT,
};

static bool storage_ready;
static bool config_ready;
static int storage_error;
static uint32_t last_sequence;
static uint16_t input_buttons;
static char boot_config[CONFIG_BYTES];
static char request[REQUEST_BYTES];
static char response[RESPONSE_BYTES];

static void load_boot_config(void)
{
    uint32_t sectors = 0, sector_bytes = 0;
    struct fs_file_t file;
    int ret;

    if (!device_is_ready(DEVICE_DT_GET(DT_NODELABEL(nvme0)))) {
        storage_error = -ENODEV;
        printk("FES_BASE storage unavailable: NVMe controller not ready\n");
        return;
    }
    ret = disk_access_init("nvme0n0");
    if (ret != 0) {
        storage_error = ret;
        return;
    }
    ret = disk_access_ioctl("nvme0n0", DISK_IOCTL_GET_SECTOR_COUNT, &sectors);
    if (ret == 0) {
        ret = disk_access_ioctl("nvme0n0", DISK_IOCTL_GET_SECTOR_SIZE, &sector_bytes);
    }
    if (ret != 0) {
        storage_error = ret;
        return;
    }
    ret = fs_mount(&mount);
    if (ret != 0) {
        storage_error = ret;
        return;
    }
    if (fat_fs.fs_type != FS_FAT32) {
        storage_error = -EPROTONOSUPPORT;
        (void)fs_unmount(&mount);
        return;
    }
    storage_ready = true;
    printk("FES_BASE STORAGE_READY filesystem=fat32 driver=nvme sectors=%u sector_bytes=%u\n",
           sectors, sector_bytes);
    fs_file_t_init(&file);
    ret = fs_open(&file, "/nvme0n0:/FESBOOT.CFG", FS_O_READ);
    if (ret != 0) {
        storage_error = ret;
        return;
    }
    ssize_t count = fs_read(&file, boot_config, sizeof(boot_config) - 1);
    unsigned char extra;
    ssize_t tail = count < 0 ? count : fs_read(&file, &extra, 1);
    int close_result = fs_close(&file);
    if (count <= 0 || tail != 0 || close_result != 0) {
        storage_error = count < 0 ? (int)count : -EINVAL;
        return;
    }
    boot_config[count] = '\0';
    static const char expected_config[] = "format=fes-base-v1\nlauncher=tenfoot\n";
    config_ready = count == (ssize_t)(sizeof(expected_config) - 1) &&
                   memcmp(boot_config, expected_config, sizeof(expected_config) - 1) == 0;
    if (!config_ready) storage_error = -EINVAL;
    printk("FES_BASE storage=fat32 driver=nvme sectors=%u sector_bytes=%u config=%s\n",
           sectors, sector_bytes, config_ready ? "verified" : "invalid");
}

static bool parse_u32(const char **cursor, uint32_t *value)
{
    const char *start = *cursor;
    char *end;
    unsigned long number;

    if (*start < '0' || *start > '9') return false;
    errno = 0;
    number = strtoul(start, &end, 10);
    if (errno || number > UINT32_MAX || end == start) return false;
    *value = (uint32_t)number;
    *cursor = end;
    return true;
}

static void status_response(void)
{
    (void)snprintf(response, sizeof(response),
        "{\"protocol\":1,\"target\":\"qemu-virt\",\"cpu\":\"aarch64\","
        "\"os\":\"zephyr\",\"uptime_ms\":%lld,"
        "\"storage\":{\"driver\":\"nvme\",\"filesystem\":\"fat32\","
        "\"ready\":%s,\"boot_config\":%s,\"error\":%d},"
        "\"network\":\"e1000-tcp\",\"sdcard\":\"unavailable\","
        "\"usb\":\"unavailable\",\"fpga\":\"unavailable\","
        "\"input\":{\"route\":\"diagnostic-only\",\"sequence\":%u,\"buttons\":%u}}\n",
        (long long)k_uptime_get(), storage_ready ? "true" : "false",
        config_ready ? "true" : "false", storage_error, last_sequence, input_buttons);
}

static void handle_request(void)
{
    if (strcmp(request, "STATUS") == 0) {
        status_response();
    } else if (strncmp(request, "INPUT ", 6) == 0) {
        const char *cursor = request + 6;
        uint32_t sequence, buttons;
        bool valid = parse_u32(&cursor, &sequence) && *cursor++ == ' ' &&
                     parse_u32(&cursor, &buttons) && *cursor == '\0' &&
                     buttons <= UINT16_MAX;
        if (!valid) {
            strcpy(response, "{\"error\":\"invalid-input\"}\n");
        } else if (sequence <= last_sequence) {
            strcpy(response, "{\"error\":\"stale-sequence\"}\n");
        } else {
            last_sequence = sequence;
            input_buttons = (uint16_t)buttons;
            strcpy(response, "{\"ok\":true,\"route\":\"diagnostic-only\"}\n");
        }
    } else if (strcmp(request, "FPGA") == 0 || strcmp(request, "USB") == 0 ||
               strcmp(request, "SDCARD") == 0) {
        strcpy(response, "{\"error\":\"unavailable\"}\n");
    } else {
        strcpy(response, "{\"error\":\"unknown-command\"}\n");
    }
}

static void serve_client(int client)
{
    /* A stalled peer may occupy this diagnostic for at most one second. */
    struct timeval timeout = {.tv_sec = 0, .tv_usec = 100000};
    if (zsock_setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) < 0 ||
        zsock_setsockopt(client, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout)) < 0) {
        (void)zsock_close(client);
        return;
    }
    size_t used = 0;
    bool complete = false;
    int64_t deadline = k_uptime_get() + 1000;
    while (used + 1 < sizeof(request) && k_uptime_get() < deadline) {
        ssize_t n = zsock_recv(client, request + used, 1, 0);
        if (n != 1) break;
        if (request[used] == '\n') {
            complete = true;
            break;
        }
        if (request[used] == '\0' || request[used] == '\r') break;
        ++used;
    }
    request[used] = '\0';
    if (complete) {
        handle_request();
    } else {
        strcpy(response, "{\"error\":\"invalid-request\"}\n");
    }
    size_t length = strlen(response), sent = 0;
    while (sent < length && k_uptime_get() < deadline) {
        ssize_t n = zsock_send(client, response + sent, length - sent, 0);
        if (n <= 0) break;
        sent += (size_t)n;
    }
    (void)zsock_close(client);
}

int main(void)
{
    printk("FES_BASE boot target=qemu-virt os=zephyr scope=base-services\n");
    load_boot_config();
    struct net_if *interface = net_if_get_default();
    struct in_addr *local = interface ? net_if_ipv4_get_global_addr(interface, NET_ADDR_PREFERRED) : NULL;
    char local_text[NET_IPV4_ADDR_LEN] = "unavailable";
    if (local) (void)net_addr_ntop(AF_INET, local, local_text, sizeof(local_text));
    printk("FES_BASE network interface_up=%d ipv4=%s\n", interface && net_if_is_up(interface), local_text);
    int listener = zsock_socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (listener < 0) {
        printk("FES_BASE FAIL socket errno=%d\n", errno);
        return 1;
    }
    struct sockaddr_in address = {
        .sin_family = AF_INET,
        .sin_port = htons(SERVICE_PORT),
        .sin_addr.s_addr = htonl(INADDR_ANY),
    };
    if (zsock_bind(listener, (struct sockaddr *)&address, sizeof(address)) < 0 ||
        zsock_listen(listener, 1) < 0) {
        printk("FES_BASE FAIL listen errno=%d\n", errno);
        (void)zsock_close(listener);
        return 1;
    }
    printk("FES_BASE READY tcp=%d storage=%s boot_config=%s\n", SERVICE_PORT,
           storage_ready ? "ready" : "unavailable", config_ready ? "verified" : "unavailable");
    for (;;) {
        int client = zsock_accept(listener, NULL, NULL);
        if (client >= 0) serve_client(client);
        else k_sleep(K_MSEC(10));
    }
}
