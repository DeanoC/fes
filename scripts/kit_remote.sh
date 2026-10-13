#!/bin/sh
# Apply or inspect kit manifest changes. The host runs this through ssh
# (sh -s) or directly in tests with KIT_FAKE=1 and KIT_ROOT set.
# It does not reboot the board.
set -eu

if [ "${KIT_FAKE:-}" = 1 ]; then
  : "${KIT_ROOT:?KIT_ROOT is required when KIT_FAKE=1}"
  proc=$KIT_ROOT/proc
else
  KIT_ROOT=
  proc=/proc
fi

lp() {
  printf '%s%s' "$KIT_ROOT" "$1"
}

die() {
  printf 'REFUSE %s\n' "$*" >&2
  exit 2
}

note() {
  printf 'fes-kit: %s\n' "$*" >&2
}

valid_name() {
  case "$1" in
    ''|*[!A-Za-z0-9._+-]*) return 1 ;;
  esac
}

valid_hash() {
  case "$1" in
    *[!0-9a-f]*) return 1 ;;
  esac
  [ "${#1}" -eq 64 ]
}

valid_mode() {
  case "$1" in
    0755|0644) ;;
    *) return 1 ;;
  esac
}

valid_path() {
  case "$1" in
    /*) ;;
    *) return 1 ;;
  esac
  case "$1" in
    *..*|*' '*|*'$'*|*'`'*|*'//'*) return 1 ;;
  esac
  # Reject "." segments. ".." is already rejected above. A lexical "." can
  # combine with a later ".." and leave the tracked directory.
  (
    IFS=/
    first=1
    for seg in $1; do
      if [ "$first" = 1 ]; then
        first=0
        [ -z "$seg" ] && continue
      fi
      case "$seg" in
        ''|.|..) exit 1 ;;
      esac
    done
  )
}

allowed_src() {
  case "$1" in
    /tmp/fes-incoming/*|/tmp/fes-swap/*|/tmp/fes-work/*) ;;
    /usr/sbin/mister-runtime|/usr/sbin/mister-agent|/usr/sbin/fogcast-kit|/usr/sbin/fogcast-tenfoot) ;;
    /media/fat/fogcast/launcher.json|/media/fat/fogcast/agent.toml) ;;
    /media/fat/fes/stock/*|/media/fat/fes/baselines/*) ;;
    /usr/share/mister-runtime/core-packages/*) ;;
    *) return 1 ;;
  esac
  valid_path "$1"
}

allowed_dest() {
  case "$1" in
    /usr/sbin/mister-runtime|/usr/sbin/mister-agent|/usr/sbin/fogcast-kit|/usr/sbin/fogcast-tenfoot) ;;
    /media/fat/fogcast/launcher.json|/media/fat/fogcast/agent.toml) ;;
    /media/fat/fes/kit-manifest.json|/media/fat/fes/baseline.json) ;;
    /media/fat/fes/stock/*|/media/fat/fes/baselines/*) ;;
    /usr/share/mister-runtime/core-packages/*) ;;
    *) return 1 ;;
  esac
  valid_path "$1"
}

hash_file() {
  sha256sum "$1" | awk '{print $1; exit}'
}

mounted() {
  awk -v p="$1" '$2==p {found=1} END {exit found ? 0 : 1}' "$proc/mounts"
}

mount_add() {
  dest=$1
  if mounted "$dest"; then
    return 0
  fi
  if [ "${KIT_FAKE:-}" = 1 ]; then
    printf 'tmpfs %s tmpfs rw 0 0\n' "$dest" >> "$proc/mounts"
    return 0
  fi
  mount --bind "$2" "$dest"
}

mount_del() {
  dest=$1
  if mounted "$dest"; then
    if [ "${KIT_FAKE:-}" = 1 ]; then
      awk -v p="$dest" '$2!=p' "$proc/mounts" > "$proc/mounts.tmp"
      mv "$proc/mounts.tmp" "$proc/mounts"
    else
      umount "$dest"
    fi
  fi
}

apply_bind() {
  dest=$1
  stage=$2
  if [ "${KIT_FAKE:-}" = 1 ]; then
    cp -a "$(lp "$stage")" "$(lp "$dest")"
    mount_add "$dest" "$(lp "$stage")"
  else
    if mounted "$dest"; then
      umount "$(lp "$dest")"
    fi
    mount --bind "$(lp "$stage")" "$(lp "$dest")"
  fi
}

apply_umount() {
  dest=$1
  backup=$2
  if [ "${KIT_FAKE:-}" = 1 ]; then
    cp -a "$(lp "$backup")" "$(lp "$dest")"
    mount_del "$dest"
  else
    mount_del "$dest"
  fi
}

rw_on=0
remount_rw() {
  if [ "${KIT_FAKE:-}" = 1 ]; then
    rw_on=1
    return 0
  fi
  mount -o remount,rw /
  rw_on=1
}

remount_ro() {
  [ "$rw_on" = 1 ] || return 0
  if [ "${KIT_FAKE:-}" != 1 ]; then
    mount -o remount,ro / || return 1
  fi
  rw_on=0
}

service_one() {
  action=$1
  name=$2
  case "$name" in
    S[0-9][0-9]*) ;;
    *) die "bad service $name" ;;
  esac
  case "$name" in
    *[!A-Za-z0-9._-]*) die "bad service $name" ;;
  esac
  printf '%s %s\n' "$action" "$name" >> "$(lp /tmp)/fes-services"
  if [ "${KIT_FAKE:-}" = 1 ]; then
    return 0
  fi
  "/etc/init.d/$name" "$action"
}

walk_services() {
  action=$1
  list=$2
  [ -n "$list" ] || return 0
  while [ -n "$list" ]; do
    case "$list" in
      *,*) one=${list%%,*}; list=${list#*,} ;;
      *) one=$list; list= ;;
    esac
    service_one "$action" "$one"
  done
}

supervise_name() {
  case "$1" in
    S40mister-runtime) printf '%s\n' mister-runtime ;;
    S50mister-agent) printf '%s\n' mister-agent ;;
    S60fogcast-kit) printf '%s\n' fogcast-kit ;;
    *) printf '\n' ;;
  esac
}

supervisor_alive() {
  name=$1
  if [ "${KIT_FAKE:-}" = 1 ]; then
    [ -e "$(lp /tmp)/fes-supervisor-$name" ]
    return
  fi
  for dir in "$proc"/[0-9]*; do
    [ -e "$dir/cmdline" ] || continue
    cmd=$(tr '\0' ' ' < "$dir/cmdline" 2>/dev/null || true)
    case "$cmd" in
      *"mister-supervise ${name} "*) return 0 ;;
    esac
  done
  return 1
}

supervisors_left() {
  list=$1
  left=0
  while [ -n "$list" ]; do
    case "$list" in
      *,*) one=${list%%,*}; list=${list#*,} ;;
      *) one=$list; list= ;;
    esac
    name=$(supervise_name "$one")
    [ -n "$name" ] || continue
    if supervisor_alive "$name"; then
      left=$((left + 1))
    fi
  done
  printf '%s\n' "$left"
}

start_services() {
  list=$1
  [ -n "$list" ] || return 0
  if [ "${KIT_FAIL_START:-}" = 1 ] && [ ! -f "$(lp /tmp)/fes-start-failed" ]; then
    printf 'failed\n' > "$(lp /tmp)/fes-start-failed"
    return 1
  fi
  # A partial restart leaves the other supervisors running. Only the services
  # in this start list must be gone; mister-runtime staying up is normal when
  # the swap restarts the agent and the launcher.
  left=$(supervisors_left "$list")
  if [ "$left" != 0 ]; then
    note "supervisors still running ($left)"
    return 1
  fi
  walk_services start "$list" || return 1
  if [ "${KIT_FAKE:-}" != 1 ]; then
    sleep 2
  fi
}

stop_services() {
  list=$1
  [ -n "$list" ] || return 0
  walk_services stop "$list" || return 1
  if [ "${KIT_FAKE:-}" != 1 ]; then
    sleep 1
  fi
}

kill_strays() {
  for dir in "$proc"/[0-9]*; do
    [ -e "$dir/cmdline" ] || continue
    pid=${dir##*/}
    case "$pid" in
      *[!0-9]*|0|1) continue ;;
    esac
    exe=$(readlink "$dir/exe" 2>/dev/null || true)
    cmd=$(tr '\0' ' ' < "$dir/cmdline" 2>/dev/null || true)
    case "$cmd" in
      *mister-agent*|*mister-runtime*|*fogcast-kit*|*fogcast-tenfoot*) ;;
      *) continue ;;
    esac
    case "$exe" in
      /tmp/fes-swap/*|*\(deleted\)*) ;;
      *) continue ;;
    esac
    if [ "${KIT_FAKE:-}" = 1 ]; then
      printf '%s\n' "$pid" >> "$(lp /tmp)/fes-kills"
      rm -rf "$dir"
    else
      kill "$pid" 2>/dev/null || true
    fi
  done
}

cat_block() {
  name=$1
  file=$2
  printf -- '--%s--\n' "$name"
  if [ -f "$file" ]; then
    cat "$file"
    printf '\n'
  fi
  printf -- '--/%s--\n' "$name"
}

extract_good() {
  file=$1
  [ -f "$file" ] || return 0
  tr -d '\n' < "$file" | sed -n 's/.*"good" *: *"\([0-9a-f][0-9a-f]*\)".*/\1/p' | head -n 1
}

