set -eu
ulimit -c 0
umask 077
test "$(cat /proc/sys/kernel/random/boot_id)" = 15e107d8-0ed1-4e5e-83d8-b27c8a49d853
mkdir /tmp/fes-cpu-proof-9bcf1a7a7
trap 'rm -rf /tmp/fes-cpu-proof-9bcf1a7a7' EXIT
cd /tmp/fes-cpu-proof-9bcf1a7a7
tar -xf -
test "$(sha256sum static-test | cut -d " " -f 1)" = 2ec16ee8f74281b9f92eb8159c44e3188496a76747a746b132ace0a97086a3d9
test "$(sha256sum dynamic-test | cut -d " " -f 1)" = 3dfc40ed34e24c0902047652261e7e683b7f3d4ac5168f171f15a988eebccb99
set +e
timeout 30 ./static-test
static_rc=$?
echo STATIC_TEST_RETURN=$static_rc
timeout 30 ./dynamic-test
dynamic_rc=$?
echo DYNAMIC_TEST_RETURN=$dynamic_rc
set -e
cat /proc/sys/kernel/random/boot_id
sha256sum /sys/kernel/notes /usr/share/fes/kernel-modules.json
test "$dynamic_rc" -eq 0
echo DE25_CPU_ONLY_PAIRED_TRIAL_COMPLETE
