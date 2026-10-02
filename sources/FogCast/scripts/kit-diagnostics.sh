#!/bin/sh
# Read-only diagnostics for a BusyBox DE10-Nano kit.
# Prints schema fogcast.kit-diagnostics.v1 as key=value on stdout.
# Reads /proc, a few identity files, and `df -P -k`. Does not create files,
# restart services, or write /media/fat or u-boot.txt.
#
# The output contract is sources/FogCast/docs/kit-resource-limits.md.
#
# FES_KIT_DIAG_ROOT, when set, reads that directory instead of / and expects
# df text at df.txt under it. proc/stat.sample2 is the second CPU sample and
# per-process proc/<pid>/stat.sample2 is the second process sample. The script
# does not sleep in that mode. Tests set the variable. A live kit leaves it
# unset. FES_KIT_DIAG_SAMPLE_SECONDS (default 1, range 1..5) is the live sample.

set -eu

if [ "$#" -ne 0 ]; then
  printf '%s\n' 'usage: kit-diagnostics.sh' >&2
  exit 2
fi

nl='
'
alpha='-./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz'

die() {
  printf '%s\n' "kit-diagnostics: $1" >&2
  exit 1
}

u_strip() {
  v=$1
  while [ "${#v}" -gt 1 ] && [ "${v#0}" != "$v" ]; do
    v=${v#0}
  done
  printf '%s\n' "$v"
}

u_norm() {
  case $1 in
    ''|*[!0-9]*) return 1 ;;
  esac
  u_strip "$1"
}

u_cmp() {
  a=$(u_norm "$1") || return 1
  b=$(u_norm "$2") || return 1
  if [ "${#a}" -lt "${#b}" ]; then
    printf '%s\n' -1
    return 0
  fi
  if [ "${#a}" -gt "${#b}" ]; then
    printf '%s\n' 1
    return 0
  fi
  while [ -n "$a" ]; do
    da=${a%"${a#?}"}
    db=${b%"${b#?}"}
    if [ "$da" != "$db" ]; then
      if [ "$da" -lt "$db" ]; then
        printf '%s\n' -1
      else
        printf '%s\n' 1
      fi
      return 0
    fi
    a=${a#?}
    b=${b#?}
  done
  printf '%s\n' 0
}

u_add() {
  a=$(u_norm "$1") || return 1
  b=$(u_norm "$2") || return 1
  while [ "${#a}" -lt "${#b}" ]; do
    a=0$a
  done
  while [ "${#b}" -lt "${#a}" ]; do
    b=0$b
  done
  carry=0
  result=
  while [ -n "$a" ]; do
    da=${a#"${a%?}"}
    db=${b#"${b%?}"}
    a=${a%?}
    b=${b%?}
    sum=$((da + db + carry))
    carry=$((sum / 10))
    result=$((sum % 10))$result
  done
  if [ "$carry" -ne 0 ]; then
    result=$carry$result
  fi
  u_strip "$result"
}

u_sub() {
  a=$(u_norm "$1") || return 1
  b=$(u_norm "$2") || return 1
  cmp=$(u_cmp "$a" "$b") || return 1
  if [ "$cmp" -lt 0 ]; then
    return 1
  fi
  while [ "${#b}" -lt "${#a}" ]; do
    b=0$b
  done
  borrow=0
  result=
  while [ -n "$a" ]; do
    da=${a#"${a%?}"}
    db=${b#"${b%?}"}
    a=${a%?}
    b=${b%?}
    diff=$((da - db - borrow))
    if [ "$diff" -lt 0 ]; then
      diff=$((diff + 10))
      borrow=1
    else
      borrow=0
    fi
    result=$diff$result
  done
  if [ "$borrow" -ne 0 ]; then
    return 1
  fi
  u_strip "$result"
}

small_ok() {
  n=$(u_norm "$1") || return 1
  [ "${#n}" -le 9 ]
}

percent() {
  part=$(u_norm "$1") || return 1
  whole=$(u_norm "$2") || return 1
  small_ok "$part" || return 1
  small_ok "$whole" || return 1
  [ "$whole" -ne 0 ] || return 1
  printf '%s\n' $(( (part * 100 + whole / 2) / whole ))
}

trim() {
  t=$1
  t=${t#"${t%%[![:space:]]*}"}
  t=${t%"${t##*[![:space:]]}"}
  printf '%s\n' "$t"
}

valid_token() {
  case $1 in
    ''|*[!0-9A-Za-z._:-]*) return 1 ;;
  esac
  [ "${#1}" -le 128 ]
}

decimal_ok() {
  case $1 in
    ''|*[!0-9.]*|.*|*.*.*|*. ) return 1 ;;
  esac
  case $1 in
    *[0-9]*) return 0 ;;
    *) return 1 ;;
  esac
}

kit_path() {
  rel=$1
  while [ "${rel#/}" != "$rel" ]; do
    rel=${rel#/}
  done
  if [ -n "$root" ]; then
    printf '%s/%s\n' "$root" "$rel"
  else
    printf '/%s\n' "$rel"
  fi
}

emit() {
  printf '%s=%s\n' "$1" "$2"
}

take_id() {
  logical=$1
  file=$2
  [ -n "$image_id" ] && return 0
  [ -f "$file" ] || return 0
  IFS= read -r token < "$file" || return 0
  token=$(trim "$token") || return 0
  valid_token "$token" || return 0
  image_id=$token
  image_id_source=$logical
}

read_os_release() {
  file=$1
  [ -f "$file" ] || return 0
  id_fes=
  id_image=
  id_build=
  while IFS= read -r line || [ -n "$line" ]; do
    case $line in
      ''|'#'*) continue ;;
      FES_IMAGE_ID=*|IMAGE_ID=*|BUILD_ID=*) ;;
      *) continue ;;
    esac
    key=${line%%=*}
    val=${line#*=}
    val=$(trim "$val")
    case $val in
      \"*\") val=${val#\"}; val=${val%\"} ;;
      \'*\') val=${val#\'}; val=${val%\'} ;;
    esac
    case $key in
      FES_IMAGE_ID) id_fes=$val ;;
      IMAGE_ID) id_image=$val ;;
      BUILD_ID) id_build=$val ;;
    esac
  done < "$file"
  for candidate in "$id_fes" "$id_image" "$id_build"; do
    if valid_token "$candidate"; then
      image_id=$candidate
      image_id_source=/etc/os-release
      return 0
    fi
  done
  return 0
}