probe() {
  mkdir -p "$(lp /tmp)" "$(lp /media/fat/fes)"
  boot=$(tr -d '[:space:]' < "$proc/sys/kernel/random/boot_id")
  printf -- '--BOOT--\n%s\n--/BOOT--\n' "$boot"
  cat_block BOOTJSON "$(lp /media/fat/fogcast/releases/boot.json)"
  cat_block STATEJSON "$(lp /media/fat/fogcast/releases/state.json)"
  good=$(extract_good "$(lp /media/fat/fogcast/releases/state.json)")
  release=
  if valid_hash "$good"; then
    release=$(lp "/media/fat/fogcast/releases/manifests/$good.json")
  fi
  if [ -n "$release" ] && [ -f "$release" ]; then
    cat_block RELEASEJSON "$release"
  else
    printf -- '--RELEASEJSON--\n--/RELEASEJSON--\n'
  fi
  printf -- '--MOUNTS--\n'
  if [ -f "$proc/mounts" ]; then
    cat "$proc/mounts"
  fi
  printf -- '--/MOUNTS--\n'
  printf -- '--HASHES--\n'
  for spec in "$@"; do
    name=${spec%%=*}
    path=${spec#*=}
    valid_name "$name" || die "bad component $name"
    valid_path "$path" || die "bad path $path"
    if [ -f "$(lp "$path")" ]; then
      printf '%s %s\n' "$name" "$(hash_file "$(lp "$path")")"
    else
      printf '%s MISSING\n' "$name"
    fi
  done
  printf -- '--/HASHES--\n'
  cat_block MANIFEST "$(lp /media/fat/fes/kit-manifest.json)"
  cat_block BASELINE "$(lp /media/fat/fes/baseline.json)"
  printf -- '--PROCS--\n'
  for dir in "$proc"/[0-9]*; do
    [ -e "$dir/cmdline" ] || continue
    pid=${dir##*/}
    exe=$(readlink "$dir/exe" 2>/dev/null || true)
    cmd=$(tr '\0' ' ' < "$dir/cmdline" 2>/dev/null || true)
    case "$cmd" in
      *mister-agent*|*mister-runtime*|*fogcast-kit*|*fogcast-tenfoot*)
        printf '%s\t%s\t%s\n' "$pid" "$exe" "$cmd"
        ;;
    esac
  done
  printf -- '--/PROCS--\n'
}

undo_n=0
push_undo() {
  undo_n=$((undo_n + 1))
  printf '%s\n' "$1" > "$(lp /tmp/fes-work)/u$undo_n"
}

run_undo() {
  i=$undo_n
  while [ "$i" -gt 0 ]; do
    line=$(cat "$(lp /tmp/fes-work)/u$i")
    set -- $line
    case "$1" in
      umount)
        apply_umount "$2" "$3"
        ;;
      bind)
        apply_bind "$2" "$3"
        ;;
      copy)
        cp -a "$(lp "$2")" "$(lp "$3")"
        ;;
      *)
        note "unknown undo $1"
        ;;
    esac
    i=$((i - 1))
  done
}

