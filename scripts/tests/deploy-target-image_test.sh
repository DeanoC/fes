#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-deploy-target.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

deploy=$repo/scripts/deploy-target-image.sh
test -x "$deploy"
grep -Fq 'linux.img.new' "$deploy"
grep -Fq '&& mv $remote_image /media/fat/linux/linux.img' "$deploy"
grep -Fq 'scp -O' "$deploy"
grep -Fq 'reboot' "$deploy"

fake_bin=$fixture/bin
mkdir -p "$fake_bin"
ssh_log=$fixture/ssh.log
cat > "$fake_bin/sshpass" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FOGCAST_SSH_LOG"
EOF
chmod 0755 "$fake_bin/sshpass"

image=$fixture/linux.img
printf '%s\n' image > "$image"
FOGCAST_SSH_LOG=$ssh_log PATH="$fake_bin:$PATH" \
  sh "$deploy" "$image"

grep -Fq -- 'scp' "$ssh_log"
grep -Fq -- '/media/fat/linux/linux.img.new' "$ssh_log"
grep -Fq -- 'test -s /media/fat/linux/linux.img.new' "$ssh_log"
grep -Fq -- 'mv /media/fat/linux/linux.img.new /media/fat/linux/linux.img' "$ssh_log"
grep -Fq -- 'reboot' "$ssh_log"

: > "$fixture/empty.img"
if FOGCAST_SSH_LOG=$ssh_log PATH="$fake_bin:$PATH" \
  sh "$deploy" "$fixture/empty.img" >/dev/null 2>&1; then
  echo 'deployment accepted an empty image' >&2
  exit 1
fi

echo 'target image deployment tests passed'
