#!/usr/bin/env bash

# Run one command while retaining exactly what was printed and the command's
# shell-escaped invocation.  In particular, do not use the tempting
# ``command | tee log`` form without reading PIPESTATUS: that would report the
# exit status of tee instead of the build command.
set -u
set -o pipefail

usage() {
    printf 'usage: %s LOG COMMAND [ARG ...]\n' "${0##*/}" >&2
}

if (( $# < 2 )); then
    usage
    exit 2
fi

log=$1
shift

# Create the log's parent without evaluating any part of the command as shell
# syntax.  ``dirname --`` also handles log names beginning with a dash.
log_dir=$(dirname -- "$log")
if ! mkdir -p -- "$log_dir"; then
    printf 'failed command logging: cannot create log directory: %s\n' "$log_dir" >&2
    exit 2
fi

print_command() {
    printf 'command:'
    printf ' %q' "$@"
    printf '\n'
}

# The command and tee are deliberately one pipeline.  stderr is merged before
# tee so stdout and stderr are both retained in their original write stream.
{
    print_command "$@"
    "$@"
} 2>&1 | tee -- "$log"

# PIPESTATUS must be read immediately after the pipeline.  The first element
# is the actual command block; the second is tee.  A failed tee is still a
# failed run even when the command succeeded, but a real command failure is
# the primary status when both sides fail.
pipeline_status=("${PIPESTATUS[@]}")
command_status=${pipeline_status[0]}
tee_status=${pipeline_status[1]}

if (( command_status != 0 )); then
    status=$command_status
    failure_label="failed command (exit $command_status)"
elif (( tee_status != 0 )); then
    status=$tee_status
    failure_label="failed command log (tee exit $tee_status)"
else
    status=0
    failure_label=""
fi

if (( status != 0 )); then
    printf '%s:' "$failure_label" >&2
    printf ' %q' "$@" >&2
    # Escape the path as well so this summary remains one physical line even
    # when a caller supplies an unusual log name containing whitespace/newlines.
    printf '; log: %q\n' "$log" >&2
fi

exit "$status"