verify_rollback() {
  file=$(lp /tmp/fes-work)/expect-mounts
  [ -f "$file" ] || return 0
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    mounted "$1" || {
      note "rollback lost mount on $1"
      return 1
    }
    got=$(hash_file "$(lp "$1")")
    [ "$got" = "$2" ] || {
      note "rollback hash mismatch for $1"
      return 1
    }
  done < "$file"
}

copy_install() {
  src=$1
  dest=$2
  allowed_src "$src" || die "source not allowed: $src"
  allowed_dest "$dest" || die "dest not allowed: $dest"
  [ -f "$(lp "$src")" ] || die "missing $src"
  mkdir -p "$(dirname "$(lp "$dest")")"
  cp -a "$(lp "$src")" "$(lp "$dest").tmp"
  mv "$(lp "$dest").tmp" "$(lp "$dest")"
}

ensure_backup() {
  dest=$1
  backup=$2
  stock=$3
  src=$4
  if [ -f "$(lp "$backup")" ]; then
    got=$(hash_file "$(lp "$backup")")
    [ "$got" = "$stock" ] || die "stock backup mismatch for $dest"
    return 0
  fi
  # A drifted destination is not stock. The incoming bytes may be, when a
  # named baseline is promoted because no backup exists yet.
  if [ -n "$src" ] && [ -f "$(lp "$src")" ]; then
    got=$(hash_file "$(lp "$src")")
    if [ "$got" = "$stock" ]; then
      mkdir -p "$(dirname "$(lp "$backup")")"
      cp -a "$(lp "$src")" "$(lp "$backup")"
      return 0
    fi
  fi
  if mounted "$dest"; then
    die "mounted without stock backup: $dest"
  fi
  [ -f "$(lp "$dest")" ] || die "missing $dest"
  got=$(hash_file "$(lp "$dest")")
  [ "$got" = "$stock" ] || die "stock hash mismatch for $dest"
  mkdir -p "$(dirname "$(lp "$backup")")"
  cp -a "$(lp "$dest")" "$(lp "$backup")"
}

