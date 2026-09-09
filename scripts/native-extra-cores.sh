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
package_dir=${FES_PONG_PACKAGE_DIR:-}
package_selection=${FES_PONG_PACKAGE_SELECTION:-}
if { [ -n "$package_dir" ] && [ -z "$package_selection" ]; } ||
   { [ -z "$package_dir" ] && [ -n "$package_selection" ]; }; then
  echo 'native-extra-cores: FES Pong package directory and selection are all-or-nothing' >&2
  exit 2
fi
package_enabled=0
if [ -n "$package_dir" ]; then
  case "$package_dir:$package_selection" in /*:/*) ;; *) echo 'native-extra-cores: FES Pong inputs must be absolute' >&2; exit 2 ;; esac
  [ -d "$package_dir" ] && [ ! -L "$package_dir" ] || { echo 'native-extra-cores: FES Pong package is not a directory' >&2; exit 2; }
  [ -f "$package_selection" ] && [ ! -L "$package_selection" ] || { echo 'native-extra-cores: FES Pong selection is not a file' >&2; exit 2; }
  package_enabled=1
fi
# Host preflight also calls this helper before entering the Linux container.
case "$(uname -s)" in
  Darwin) default_selector=$repo/bin/target-image-lock ;;
  *) default_selector=$repo/bin/target-image-lock-linux-amd64 ;;
esac
selector=${TARGET_IMAGE_LOCK_BIN:-$default_selector}
action=${1:-validate}
case "$action" in
  validate) exit 0 ;;
  count)
    if [ -n "$extras" ]; then count=5; else count=2; fi
    if [ "$package_enabled" -eq 1 ]; then count=$((count + 1)); fi
    echo "$count"
    exit 0
    ;;
  fetch|verify|install|verify-image|copy-records|build-inputs) ;;
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
package_cache=$cache/core-packages
package_record=$cache/fes-pong.package-selection.toml
find_cached_package() {
  find_single_package "$package_cache"
}
find_single_package() {
  package_root=$1
  [ -d "$package_root" ] && [ ! -L "$package_root" ] || return 1
  package_count=0
  package_entry=
  for candidate in "$package_root"/* "$package_root"/.[!.]* "$package_root"/..?*; do
    if [ -e "$candidate" ] || [ -L "$candidate" ]; then
      package_count=$((package_count + 1))
      package_entry=$candidate
    fi
  done
  [ "$package_count" -eq 1 ] && [ -d "$package_entry" ] && [ ! -L "$package_entry" ] || return 1
  printf '%s\n' "$package_entry"
}
if [ "$package_enabled" -eq 1 ]; then
  case "$action" in
    fetch)
      "$selector" select-package --package "$package_dir" --selection "$package_selection" \
        --cache "$cache" --output "$package_record"
      ;;
    verify|install)
      cached_package=$(find_cached_package) || { echo 'native-extra-cores: expected exactly one cached package' >&2; exit 1; }
      cmp "$package_selection" "$package_record"
      "$selector" verify-package --package "$cached_package" --selection "$package_record"
      if [ "$action" = install ]; then
        package_id=${cached_package##*/}
        installed=$target/usr/share/mister-runtime/core-packages/$package_id
        if [ -d "$target/usr/share/mister-runtime/core-packages" ]; then
          chmod -R u+w "$target/usr/share/mister-runtime/core-packages"
        fi
        rm -rf "$target/usr/share/mister-runtime/core-packages"
        install -d -m 0755 "$installed"
        install -m 0444 "$cached_package/manifest.toml" "$installed/manifest.toml"
        install -m 0444 "$cached_package/core.rbf" "$installed/core.rbf"
        chmod 0555 "$installed"
        install -D -m 0444 "$package_record" "$target/usr/share/mister-runtime/selections/fes-pong.package.toml"
        "$selector" verify-package --package "$installed" --selection "$package_record"
      fi
      ;;
    verify-image)
      cmp "$package_selection" "$package_record"
      installed_root=$target/usr/share/mister-runtime/core-packages
      installed_package=$(find_single_package "$installed_root") || { echo 'native-extra-cores: installed package set is not closed' >&2; exit 1; }
      "$selector" verify-package --package "$installed_package" --selection "$package_record"
      installed_record=$target/usr/share/mister-runtime/selections/fes-pong.package.toml
      [ -f "$installed_record" ] && [ ! -L "$installed_record" ]
      [ "$(stat -c %a "$installed_record")" = 444 ]
      cmp "$package_record" "$installed_record"
      ;;
    build-inputs)
      cmp "$package_selection" "$package_record"
      installed_root=$target/usr/share/mister-runtime/core-packages
      installed_package=$(find_single_package "$installed_root") || { echo 'native-extra-cores: installed package set is not closed' >&2; exit 1; }
      "$selector" verify-package --package "$installed_package" --selection "$package_record" --print-inputs
      ;;
    copy-records)
      cmp "$package_selection" "$package_record"
      install -m 0444 "$package_record" "$target/fes-pong.package-selection.toml"
      ;;
  esac
else
  case "$action" in
    fetch)
      if [ -d "$package_cache" ]; then chmod -R u+w "$package_cache"; fi
      rm -rf "$package_cache" "$package_record" "$package_record.previous"
      ;;
    verify)
      [ ! -e "$package_cache" ] && [ ! -L "$package_cache" ]
      [ ! -e "$package_record" ] && [ ! -L "$package_record" ]
      ;;
    install)
      if [ -d "$target/usr/share/mister-runtime/core-packages" ]; then
        chmod -R u+w "$target/usr/share/mister-runtime/core-packages"
      fi
      rm -rf "$target/usr/share/mister-runtime/core-packages"
      rm -f "$target/usr/share/mister-runtime/selections/fes-pong.package.toml"
      ;;
    verify-image)
      [ ! -e "$target/usr/share/mister-runtime/core-packages" ] && [ ! -L "$target/usr/share/mister-runtime/core-packages" ]
      [ ! -e "$target/usr/share/mister-runtime/selections/fes-pong.package.toml" ] && [ ! -L "$target/usr/share/mister-runtime/selections/fes-pong.package.toml" ]
      ;;
    build-inputs) : ;;
    copy-records) rm -f "$target/fes-pong.package-selection.toml" ;;
  esac
fi
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