read_factory() {
  logical=$1
  file=$2
  [ -n "$image_id" ] && return 0
  [ -f "$file" ] || return 0
  sed_path=$(command -v sed || true)
  [ -n "$sed_path" ] || return 0
  flat=
  while IFS= read -r line || [ -n "$line" ]; do
    flat=$flat$line' '
  done < "$file"
  found=$(printf '%s\n' "$flat" | sed -n 's/.*"fes_revision"[[:space:]]*:[[:space:]]*"\([0-9a-fA-F]\{7,40\}\)".*/\1/p') || return 0
  found=${found%%"$nl"*}
  if valid_token "$found"; then
    image_id=$found
    image_id_source=$logical
  fi
}

read_proc_version() {
  [ -n "$image_id" ] && return 0
  file=$(kit_path /proc/version)
  [ -f "$file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    set -f
    # shellcheck disable=SC2086
    set -- $line
    set +f
    for word do
      case $word in
        *[!0-9a-fA-F]*) continue ;;
      esac
      n=${#word}
      if [ "$n" -ge 7 ] && [ "$n" -le 40 ]; then
        image_id=$word
        image_id_source=/proc/version
        return 0
      fi
    done
  done < "$file"
}

mem_value() {
  want=$1
  file=$2
  found=
  while read -r key value _ || [ -n "${key:-}" ]; do
    if [ "$key" = "$want" ]; then
      found=$value
      break
    fi
  done < "$file"
  case $found in
    ''|*[!0-9]*) return 1 ;;
  esac
  printf '%s\n' "$found"
}

read_cpu() {
  file=$1
  while read -r label user nice system idle iowait irq softirq steal _ || [ -n "${label:-}" ]; do
    [ "$label" = cpu ] || continue
    iowait=${iowait:-0}
    irq=${irq:-0}
    softirq=${softirq:-0}
    steal=${steal:-0}
    for part in "$user" "$nice" "$system" "$idle" "$iowait" "$irq" "$softirq" "$steal"; do
      norm=$(u_norm "$part") || return 1
      [ -n "$norm" ] || return 1
    done
    printf '%s %s %s %s %s %s %s %s\n' \
      "$user" "$nice" "$system" "$idle" "$iowait" "$irq" "$softirq" "$steal"
    return 0
  done < "$file"
  return 1
}