apply_plan() {
  plan=$1
  valid_path "$plan" || die "bad plan path"
  [ -f "$(lp "$plan")" ] || die "missing plan $plan"
  mkdir -p "$(lp /tmp)/fes-work" "$(lp /tmp)/fes-swap" "$(lp /tmp)/fes-incoming" \
    "$(lp /media/fat/fes)/stock" "$(lp /proc)"
  : > "$(lp /tmp)/fes-services"
  boot_expect=
  stops=
  starts=
  manifest=
  places=$(lp /tmp/fes-work)/places
  clears=$(lp /tmp/fes-work)/clears
  unmounts=$(lp /tmp/fes-work)/unmounts
  removes=$(lp /tmp/fes-work)/removes
  installs=$(lp /tmp/fes-work)/installs
  : > "$places"
  : > "$clears"
  : > "$unmounts"
  : > "$removes"
  : > "$installs"
  : > "$(lp /tmp/fes-work)/expect-mounts"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ''|'#'*) continue ;;
    esac
    set -- $line
    case "$1" in
      BOOT)
        [ "$#" -eq 2 ] || die "BOOT fields"
        boot_expect=$2
        ;;
      STOP)
        [ "$#" -eq 2 ] || die "STOP fields"
        stops=$2
        ;;
      START)
        [ "$#" -eq 2 ] || die "START fields"
        starts=$2
        ;;
      PLACE)
        [ "$#" -eq 10 ] || die "PLACE fields"
        printf '%s\n' "$line" >> "$places"
        ;;
      CLEAR)
        [ "$#" -eq 7 ] || die "CLEAR fields"
        printf '%s\n' "$line" >> "$clears"
        ;;
      UNMOUNT)
        [ "$#" -eq 3 ] || die "UNMOUNT fields"
        printf '%s\n' "$line" >> "$unmounts"
        ;;
      REMOVE)
        [ "$#" -eq 3 ] || die "REMOVE fields"
        printf '%s\n' "$line" >> "$removes"
        ;;
      INSTALL)
        [ "$#" -eq 3 ] || die "INSTALL fields"
        printf '%s\n' "$line" >> "$installs"
        ;;
      MANIFEST)
        [ "$#" -eq 2 ] || die "MANIFEST fields"
        manifest=$2
        ;;
      COMMIT) ;;
      *) die "bad plan line $1" ;;
    esac
  done < "$(lp "$plan")"
  [ -n "$boot_expect" ] || die "plan has no BOOT"
  current=$(tr -d '[:space:]' < "$proc/sys/kernel/random/boot_id")
  boot_changed=0
  if [ "$current" != "$boot_expect" ]; then
    boot_changed=1
    note "boot id changed"
  fi

  # PLACE component path source persistence mode backup stage stock desired
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    valid_name "$2" || die "bad component $2"
    valid_path "$3" || die "bad path"
    allowed_src "$4" || die "source not allowed: $4"
    case "$5" in
      tmpfs|persistent) ;;
      *) die "bad persistence" ;;
    esac
    valid_mode "$6" || die "bad mode"
    valid_path "$7" || die "bad backup"
    valid_path "$8" || die "bad stage"
    valid_hash "$9" || die "bad stock hash"
    valid_hash "${10}" || die "bad desired hash"
    allowed_dest "$3" || die "dest not allowed"
    [ -f "$(lp "$4")" ] || die "missing source $4"
    got=$(hash_file "$(lp "$4")")
    [ "$got" = "${10}" ] || die "source hash mismatch for $2"
    ensure_backup "$3" "$7" "$9" "$4"
  done < "$places"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    valid_name "$2" || die "bad component"
    valid_path "$3" || die "bad path"
    case "$4" in
      tmpfs|persistent) ;;
      *) die "bad persistence" ;;
    esac
    valid_path "$5" || die "bad backup"
    valid_path "$6" || die "bad stage"
    valid_hash "$7" || die "bad stock hash"
    [ -f "$(lp "$5")" ] || die "missing backup $5"
    got=$(hash_file "$(lp "$5")")
    [ "$got" = "$7" ] || die "backup hash mismatch for $2"
  done < "$clears"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    valid_name "$2" || die "bad component"
    valid_path "$3" || die "bad path"
    allowed_dest "$3" || die "dest not allowed"
  done < "$unmounts"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    valid_name "$2" || die "bad component"
    valid_path "$3" || die "bad path"
    allowed_dest "$3" || die "dest not allowed"
  done < "$removes"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    allowed_src "$2" || die "source not allowed"
    allowed_dest "$3" || die "dest not allowed"
  done < "$installs"

  if [ -n "$manifest" ]; then
    valid_path "$manifest" || die "bad manifest path"
    [ -f "$(lp "$manifest")" ] || die "missing manifest"
  fi

  if [ -n "$stops" ]; then
    if ! stop_services "$stops"; then
      note "stop failed; starting services again"
      start_services "$starts" || note "restart after stop failure failed"
      die "stop failed"
    fi
    kill_strays
  fi

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    if mounted "$3"; then
      if [ "${KIT_FAKE:-}" = 1 ]; then
        mount_del "$3"
      else
        umount "$(lp "$3")" || {
          note "umount failed; starting services again"
          start_services "$starts" || note "restart after umount failure failed"
          die "umount failed $3"
        }
      fi
    fi
  done < "$unmounts"

  need_rw=0
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    case "$5" in
      persistent)
        case "$3" in
          /usr/*) need_rw=1 ;;
        esac
        ;;
    esac
  done < "$places"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    case "$4" in
      persistent)
        case "$3" in
          /usr/*) need_rw=1 ;;
        esac
        ;;
    esac
  done < "$clears"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    case "$3" in
      /usr/*) need_rw=1 ;;
    esac
  done < "$removes"
  if [ "$need_rw" = 1 ]; then
    remount_rw || die "remount rw failed"
  fi

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    dest=$3
    src=$4
    persistence=$5
    mode=$6
    backup=$7
    stage=$8
    mkdir -p "$(dirname "$(lp "$stage")")" "$(lp /tmp/fes-work)"
    stage_saved=0
    if [ -f "$(lp "$stage")" ]; then
      cp -a "$(lp "$stage")" "$(lp /tmp/fes-work)/stage-$2"
      stage_saved=1
    fi
    cp -a "$(lp "$src")" "$(lp "$stage")"
    chmod "$mode" "$(lp "$stage")"
    if [ "$persistence" = tmpfs ]; then
      if [ "$stage_saved" = 1 ]; then
        push_undo "copy /tmp/fes-work/stage-$2 $stage"
      fi
      if mounted "$dest"; then
        push_undo "bind $dest $stage"
        apply_umount "$dest" "$backup"
      fi
      push_undo "umount $dest $backup"
      apply_bind "$dest" "$stage"
    elif mounted "$dest"; then
      # Switching a bind to a persistent write. Undo restores the underlying
      # stock, then the previous stage, then the bind. A byte copy of the
      # mounted file would leave the new bytes on FAT with no mount.
      if [ "$stage_saved" != 1 ]; then
        cp -a "$(lp "$dest")" "$(lp /tmp/fes-work)/stage-$2"
        stage_saved=1
      fi
      prev=$(hash_file "$(lp /tmp/fes-work)/stage-$2")
      printf '%s %s\n' "$dest" "$prev" >> "$(lp /tmp/fes-work)/expect-mounts"
      push_undo "bind $dest $stage"
      push_undo "copy /tmp/fes-work/stage-$2 $stage"
      push_undo "copy $backup $dest"
      apply_umount "$dest" "$backup"
      cp -a "$(lp "$stage")" "$(lp "$dest")"
      chmod "$mode" "$(lp "$dest")"
    else
      if [ "$stage_saved" = 1 ]; then
        push_undo "copy /tmp/fes-work/stage-$2 $stage"
      fi
      cp -a "$(lp "$dest")" "$(lp /tmp/fes-work)/bytes-$2"
      push_undo "copy /tmp/fes-work/bytes-$2 $dest"
      cp -a "$(lp "$stage")" "$(lp "$dest")"
      chmod "$mode" "$(lp "$dest")"
    fi
  done < "$places"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    dest=$3
    persistence=$4
    backup=$5
    stage=$6
    if [ "$persistence" = tmpfs ]; then
      if [ -f "$(lp "$stage")" ]; then
        push_undo "bind $dest $stage"
      fi
      apply_umount "$dest" "$backup"
      rm -f "$(lp "$stage")"
    else
      cp -a "$(lp "$dest")" "$(lp /tmp/fes-work)/bytes-$2"
      push_undo "copy /tmp/fes-work/bytes-$2 $dest"
      if mounted "$dest"; then
        apply_umount "$dest" "$backup"
      fi
      cp -a "$(lp "$backup")" "$(lp "$dest")"
    fi
  done < "$clears"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    dest=$3
    name=$2
    if [ -f "$(lp "$dest")" ]; then
      cp -a "$(lp "$dest")" "$(lp /tmp/fes-work)/removed-$name"
      push_undo "copy /tmp/fes-work/removed-$name $dest"
    fi
    if mounted "$dest"; then
      if [ -f "$(lp /tmp/fes-swap/$name)" ]; then
        cp -a "$(lp /tmp/fes-swap/$name)" "$(lp /tmp/fes-work)/stage-$name"
        push_undo "bind $dest /tmp/fes-swap/$name"
        push_undo "copy /tmp/fes-work/stage-$name /tmp/fes-swap/$name"
      fi
      if [ "${KIT_FAKE:-}" = 1 ]; then
        mount_del "$dest"
      else
        umount "$(lp "$dest")" || die "umount failed $dest"
      fi
    fi
    rm -f "$(lp "$dest")"
  done < "$removes"

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    copy_install "$2" "$3"
  done < "$installs"

  if [ -n "$manifest" ]; then
    if [ -f "$(lp /media/fat/fes/kit-manifest.json)" ]; then
      cp -a "$(lp /media/fat/fes/kit-manifest.json)" "$(lp /tmp/fes-work)/manifest.prev"
      push_undo "copy /tmp/fes-work/manifest.prev /media/fat/fes/kit-manifest.json"
    fi
    copy_install "$manifest" /media/fat/fes/kit-manifest.json
  fi

  if ! start_services "$starts"; then
    note "start failed; rolling back"
    run_undo || true
    verify_rollback || note "rollback did not restore the previous bind"
    remount_ro || true
    start_services "$starts" || note "rollback start failed"
    die "start failed"
  fi

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    got=$(hash_file "$(lp "$3")")
    [ "$got" = "${10}" ] || {
      run_undo || true
      verify_rollback || note "rollback did not restore the previous bind"
      remount_ro || true
      start_services "$starts" || true
      die "placed hash mismatch for $2"
    }
  done < "$places"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    set -- $line
    got=$(hash_file "$(lp "$3")")
    [ "$got" = "$7" ] || {
      run_undo || true
      verify_rollback || note "rollback did not restore the previous bind"
      remount_ro || true
      start_services "$starts" || true
      die "restored hash mismatch for $2"
    }
    if [ "$4" = tmpfs ] && mounted "$3"; then
      run_undo || true
      verify_rollback || note "rollback did not restore the previous bind"
      remount_ro || true
      start_services "$starts" || true
      die "tmpfs mount remains on $3"
    fi
  done < "$clears"

  remount_ro || die "remount ro failed"
  sync >/dev/null 2>&1 || true
  if [ "$boot_changed" = 1 ]; then
    printf 'BOOT_CHANGED\n'
  else
    printf 'OK\n'
  fi
}

cleanup() {
  remount_ro || true
}
trap cleanup EXIT

mkdir -p "$(lp /tmp)"
op=${1:-}
shift || true
case "$op" in
  probe) probe "$@" ;;
  apply) apply_plan "${1:-}" ;;
  show)
    path=${1:-}
    valid_path "$path" || die "bad path"
    allowed_src "$path" || allowed_dest "$path" || die "path not allowed"
    [ -f "$(lp "$path")" ] || die "missing $path"
    cat "$(lp "$path")"
    ;;
  *) die "usage: kit_remote.sh probe|apply|show" ;;
esac
