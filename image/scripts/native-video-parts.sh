#!/bin/sh
# Retain factory video assets separately from canonical core packages.
set -eu
action=$1
cache=$2
target=${3:-}
source=${FES_VIDEO_PARTS_DIR:-}
selector=${TARGET_IMAGE_LOCK_BIN:?TARGET_IMAGE_LOCK_BIN is required}
tree=$cache/core-video-parts
record=$cache/fes-core-video-parts.json
installed=$target/usr/share/mister-runtime/core-video-parts
installed_record=$target/usr/share/mister-runtime/selections/fes-core-video-parts.json

remove_tree() {
  if [ -e "$1" ] || [ -L "$1" ]; then
    [ -d "$1" ] && [ ! -L "$1" ] || {
      echo 'native-video-parts: unsafe stale video tree' >&2; exit 1;
    }
    find -P "$1" -type d -exec chmod u+w {} \;
    rm -rf "$1"
  fi
}
remove_record() {
  if [ -e "$1" ] || [ -L "$1" ]; then
    [ -f "$1" ] && [ ! -L "$1" ] || {
      echo 'native-video-parts: unsafe stale video index' >&2; exit 1;
    }
    rm -f "$1"
  fi
}
verify_record() {
  [ -f "$record" ] && [ ! -L "$record" ] && [ "$(stat -c %a "$record")" = 444 ] || {
    echo 'native-video-parts: external video index must be sealed' >&2; exit 1;
  }
  cmp "$source/index.json" "$record" || {
    echo 'native-video-parts: cached index differs from selected video assets' >&2; exit 1;
  }
}
verify_cache() {
  if [ -n "$source" ]; then
    verify_record
    "$selector" verify-video-parts --parts "$tree" --packages "$cache/core-packages" --selection "$record"
  else
    [ ! -e "$tree" ] && [ ! -L "$tree" ] && [ ! -e "$record" ] && [ ! -L "$record" ] || {
      echo 'native-video-parts: unselected factory video assets remain in cache' >&2; exit 1;
    }
    "$selector" verify-video-coverage --packages "$cache/core-packages"
  fi
}
verify_image() {
  if [ -n "$source" ]; then
    [ -f "$installed_record" ] && [ ! -L "$installed_record" ] && [ "$(stat -c %a "$installed_record")" = 444 ] || {
      echo 'native-video-parts: installed video selection must be sealed' >&2; exit 1;
    }
    cmp "$record" "$installed_record"
    "$selector" verify-video-parts --parts "$installed" --packages "$target/usr/share/mister-runtime/core-packages" --selection "$record"
  else
    [ ! -e "$installed" ] && [ ! -L "$installed" ] && [ ! -e "$installed_record" ] && [ ! -L "$installed_record" ] || {
      echo 'native-video-parts: image retains unselected factory video assets' >&2; exit 1;
    }
    "$selector" verify-video-coverage --packages "$target/usr/share/mister-runtime/core-packages"
  fi
}
case "$action" in
  fetch)
    if [ -n "$source" ]; then
      "$selector" select-video-parts --parts "$source" --packages "$cache/core-packages" --cache "$cache"
    else
      remove_tree "$tree"
      remove_record "$record"
    fi
    verify_cache
    ;;
  verify) verify_cache ;;
  install)
    verify_cache
    remove_tree "$installed"
    remove_record "$installed_record"
    if [ -n "$source" ]; then
      cp -R "$tree" "$installed"
      install -D -m 0444 "$record" "$installed_record"
    fi
    verify_image
    ;;
  verify-image) verify_cache; verify_image ;;
  copy-records)
    if [ -n "$source" ]; then
      verify_record
      install -m 0444 "$record" "$target/fes-core-video-parts.json"
    else
      remove_record "$target/fes-core-video-parts.json"
    fi
    ;;
  build-inputs)
    verify_cache >&2
    verify_image >&2
    if [ -n "$source" ]; then
      "$selector" verify-video-parts --parts "$installed" --packages "$target/usr/share/mister-runtime/core-packages" --selection "$record" --print-inputs
    fi
    ;;
  *) exit 2 ;;
esac