eight_deltas() {
  first=$1
  second=$2
  # shellcheck disable=SC2086
  set -- $first
  b1=$1
  b2=$2
  b3=$3
  b4=$4
  b5=$5
  b6=$6
  b7=$7
  b8=$8
  # shellcheck disable=SC2086
  set -- $second
  d1=$(u_sub "$1" "$b1") || return 1
  d2=$(u_sub "$2" "$b2") || return 1
  d3=$(u_sub "$3" "$b3") || return 1
  d4=$(u_sub "$4" "$b4") || return 1
  d5=$(u_sub "$5" "$b5") || return 1
  d6=$(u_sub "$6" "$b6") || return 1
  d7=$(u_sub "$7" "$b7") || return 1
  d8=$(u_sub "$8" "$b8") || return 1
  printf '%s %s %s %s %s %s %s %s\n' "$d1" "$d2" "$d3" "$d4" "$d5" "$d6" "$d7" "$d8"
}

process_name() {
  dir=$1
  raw=
  tr_path=$(command -v tr || true)
  if [ -s "$dir/cmdline" ] && [ -n "$tr_path" ]; then
    raw=$(tr '\000' ' ' < "$dir/cmdline") || raw=
    set -f
    # shellcheck disable=SC2086
    set -- $raw
    set +f
    raw=${1:-}
  fi
  if [ -z "$raw" ] && [ -f "$dir/comm" ]; then
    IFS= read -r raw < "$dir/comm" || raw=
    raw=$(trim "$raw")
  fi
  raw=${raw##*/}
  case $raw in
    fogcast-*) ;;
    *)
      printf '%s\n' ''
      return 0
      ;;
  esac
  case $raw in
    *[!0-9A-Za-z._-]*)
      printf '%s\n' ''
      return 0
      ;;
  esac
  printf '%s\n' "$raw"
}

process_ticks() {
  file=$1
  [ -f "$file" ] || return 1
  IFS= read -r line < "$file" || return 1
  rest=${line##*)}
  set -f
  # shellcheck disable=SC2086
  set -- $rest
  set +f
  [ "$#" -ge 13 ] || return 1
  u_add "${12}" "${13}"
}

process_rss() {
  file=$1/status
  rss=0
  if [ -f "$file" ]; then
    while read -r key value _ || [ -n "${key:-}" ]; do
      if [ "$key" = VmRSS: ]; then
        rss=$value
        break
      fi
    done < "$file"
  fi
  case $rss in
    ''|*[!0-9]*) rss=0 ;;
  esac
  printf '%s\n' "$rss"
}

snapshot_processes() {
  mode=$1
  proc=$(kit_path /proc)
  out=
  for dir in "$proc"/[0-9]*; do
    [ -d "$dir" ] || continue
    pid=${dir##*/}
    case $pid in
      *[!0-9]*) continue ;;
    esac
    name=$(process_name "$dir")
    [ -n "$name" ] || continue
    if [ "$mode" = sample2 ]; then
      ticks=$(process_ticks "$dir/stat.sample2") || continue
    else
      ticks=$(process_ticks "$dir/stat") || continue
    fi
    rss=$(process_rss "$dir")
    out=$out$pid'|'$ticks'|'$name'|'$rss$nl
  done
  printf '%s' "$out"
}

lookup_ticks() {
  list=$1
  want=$2
  rest=$list
  while [ -n "$rest" ]; do
    case $rest in
      *"$nl"*)
        line=${rest%%"$nl"*}
        rest=${rest#*"$nl"}
        ;;
      *)
        line=$rest
        rest=
        ;;
    esac
    case $line in
      "$want|"*)
        tail=${line#*|}
        printf '%s\n' "${tail%%|*}"
        return 0
        ;;
    esac
  done
  return 1
}

