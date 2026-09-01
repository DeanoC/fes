#!/bin/sh
set -eu

usage() {
  printf '%s\n' 'usage: validate-native-init-services.sh RUNTIME_SERVICE AGENT_SERVICE' >&2
  exit 2
}

normalize_start_block() {
  awk '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }

    /^[[:space:]]*start\)[[:space:]]*$/ {
      start_count++
      in_start=1
      next
    }

    in_start && /^[[:space:]]*;;[[:space:]]*$/ {
      if (pending != "") {
        malformed=1
      }
      end_count++
      in_start=0
      next
    }

    in_start {
      line=trim($0)
      if (line == "" || line ~ /^#/) {
        next
      }
      if (pending != "") {
        line=pending line
        pending=""
      }
      if (line ~ /\\[[:space:]]*$/) {
        sub(/\\[[:space:]]*$/, "", line)
        pending=trim(line) " "
        next
      }
      print trim(line)
    }

    END {
      if (start_count != 1 || end_count != 1 || in_start || pending != "" || malformed) {
        exit 1
      }
    }
  ' "$1"
}

validate_supervisor_assignment() {
  service=$1
  expected=$2
  awk -v expected="$expected" '
    {
      line=$0
      sub(/^[[:space:]]+/, "", line)
      sub(/[[:space:]]+$/, "", line)
      if (line == "" || line ~ /^#/) {
        next
      }
      if (line == expected) {
        exact_count++
      }
      if (line ~ /^supervisor_pid[[:space:]]*=/) {
        assignment_count++
      }
    }
    END {
      if (exact_count != 1 || assignment_count != 1) {
        exit 1
      }
    }
  ' "$service"
}

validate_start_commands() {
  service=$1
  expected_launch=$2
  expected_pid_write=$3
  label=$4

  if ! commands=$(normalize_start_block "$service"); then
    printf 'validate-native-init-services: %s has a malformed start block\n' "$label" >&2
    exit 1
  fi

  printf '%s\n' "$commands" | \
    EXPECTED_LAUNCH="$expected_launch" \
    EXPECTED_PID_WRITE="$expected_pid_write" \
    awk '
      BEGIN {
        expected_launch=ENVIRON["EXPECTED_LAUNCH"]
        expected_pid_write=ENVIRON["EXPECTED_PID_WRITE"]
      }
      {
        if ($0 == expected_launch) {
          launch_count++
          launch_line=NR
        }
        if ($0 == expected_pid_write) {
          pid_count++
          pid_line=NR
        }
        if ($0 ~ /&[[:space:]]*$/) {
          background_count++
        }
        if (index($0, "/usr/sbin/mister-supervise") != 0) {
          supervisor_count++
        }
        if (index($0, "/usr/sbin/mister-supervise") != 0 ||
            index($0, "/usr/sbin/mister-runtime") != 0 ||
            index($0, "/usr/sbin/mister-agent") != 0) {
          native_launch_line_count++
        }
        if (index($0, "$!") != 0 && index($0, ">") != 0) {
          pid_write_count++
        }
      }
      END {
        if (launch_count != 1 || pid_count != 1 ||
            background_count != 1 || supervisor_count != 1 ||
            native_launch_line_count != 1 ||
            pid_write_count != 1 || pid_line != launch_line + 1) {
          exit 1
        }
      }
    ' || {
      printf 'validate-native-init-services: %s has invalid active start commands\n' "$label" >&2
      exit 1
    }
}

[ "$#" -eq 2 ] || usage
runtime_service=$1
agent_service=$2
[ -f "$runtime_service" ] && [ -f "$agent_service" ] || {
  printf '%s\n' 'validate-native-init-services: service file is missing' >&2
  exit 1
}

validate_supervisor_assignment \
  "$runtime_service" \
  'supervisor_pid=/run/mister-runtime-supervisor.pid' || {
    printf '%s\n' 'validate-native-init-services: runtime supervisor PID assignment is invalid' >&2
    exit 1
  }
validate_start_commands \
  "$runtime_service" \
  '/usr/sbin/mister-supervise mister-runtime /usr/sbin/mister-runtime &' \
  'printf '\''%s\n'\'' "$!" > /run/mister-runtime-supervisor.pid' \
  runtime

validate_supervisor_assignment \
  "$agent_service" \
  'supervisor_pid=/run/mister-agent-supervisor.pid' || {
    printf '%s\n' 'validate-native-init-services: agent supervisor PID assignment is invalid' >&2
    exit 1
  }
validate_start_commands \
  "$agent_service" \
  '/usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent --config /media/fat/fogcast/agent.toml --runtime native &' \
  'printf '\''%s\n'\'' "$!" > /run/mister-agent-supervisor.pid' \
  agent
