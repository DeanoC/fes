#!/bin/sh
set -eu

# Prove exact ui/tenfoot prefixes are rejected for ui/kitlauncher and
# cmd/fogcast-kit, hostclient and ui/shared are allowed, transitive tenfoot
# is rejected, and rg/go/missing-dir fail closed.

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
guard=$here/ui-kit-tenfoot-boundary_test.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

write_mod() {
  printf 'module github.com/DeanoC/FogCast\n\ngo 1.24.0\n' >"$root/go.mod"
}

write_pkg() {
  dir=$1
  pkg=$2
  import=$3
  mkdir -p "$root/$dir"
  if [ -n "$import" ]; then
    printf 'package %s\n\nimport _ "%s"\n' "$pkg" "$import" >"$root/$dir/doc.go"
  else
    printf 'package %s\n' "$pkg" >"$root/$dir/doc.go"
  fi
}

setup_tree() {
  root=$tmp/tree
  rm -rf "$root"
  mkdir -p "$root"
  write_mod
  write_pkg hostclient hostclient ''
  write_pkg ui/shared shared ''
  write_pkg ui/tenfoot tenfoot ''
  write_pkg ui/tenfoot/theme theme ''
  write_pkg ui/tenfootprint tenfootprint ''
  write_pkg ui/kitlauncher kitlauncher 'github.com/DeanoC/FogCast/hostclient'
  write_pkg cmd/fogcast-kit main 'github.com/DeanoC/FogCast/hostclient'
}

run_guard() {
  set +e
  out=$(FOGCAST_BOUNDARY_ROOT=$root sh "$guard" 2>&1)
  status=$?
  set -e
}

expect() {
  name=$1
  want=$2
  needle=$3
  if [ "$status" -ne "$want" ]; then
    printf '%s: status %s want %s\n%s\n' "$name" "$status" "$want" "$out" >&2
    exit 1
  fi
  if [ -n "$needle" ] && ! printf '%s\n' "$out" | grep -q "$needle"; then
    printf '%s: missing %s\n%s\n' "$name" "$needle" "$out" >&2
    exit 1
  fi
  case $name in
    allow_*)
      if printf '%s\n' "$out" | grep -q 'ui/tenfoot'; then
        printf '%s: allowed import treated as tenfoot\n%s\n' "$name" "$out" >&2
        exit 1
      fi
      ;;
  esac
}

setup_tree
run_guard
expect allow_hostclient 0 ''

setup_tree
write_pkg ui/kitlauncher kitlauncher 'github.com/DeanoC/FogCast/ui/shared'
write_pkg cmd/fogcast-kit main 'github.com/DeanoC/FogCast/ui/shared'
run_guard
expect allow_shared 0 ''

setup_tree
write_pkg ui/kitlauncher kitlauncher 'github.com/DeanoC/FogCast/ui/tenfootprint'
write_pkg cmd/fogcast-kit main 'github.com/DeanoC/FogCast/ui/tenfootprint'
run_guard
expect allow_tenfootprint 0 ''

setup_tree
write_pkg ui/kitlauncher kitlauncher 'github.com/DeanoC/FogCast/ui/tenfoot'
run_guard
expect forbid_kitlauncher_tenfoot 1 'kit launcher imports ui/tenfoot'

setup_tree
write_pkg cmd/fogcast-kit main 'github.com/DeanoC/FogCast/ui/tenfoot'
run_guard
expect forbid_cmd_tenfoot 1 'fogcast-kit imports ui/tenfoot'

setup_tree
write_pkg ui/kitlauncher kitlauncher 'github.com/DeanoC/FogCast/ui/tenfoot/theme'
run_guard
expect forbid_kitlauncher_subpackage 1 'kit launcher imports ui/tenfoot'

setup_tree
write_pkg cmd/fogcast-kit main 'github.com/DeanoC/FogCast/ui/tenfoot/theme'
run_guard
expect forbid_cmd_subpackage 1 'fogcast-kit imports ui/tenfoot'