char_ord() {
  c=$1
  rest=$alpha
  n=0
  while [ -n "$rest" ]; do
    d=${rest%"${rest#?}"}
    if [ "$d" = "$c" ]; then
      printf '%s\n' "$n"
      return 0
    fi
    n=$((n + 1))
    rest=${rest#?}
  done
  return 1
}

str_cmp() {
  a=$1
  b=$2
  while [ -n "$a" ] && [ -n "$b" ]; do
    da=${a%"${a#?}"}
    db=${b%"${b#?}"}
    if [ "$da" != "$db" ]; then
      oa=$(char_ord "$da") || return 1
      ob=$(char_ord "$db") || return 1
      if [ "$oa" -lt "$ob" ]; then
        printf '%s\n' -1
      else
        printf '%s\n' 1
      fi
      return 0
    fi
    a=${a#?}
    b=${b#?}
  done
  if [ -z "$a" ] && [ -z "$b" ]; then
    printf '%s\n' 0
  elif [ -z "$a" ]; then
    printf '%s\n' -1
  else
    printf '%s\n' 1
  fi
}

better() {
  a=$1
  b=$2
  # shellcheck disable=SC2086
  set -- $a
  ac=$1
  ar=$2
  an=$3
  ap=$4
  # shellcheck disable=SC2086
  set -- $b
  bc=$1
  br=$2
  bn=$3
  bp=$4
  if [ "$ac" -gt "$bc" ]; then
    return 0
  fi
  if [ "$ac" -lt "$bc" ]; then
    return 1
  fi
  cmp=$(u_cmp "$ar" "$br") || return 1
  if [ "$cmp" -gt 0 ]; then
    return 0
  fi
  if [ "$cmp" -lt 0 ]; then
    return 1
  fi
  cmp=$(str_cmp "$an" "$bn") || return 1
  if [ "$cmp" -lt 0 ]; then
    return 0
  fi
  if [ "$cmp" -gt 0 ]; then
    return 1
  fi
  cmp=$(u_cmp "$ap" "$bp") || return 1
  [ "$cmp" -lt 0 ]
}

drop_one() {
  rest=$1
  target=$2
  out=
  removed=0
  while [ -n "$rest" ]; do
    case $rest in
      *"$nl"*)
        line=${rest%%"$nl"*}
        rest=${rest#*"$nl"}
        ;;
      *)
        line=$rest
        rest=
        ;;
    esac
    if [ "$removed" -eq 0 ] && [ "$line" = "$target" ]; then
      removed=1
      continue
    fi
    [ -n "$line" ] || continue
    out=$out$line$nl
  done
  [ "$removed" -eq 1 ] || return 1
  printf '%s' "$out"
}

next_line() {
  rest=$1
  case $rest in
    *"$nl"*)
      printf '%s\n' "${rest%%"$nl"*}"
      ;;
    *)
      printf '%s\n' "$rest"
      ;;
  esac
}

advance_rest() {
  rest=$1
  case $rest in
    *"$nl"*)
      printf '%s' "${rest#*"$nl"}"
      ;;
    *)
      printf '%s' ''
      ;;
  esac
}

parse_df_text() {
  rest=$1
  while [ -n "$rest" ]; do
    line=$(next_line "$rest")
    rest=$(advance_rest "$rest")
    [ -n "$line" ] || continue
    set -f
    # shellcheck disable=SC2086
    set -- $line
    set +f
    [ "$#" -ge 6 ] || continue
    blocks=$2
    used=$3
    avail=$4
    cap=$5
    mount=$6
    case $blocks in
      *[!0-9]*) continue ;;
    esac
    case $used in
      *[!0-9]*) continue ;;
    esac
    case $avail in
      *[!0-9]*) continue ;;
    esac
    case $cap in
      *%) cap=${cap%\%} ;;
    esac
    case $cap in
      *[!0-9]*) continue ;;
    esac
    case $mount in
      /)
        [ "$fs_root_present" -eq 0 ] || continue
        fs_root_present=1
        fs_root_size=$blocks
        fs_root_used=$used
        fs_root_free=$avail
        fs_root_pct=$cap
        ;;
      /media/fat)
        [ "$fs_media_present" -eq 0 ] || continue
        fs_media_present=1
        fs_media_size=$blocks
        fs_media_used=$used
        fs_media_free=$avail
        fs_media_pct=$cap
        ;;
      /tmp)
        [ "$fs_tmp_present" -eq 0 ] || continue
        fs_tmp_present=1
        fs_tmp_size=$blocks
        fs_tmp_used=$used
        fs_tmp_free=$avail
        fs_tmp_pct=$cap
        ;;
    esac
  done
}

root=${FES_KIT_DIAG_ROOT:-}
root=${root%/}
if [ -n "$root" ] && [ ! -d "$root" ]; then
  die 'FES_KIT_DIAG_ROOT is not a directory'
