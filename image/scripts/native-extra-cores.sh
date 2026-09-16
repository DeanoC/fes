#!/bin/sh
# FES core packages share one explicit admission/install path.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
native_mode=${NATIVE_RUNTIME_MODE:-package-only}
[ "$native_mode" = package-only ] || {
  echo 'native-extra-cores: native runtime mode must be package-only' >&2
  exit 2
}
package_ids=${FES_PACKAGE_IDS:-}
selected_packages=
selected_package_count=0
package_core_for() {
  case "$1" in
    fes.pong) package_core=pong ;;
    fes.zx81) package_core=zx81 ;;
    fes.coleco) package_core=coleco ;;
    *) return 1 ;;
  esac
}
package_dir_for() {
  case "$1" in
    fes.pong) package_dir=${FES_PONG_PACKAGE_DIR:-} ;;
    fes.zx81) package_dir=${FES_ZX81_PACKAGE_DIR:-} ;;
    fes.coleco) package_dir=${FES_COLECO_PACKAGE_DIR:-} ;;
    *) return 1 ;;
  esac
}
package_selection_for() {
  case "$1" in
    fes.pong) package_selection=${FES_PONG_PACKAGE_SELECTION:-} ;;
    fes.zx81) package_selection=${FES_ZX81_PACKAGE_SELECTION:-} ;;
    fes.coleco) package_selection=${FES_COLECO_PACKAGE_SELECTION:-} ;;
    *) return 1 ;;
  esac
}
package_record_name_for() {
  package_core_for "$1"
  package_record_name=fes-$package_core.package-selection.toml
}
package_installed_record_name_for() {
  package_core_for "$1"
  package_installed_record_name=fes-$package_core.package.toml
}
package_value_from_file() {
  awk -v wanted="$2" '
    $0 ~ "^[[:space:]]*" wanted "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == sprintf("%c", 34) || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$1"
}
package_core_id_from_file() {
  package_value_from_file "$1" core_id
}
validate_package_set() {
  [ -n "$package_ids" ] || {
    echo 'native-extra-cores: FES_PACKAGE_IDS is required in package-only mode' >&2
    exit 2
  }
  case "$package_ids" in
    ,*|*,|*,,*)
      echo 'native-extra-cores: FES_PACKAGE_IDS contains an empty package ID' >&2
      exit 2
      ;;
  esac
  remaining=$package_ids
  selected_packages=
  selected_package_count=0
  seen_ids='|'
  while :; do
    case "$remaining" in
      *,*) selected_id=${remaining%%,*}; remaining=${remaining#*,} ;;
      *) selected_id=$remaining; remaining= ;;
    esac
    package_core_for "$selected_id" || {
      printf 'native-extra-cores: unsupported FES package ID: %s\n' "$selected_id" >&2
      exit 2
    }
    case "$seen_ids" in
      *"|$selected_id|"*)
        printf 'native-extra-cores: duplicate FES package ID: %s\n' "$selected_id" >&2
        exit 2
        ;;
    esac
    seen_ids=$seen_ids$selected_id'|'
    package_dir_for "$selected_id"
    package_selection_for "$selected_id"
    [ -n "$package_dir" ] && [ -n "$package_selection" ] || {
      printf 'native-extra-cores: inputs are required for %s\n' "$selected_id" >&2
      exit 2
    }
    case "$package_dir" in
      /*) : ;;
      *) printf 'native-extra-cores: %s package directory must be absolute\n' "$selected_id" >&2; exit 2 ;;
    esac
    case "$package_selection" in
      /*) : ;;
      *) printf 'native-extra-cores: %s selection must be absolute\n' "$selected_id" >&2; exit 2 ;;
    esac
    [ -d "$package_dir" ] && [ ! -L "$package_dir" ] || {
      printf 'native-extra-cores: %s package is not a directory\n' "$selected_id" >&2
      exit 2
    }
    [ -f "$package_selection" ] && [ ! -L "$package_selection" ] || {
      printf 'native-extra-cores: %s selection is not a file\n' "$selected_id" >&2
      exit 2
    }
    [ "$(package_core_id_from_file "$package_selection")" = "$selected_id" ] || {
      printf 'native-extra-cores: %s selection core ID is misidentified\n' "$selected_id" >&2
      exit 2
    }
    selected_packages="$selected_packages $selected_id"
    selected_package_count=$((selected_package_count + 1))
    [ -n "$remaining" ] || break
  done
  selected_packages=${selected_packages# }
  for candidate_id in fes.pong fes.zx81 fes.coleco; do
    case " $selected_packages " in
      *" $candidate_id "*) ;;
      *)
        package_dir_for "$candidate_id"
        package_selection_for "$candidate_id"
        [ -z "$package_dir" ] && [ -z "$package_selection" ] || {
          printf 'native-extra-cores: unselected inputs supplied for %s\n' "$candidate_id" >&2
          exit 2
        }
        ;;
    esac
  done
}
validate_package_set
# Host preflight also calls this helper before entering the Linux container.
action=${1:-validate}
selector=${TARGET_IMAGE_LOCK_BIN:-}
if [ -z "$selector" ] && [ "$action" != validate ] && [ "$action" != count ]; then
  fogcast=${FOGCAST_DIR:?FOGCAST_DIR or TARGET_IMAGE_LOCK_BIN is required}
  case "$(uname -s)" in
    Darwin) selector=$fogcast/bin/target-image-lock ;;
    *) selector=$fogcast/bin/target-image-lock-linux-amd64 ;;
  esac
fi
case "$action" in
  validate) exit 0 ;;
  count)
    count=$((1 + selected_package_count))
    echo "$count"
    exit 0
    ;;
  fetch|verify|install|verify-image|copy-records|build-inputs) ;;
  *) exit 2 ;;
esac
cache=$2
target=${3:-}
package_cache=$cache/core-packages
package_record_path_for() {
  package_record_name_for "$1"
  package_record=$cache/$package_record_name
}
package_installed_record_path_for() {
  package_installed_record_name_for "$1"
  installed_record=$target/usr/share/mister-runtime/selections/$package_installed_record_name
}
verify_sealed_package_dir() {
  sealed_package=$1
  [ -d "$sealed_package" ] && [ ! -L "$sealed_package" ] || return 1
  [ "$(stat -c %a "$sealed_package")" = 555 ] || return 1
  sealed_count=0
  for sealed_entry in "$sealed_package"/* "$sealed_package"/.[!.]* "$sealed_package"/..?*; do
    if [ -e "$sealed_entry" ] || [ -L "$sealed_entry" ]; then
      case "${sealed_entry##*/}" in
        manifest.toml|core.rbf) ;;
        *) return 1 ;;
      esac
      [ -f "$sealed_entry" ] && [ ! -L "$sealed_entry" ] || return 1
      [ "$(stat -c %a "$sealed_entry")" = 444 ] || return 1
      sealed_count=$((sealed_count + 1))
    fi
  done
  [ "$sealed_count" -eq 2 ]
}
validate_cached_package_set() {
  [ -d "$package_cache" ] && [ ! -L "$package_cache" ] || {
    echo 'native-extra-cores: cached package set is missing' >&2
    exit 1
  }
  cached_ids=
  for selected_id in $selected_packages; do
    package_record_path_for "$selected_id"
    [ -f "$package_record" ] && [ ! -L "$package_record" ] || {
      printf 'native-extra-cores: cached selection is missing for %s\n' "$selected_id" >&2
      exit 1
    }
    package_dir_for "$selected_id"
    package_selection_for "$selected_id"
    cmp "$package_selection" "$package_record" || {
      printf 'native-extra-cores: cached selection differs for %s\n' "$selected_id" >&2
      exit 1
    }
    [ "$(package_core_id_from_file "$package_record")" = "$selected_id" ] || {
      printf 'native-extra-cores: cached selection core ID is misidentified for %s\n' "$selected_id" >&2
      exit 1
    }
    package_id=$(package_value_from_file "$package_record" package_id)
    printf '%s\n' "$package_id" | grep -Eq '^[0-9a-f]{64}$' || {
      printf 'native-extra-cores: invalid cached package ID for %s\n' "$selected_id" >&2
      exit 1
    }
    case " $cached_ids " in
      *" $package_id "*)
        printf 'native-extra-cores: duplicate cached package ID: %s\n' "$package_id" >&2
        exit 1
        ;;
    esac
    cached_ids="$cached_ids $package_id"
    cached_package=$package_cache/$package_id
    verify_sealed_package_dir "$cached_package" || {
      printf 'native-extra-cores: cached package is not sealed: %s\n' "$package_id" >&2
      exit 1
    }
  done
  cached_ids=${cached_ids# }
  cached_count=0
  for cached_entry in "$package_cache"/* "$package_cache"/.[!.]* "$package_cache"/..?*; do
    if [ -e "$cached_entry" ] || [ -L "$cached_entry" ]; then
      [ -d "$cached_entry" ] && [ ! -L "$cached_entry" ] || {
        echo 'native-extra-cores: cached package set contains a non-directory entry' >&2
        exit 1
      }
      cached_id=${cached_entry##*/}
      case " $cached_ids " in
        *" $cached_id "*) ;;
        *)
          printf 'native-extra-cores: cached package set contains an extra package: %s\n' "$cached_id" >&2
          exit 1
          ;;
      esac
      cached_count=$((cached_count + 1))
    fi
  done
  [ "$cached_count" -eq "$selected_package_count" ] || {
    echo 'native-extra-cores: cached package set is not closed' >&2
    exit 1
  }
  for candidate_id in fes.pong fes.zx81 fes.coleco; do
    case " $selected_packages " in
      *" $candidate_id "*) ;;
      *)
        package_record_path_for "$candidate_id"
        [ ! -e "$package_record" ] && [ ! -L "$package_record" ] || {
          printf 'native-extra-cores: cached selection exists for unselected %s\n' "$candidate_id" >&2
          exit 1
        }
        ;;
    esac
  done
}
verify_cached_packages() {
  validate_cached_package_set
  for selected_id in $selected_packages; do
    package_record_path_for "$selected_id"
    package_id=$(package_value_from_file "$package_record" package_id)
    cached_package=$package_cache/$package_id
    "$selector" verify-package --core-id "$selected_id" \
      --package "$cached_package" --selection "$package_record"
  done
}
validate_installed_package_set() {
  installed_root=$target/usr/share/mister-runtime/core-packages
  [ -d "$installed_root" ] && [ ! -L "$installed_root" ] || {
    echo 'native-extra-cores: installed package set is missing' >&2
    exit 1
  }
  installed_ids=
  for selected_id in $selected_packages; do
    package_record_path_for "$selected_id"
    package_id=$(package_value_from_file "$package_record" package_id)
    installed_package=$installed_root/$package_id
    verify_sealed_package_dir "$installed_package" || {
      printf 'native-extra-cores: installed package is not sealed: %s\n' "$package_id" >&2
      exit 1
    }
    "$selector" verify-package --core-id "$selected_id" \
      --package "$installed_package" --selection "$package_record"
    package_installed_record_path_for "$selected_id"
    [ -f "$installed_record" ] && [ ! -L "$installed_record" ] || {
      printf 'native-extra-cores: installed selection is missing for %s\n' "$selected_id" >&2
      exit 1
    }
    [ "$(stat -c %a "$installed_record")" = 444 ] || {
      printf 'native-extra-cores: installed selection is not sealed for %s\n' "$selected_id" >&2
      exit 1
    }
    cmp "$package_record" "$installed_record" || {
      printf 'native-extra-cores: installed selection differs for %s\n' "$selected_id" >&2
      exit 1
    }
    case " $installed_ids " in
      *" $package_id "*)
        printf 'native-extra-cores: duplicate installed package ID: %s\n' "$package_id" >&2
        exit 1
        ;;
    esac
    installed_ids="$installed_ids $package_id"
  done
  installed_ids=${installed_ids# }
  installed_count=0
  for installed_entry in "$installed_root"/* "$installed_root"/.[!.]* "$installed_root"/..?*; do
    if [ -e "$installed_entry" ] || [ -L "$installed_entry" ]; then
      [ -d "$installed_entry" ] && [ ! -L "$installed_entry" ] || {
        echo 'native-extra-cores: installed package set contains a non-directory entry' >&2
        exit 1
      }
      installed_id=${installed_entry##*/}
      case " $installed_ids " in
        *" $installed_id "*) ;;
        *)
          printf 'native-extra-cores: installed package set contains an extra package: %s\n' "$installed_id" >&2
          exit 1
          ;;
      esac
      installed_count=$((installed_count + 1))
    fi
  done
  [ "$installed_count" -eq "$selected_package_count" ] || {
    echo 'native-extra-cores: installed package set is not closed' >&2
    exit 1
  }
  for candidate_id in fes.pong fes.zx81 fes.coleco; do
    package_installed_record_path_for "$candidate_id"
    case " $selected_packages " in
      *" $candidate_id "*) ;;
      *)
        [ ! -e "$installed_record" ] && [ ! -L "$installed_record" ] || {
          printf 'native-extra-cores: installed selection exists for unselected %s\n' "$candidate_id" >&2
          exit 1
        }
        ;;
    esac
  done
}
clean_package_cache() {
  if [ -e "$package_cache" ] || [ -L "$package_cache" ]; then
    [ -d "$package_cache" ] && [ ! -L "$package_cache" ] || {
      echo 'native-extra-cores: package cache is not a directory' >&2
      exit 1
    }
    for cached_entry in "$package_cache"/* "$package_cache"/.[!.]* "$package_cache"/..?*; do
      if [ -e "$cached_entry" ] || [ -L "$cached_entry" ]; then
        [ -d "$cached_entry" ] && [ ! -L "$cached_entry" ] || {
          echo 'native-extra-cores: package cache contains an invalid entry' >&2
          exit 1
        }
      fi
    done
    chmod -R u+rwX "$package_cache"
    rm -rf "$package_cache"
  fi
  for candidate_id in fes.pong fes.zx81 fes.coleco; do
    package_record_path_for "$candidate_id"
    for stale_record in "$package_record" "${package_record}.previous"; do
      if [ -e "$stale_record" ] || [ -L "$stale_record" ]; then
        [ -f "$stale_record" ] && [ ! -L "$stale_record" ] || {
          printf 'native-extra-cores: invalid cached selection path: %s\n' "$stale_record" >&2
          exit 1
        }
        rm -f "$stale_record"
      fi
    done
  done
}
validate_package_records() {
  for selected_id in $selected_packages; do
    package_record_path_for "$selected_id"
    [ -f "$package_record" ] && [ ! -L "$package_record" ] || {
      printf 'native-extra-cores: cached selection is missing for %s\n' "$selected_id" >&2
      exit 1
    }
    package_dir_for "$selected_id"
    package_selection_for "$selected_id"
    cmp "$package_selection" "$package_record" || {
      printf 'native-extra-cores: cached selection differs for %s\n' "$selected_id" >&2
      exit 1
    }
    [ "$(package_core_id_from_file "$package_record")" = "$selected_id" ] || {
      printf 'native-extra-cores: cached selection core ID is misidentified for %s\n' "$selected_id" >&2
      exit 1
    }
    [ "$(stat -c %a "$package_record")" = 444 ] || {
      printf 'native-extra-cores: cached selection is not sealed for %s\n' "$selected_id" >&2
      exit 1
    }
  done
}
if [ "$native_mode" = package-only ]; then
  case "$action" in
    fetch)
      clean_package_cache
      for selected_id in $selected_packages; do
        package_dir_for "$selected_id"
        package_selection_for "$selected_id"
        package_record_path_for "$selected_id"
        "$selector" select-package --core-id "$selected_id" \
          --package "$package_dir" --selection "$package_selection" \
          --cache "$cache" --output "$package_record"
      done
      verify_cached_packages
      ;;
    verify)
      verify_cached_packages
      ;;
    install)
      verify_cached_packages
      installed_root=$target/usr/share/mister-runtime/core-packages
      if [ -e "$installed_root" ] || [ -L "$installed_root" ]; then
        [ -d "$installed_root" ] && [ ! -L "$installed_root" ] || {
          echo 'native-extra-cores: installed package root is not a directory' >&2
          exit 1
        }
        for installed_entry in "$installed_root"/* "$installed_root"/.[!.]* "$installed_root"/..?*; do
          if [ -e "$installed_entry" ] || [ -L "$installed_entry" ]; then
            [ -d "$installed_entry" ] && [ ! -L "$installed_entry" ] || {
              echo 'native-extra-cores: installed package root contains an invalid entry' >&2
              exit 1
            }
          fi
        done
        chmod -R u+rwX "$installed_root"
        rm -rf "$installed_root"
      fi
      for candidate_id in fes.pong fes.zx81 fes.coleco; do
        package_installed_record_path_for "$candidate_id"
        if [ -e "$installed_record" ] || [ -L "$installed_record" ]; then
          [ -f "$installed_record" ] && [ ! -L "$installed_record" ] || {
            printf 'native-extra-cores: invalid installed selection path: %s\n' "$installed_record" >&2
            exit 1
          }
          rm -f "$installed_record"
        fi
      done
      for selected_id in $selected_packages; do
        package_record_path_for "$selected_id"
        package_id=$(package_value_from_file "$package_record" package_id)
        cached_package=$package_cache/$package_id
        installed=$target/usr/share/mister-runtime/core-packages/$package_id
        install -d -m 0755 "$installed"
        install -m 0444 "$cached_package/manifest.toml" "$installed/manifest.toml"
        install -m 0444 "$cached_package/core.rbf" "$installed/core.rbf"
        chmod 0555 "$installed"
        package_installed_record_path_for "$selected_id"
        install -D -m 0444 "$package_record" "$installed_record"
      done
      validate_installed_package_set
      ;;
    verify-image)
      verify_cached_packages
      validate_installed_package_set
      ;;
    build-inputs)
      verify_cached_packages >&2
      validate_installed_package_set >&2
      for selected_id in $selected_packages; do
        package_record_path_for "$selected_id"
        package_id=$(package_value_from_file "$package_record" package_id)
        cached_package=$package_cache/$package_id
        "$selector" verify-package --core-id "$selected_id" \
          --package "$cached_package" --selection "$package_record" --print-inputs
      done
      ;;
    copy-records)
      validate_package_records
      mkdir -p "$target"
      for candidate_id in fes.pong fes.zx81 fes.coleco; do
        package_core_for "$candidate_id"
        destination=$target/fes-$package_core.package-selection.toml
        case " $selected_packages " in
          *" $candidate_id "*) ;;
          *)
            if [ -e "$destination" ] || [ -L "$destination" ]; then
              [ -f "$destination" ] && [ ! -L "$destination" ] || {
                printf 'native-extra-cores: invalid copied selection path: %s\n' "$destination" >&2
                exit 1
              }
              rm -f "$destination"
            fi
            ;;
        esac
      done
      for selected_id in $selected_packages; do
        package_record_path_for "$selected_id"
        package_core_for "$selected_id"
        install -m 0444 "$package_record" "$target/fes-$package_core.package-selection.toml"
      done
      ;;
  esac
fi