setup_tree
printf 'package kitlauncher\n\nimport _ "github.com/DeanoC/FogCast/helper"\n' >"$root/ui/kitlauncher/doc.go"
write_pkg helper helper 'github.com/DeanoC/FogCast/ui/tenfoot'
run_guard
expect forbid_kitlauncher_transitive 1 'kit launcher transitively depends on ui/tenfoot'

setup_tree
printf 'package main\n\nimport _ "github.com/DeanoC/FogCast/helper"\n' >"$root/cmd/fogcast-kit/doc.go"
write_pkg helper helper 'github.com/DeanoC/FogCast/ui/tenfoot'
run_guard
expect forbid_cmd_transitive 1 'fogcast-kit transitively depends on ui/tenfoot'

setup_tree
printf 'package kitlauncher\n\nimport (\n\t_ "github.com/DeanoC/FogCast/hostclient"\n)\n' >"$root/ui/kitlauncher/doc.go"
printf 'package kitlauncher\n\nimport _ "github.com/DeanoC/FogCast/ui/tenfoot"\n' >"$root/ui/kitlauncher/doc_test.go"
run_guard
expect forbid_kitlauncher_test 1 'kit launcher imports ui/tenfoot'

setup_tree
printf 'package main\n\nimport _ "github.com/DeanoC/FogCast/hostclient"\n' >"$root/cmd/fogcast-kit/doc.go"
printf 'package main\n\nimport _ "github.com/DeanoC/FogCast/ui/tenfoot"\n' >"$root/cmd/fogcast-kit/doc_test.go"
run_guard
expect forbid_cmd_test 1 'fogcast-kit imports ui/tenfoot'

setup_tree
printf 'package kitlauncher\n\nimport _ "github.com/DeanoC/FogCast/hostclient"\n' >"$root/ui/kitlauncher/doc.go"
printf 'package kitlauncher\n\nimport _ "github.com/DeanoC/FogCast/helper"\n' >"$root/ui/kitlauncher/doc_test.go"
write_pkg helper helper 'github.com/DeanoC/FogCast/ui/tenfoot'
run_guard
expect forbid_kitlauncher_test_helper 1 'kit launcher transitively depends on ui/tenfoot'

setup_tree
printf 'package main\n\nimport _ "github.com/DeanoC/FogCast/hostclient"\n' >"$root/cmd/fogcast-kit/doc.go"
printf 'package main\n\nimport _ "github.com/DeanoC/FogCast/helper"\n' >"$root/cmd/fogcast-kit/doc_test.go"
write_pkg helper helper 'github.com/DeanoC/FogCast/ui/tenfoot'
run_guard
expect forbid_cmd_test_helper 1 'fogcast-kit transitively depends on ui/tenfoot'

setup_tree
rm -rf "$root/ui/kitlauncher"
run_guard
expect missing_kitlauncher 1 'required directory is missing: ui/kitlauncher'

setup_tree
rm -rf "$root/cmd/fogcast-kit"
run_guard
expect missing_cmd 1 'required directory is missing: cmd/fogcast-kit'

setup_tree
mkdir -p "$tmp/rgbin"
printf '%s\n' '#!/bin/sh' 'exit 2' >"$tmp/rgbin/rg"
chmod +x "$tmp/rgbin/rg"
set +e
out=$(PATH="$tmp/rgbin:$PATH" FOGCAST_BOUNDARY_ROOT=$root sh "$guard" 2>&1)
status=$?
set -e
expect rg_tool_error 1 'rg failed with exit 2'

setup_tree
mkdir -p "$tmp/gobin"
printf '%s\n' '#!/bin/sh' 'echo go list failed >&2' 'exit 2' >"$tmp/gobin/go"
chmod +x "$tmp/gobin/go"
set +e
out=$(PATH="$tmp/gobin:$PATH" FOGCAST_BOUNDARY_ROOT=$root sh "$guard" 2>&1)
status=$?
set -e
expect go_list_error 1 'go list failed with exit 2'
