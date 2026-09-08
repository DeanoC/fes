#!/bin/sh
# Additional source-built cores share one opt-in admission/install path.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
systems=${NATIVE_RUNTIME_SYSTEMS:-megadrive}
case "$systems" in
  megadrive) extras= ;;
  'megadrive pong snes nes') extras='pong snes nes' ;;
  *) echo 'native-extra-cores: expected megadrive or megadrive pong snes nes' >&2; exit 2 ;;
esac
# Host preflight also calls this helper before entering the Linux container.
case "$(uname -s)" in
  Darwin) default_selector=$repo/bin/target-image-lock ;;
  *) default_selector=$repo/bin/target-image-lock-linux-amd64 ;;
esac
selector=${TARGET_IMAGE_LOCK_BIN:-$default_selector}
action=${1:-validate}
case "$action" in
  validate) exit 0 ;;
  count) if [ -n "$extras" ]; then echo 5; else echo 2; fi; exit 0 ;;
  fetch|verify|install|verify-image|copy-records) ;;
  *) exit 2 ;;
esac
cache=$2
target=${3:-}
for system in $extras; do
  record=$cache/$system.selection.toml
  artifact=$cache/$system.rbf
  case "$action" in
    fetch)
      case "$system" in
        pong) bundle=${PONG_RBF_BUNDLE:-} ;;
        snes) bundle=${SNES_RBF_BUNDLE:-} ;;
        nes) bundle=${NES_RBF_BUNDLE:-} ;;
      esac
      [ -n "$bundle" ] || { echo "native-extra-cores: $system bundle required" >&2; exit 2; }
      "$selector" select-core --system "$system" --source source-built --bundle "$bundle" --cache "$cache" --output "$record"
      ;;
    verify|install)
      "$selector" verify-core --system "$system" --artifact "$artifact" --output "$record"
      if [ "$action" = install ]; then
        install -D -m 0644 "$artifact" "$target/usr/share/mister-runtime/cores/$system.rbf"
        install -D -m 0444 "$record" "$target/usr/share/mister-runtime/selections/$system.toml"
        "$selector" verify-core --system "$system" --artifact "$target/usr/share/mister-runtime/cores/$system.rbf" --output "$record"
      fi
      ;;
    verify-image)
      "$selector" verify-core --system "$system" --artifact "$target/usr/share/mister-runtime/cores/$system.rbf" --output "$record"
      [ -f "$target/usr/share/mister-runtime/selections/$system.toml" ] && [ ! -L "$target/usr/share/mister-runtime/selections/$system.toml" ]
      mode=$(stat -c %a "$target/usr/share/mister-runtime/selections/$system.toml")
      [ "$mode" = 444 ] || { echo 'installed selection must be sealed' >&2; exit 1; }
      cmp "$record" "$target/usr/share/mister-runtime/selections/$system.toml"
      ;;
    copy-records)
      [ -f "$record" ] && [ ! -L "$record" ]
      mkdir -p "$target"
      install -m 0444 "$record" "$target/$system.selection.toml"
      ;;
  esac
done
case "$action" in
  install)
    if [ -z "$extras" ]; then
      rm -f "$target/usr/share/mister-runtime/cores/pong.rbf" "$target/usr/share/mister-runtime/cores/snes.rbf" "$target/usr/share/mister-runtime/cores/nes.rbf" "$target/usr/share/mister-runtime/selections/pong.toml" "$target/usr/share/mister-runtime/selections/snes.toml" "$target/usr/share/mister-runtime/selections/nes.toml"
    fi
    ;;
  copy-records)
    if [ -z "$extras" ]; then rm -f "$target/pong.selection.toml" "$target/snes.selection.toml" "$target/nes.selection.toml"; fi
    ;;
  verify-image)
    if [ -z "$extras" ]; then
      for system in pong snes nes; do
        [ ! -e "$target/usr/share/mister-runtime/cores/$system.rbf" ] && [ ! -L "$target/usr/share/mister-runtime/cores/$system.rbf" ]
        [ ! -e "$target/usr/share/mister-runtime/selections/$system.toml" ] && [ ! -L "$target/usr/share/mister-runtime/selections/$system.toml" ]
      done
    fi
    ;;
esac