fi

image_id=
image_id_source=none
take_id /etc/fes-image-id "$(kit_path /etc/fes-image-id)"
take_id /etc/fogcast-image-id "$(kit_path /etc/fogcast-image-id)"
take_id /usr/share/mister-runtime/image-id "$(kit_path /usr/share/mister-runtime/image-id)"
if [ -z "$image_id" ]; then
  read_os_release "$(kit_path /etc/os-release)"
fi
read_factory /etc/fes/factory.json "$(kit_path /etc/fes/factory.json)"
read_factory /.fes-bootstrap/etc/fes/factory.json "$(kit_path /.fes-bootstrap/etc/fes/factory.json)"
read_proc_version

uptime_file=$(kit_path /proc/uptime)
[ -f "$uptime_file" ] || die 'uptime is missing'
read -r uptime_seconds _ < "$uptime_file" || die 'uptime is unreadable'
decimal_ok "$uptime_seconds" || die 'uptime is not a decimal'

load_file=$(kit_path /proc/loadavg)
[ -f "$load_file" ] || die 'loadavg is missing'
read -r load_1 load_5 load_15 _ < "$load_file" || die 'loadavg is unreadable'
decimal_ok "$load_1" || die 'load is not a decimal'
decimal_ok "$load_5" || die 'load is not a decimal'
decimal_ok "$load_15" || die 'load is not a decimal'

cpu_count=0
cpuinfo=$(kit_path /proc/cpuinfo)
[ -f "$cpuinfo" ] || die 'cpuinfo is missing'
while read -r key _ || [ -n "${key:-}" ]; do
  if [ "$key" = processor ]; then
    cpu_count=$((cpu_count + 1))
  fi
done < "$cpuinfo"
[ "$cpu_count" -gt 0 ] || die 'cpu count is zero'

mem_file=$(kit_path /proc/meminfo)
[ -f "$mem_file" ] || die 'meminfo is missing'
mem_total=$(mem_value MemTotal: "$mem_file") || die 'MemTotal is missing'
mem_available=$(mem_value MemAvailable: "$mem_file") || die 'MemAvailable is missing'
swap_total=$(mem_value SwapTotal: "$mem_file") || die 'SwapTotal is missing'
swap_free=$(mem_value SwapFree: "$mem_file") || die 'SwapFree is missing'
if cma_total=$(mem_value CmaTotal: "$mem_file"); then
  cma_present=1
  cma_free=$(mem_value CmaFree: "$mem_file") || cma_free=0
else
  cma_present=0
  cma_total=0
  cma_free=0
fi

stat_file=$(kit_path /proc/stat)
stat_next=$(kit_path /proc/stat.sample2)
if [ -f "$stat_next" ]; then
  sample_seconds=0
  sample_mode=sample2
else
  if [ -n "$root" ]; then
    die 'fixture root has no proc/stat.sample2'
  fi
  sample_seconds=${FES_KIT_DIAG_SAMPLE_SECONDS:-1}
  case $sample_seconds in
    1|2|3|4|5) ;;
    *) die 'FES_KIT_DIAG_SAMPLE_SECONDS must be 1..5' ;;
  esac
  sample_mode=live
fi

first_cpu=$(read_cpu "$stat_file") || die 'cpu stat is missing'
first_procs=$(snapshot_processes stat)
if [ "$sample_mode" = live ]; then
  sleep_path=$(command -v sleep || true)
  [ -n "$sleep_path" ] || die 'sleep is missing'
  sleep "$sample_seconds"
  second_cpu=$(read_cpu "$stat_file") || die 'cpu stat is missing'
  second_procs=$(snapshot_processes stat)
else
  second_cpu=$(read_cpu "$stat_next") || die 'cpu stat sample2 is missing'
  second_procs=$(snapshot_processes sample2)
fi

deltas=$(eight_deltas "$first_cpu" "$second_cpu") || die 'cpu counters went backwards'
# shellcheck disable=SC2086
set -- $deltas
total=0
for part do
  small_ok "$part" || die 'cpu sample delta is too large'
  total=$((total + part))
done
idle_delta=$4
[ "$total" -gt 0 ] || die 'cpu sample window is empty'
cpu_idle=$(percent "$idle_delta" "$total") || die 'idle percent failed'
one_core=$((total / cpu_count))
[ "$one_core" -gt 0 ] || die 'cpu sample window is empty'

records=
rest=$second_procs
while [ -n "$rest" ]; do
  line=$(next_line "$rest")
  rest=$(advance_rest "$rest")
  [ -n "$line" ] || continue
  pid=${line%%|*}
  tail=${line#*|}
  ticks2=${tail%%|*}
  tail=${tail#*|}
  name=${tail%%|*}
  rss=${tail#*|}
  ticks1=$(lookup_ticks "$first_procs" "$pid") || continue
  delta=$(u_sub "$ticks2" "$ticks1") || die "process $pid ticks went backwards"
  small_ok "$delta" || die "process $pid sample delta is too large"
  cpu=$(percent "$delta" "$one_core") || die "process $pid cpu percent failed"
  records=$records$cpu' '$rss' '$name' '$pid$nl
done

ordered=
process_count=0
remaining=$records
while [ -n "$remaining" ]; do
  best=
  scan=$remaining
  while [ -n "$scan" ]; do
    line=$(next_line "$scan")
    scan=$(advance_rest "$scan")
    [ -n "$line" ] || continue
    if [ -z "$best" ] || better "$line" "$best"; then
      best=$line
    fi
  done
  [ -n "$best" ] || die 'process sort failed'
  ordered=$ordered$best$nl
  process_count=$((process_count + 1))
  next=$(drop_one "$remaining" "$best") || die 'process sort failed'
  remaining=$next
done

fs_root_present=0
fs_root_size=
fs_root_used=
fs_root_free=
fs_root_pct=
fs_media_present=0
fs_media_size=
fs_media_used=
fs_media_free=
fs_media_pct=
fs_tmp_present=0
fs_tmp_size=
fs_tmp_used=
fs_tmp_free=
fs_tmp_pct=

if [ -n "$root" ]; then
  df_file=$(kit_path df.txt)
  [ -f "$df_file" ] || die 'fixture df.txt is missing'
  df_text=$(while IFS= read -r line || [ -n "$line" ]; do
    printf '%s\n' "$line"
  done < "$df_file")
else
  df_path=$(command -v df || true)
  [ -n "$df_path" ] || die 'df is missing'
  df_text=$(df -P -k) || die 'df failed'
fi
parse_df_text "$df_text"

emit schema fogcast.kit-diagnostics.v1
emit image_id "$image_id"
emit image_id_source "$image_id_source"
emit uptime_seconds "$uptime_seconds"
emit load_1 "$load_1"
emit load_5 "$load_5"
emit load_15 "$load_15"
emit cpu_count "$cpu_count"
emit cpu_idle_percent "$cpu_idle"
emit cpu_sample_seconds "$sample_seconds"
emit mem_total_kib "$mem_total"
emit mem_available_kib "$mem_available"
emit swap_total_kib "$swap_total"
emit swap_free_kib "$swap_free"
emit cma_present "$cma_present"
emit cma_total_kib "$cma_total"
emit cma_free_kib "$cma_free"
emit fs.root.present "$fs_root_present"
emit fs.root.size_kib "$fs_root_size"
emit fs.root.used_kib "$fs_root_used"
emit fs.root.free_kib "$fs_root_free"
emit fs.root.used_percent "$fs_root_pct"
emit fs.media_fat.present "$fs_media_present"
emit fs.media_fat.size_kib "$fs_media_size"
emit fs.media_fat.used_kib "$fs_media_used"
emit fs.media_fat.free_kib "$fs_media_free"
emit fs.media_fat.used_percent "$fs_media_pct"
emit fs.tmp.present "$fs_tmp_present"
emit fs.tmp.size_kib "$fs_tmp_size"
emit fs.tmp.used_kib "$fs_tmp_used"
emit fs.tmp.free_kib "$fs_tmp_free"
emit fs.tmp.used_percent "$fs_tmp_pct"
emit process_count "$process_count"

n=0
rest=$ordered
while [ -n "$rest" ]; do
  line=$(next_line "$rest")
  rest=$(advance_rest "$rest")
  [ -n "$line" ] || continue
  n=$((n + 1))
  # shellcheck disable=SC2086
  set -- $line
  emit "process.$n.pid" "$4"
  emit "process.$n.name" "$3"
  emit "process.$n.cpu_percent" "$1"
  emit "process.$n.rss_kib" "$2"
done
