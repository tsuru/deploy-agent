#!/usr/bin/env bash
#
# local-dev.sh - Local development CLI tool used to run Deploy Agent.
#
# Version: 0.1.0

### Constants
readonly FAKE_HOST_IP="169.196.255.254"

# ------------------------------------------------------------------------------
# Pre-flight checks
#
# These functions are used to check if the system is ready to run the script.
# ------------------------------------------------------------------------------
preflight_check_deps() {
  local deps=("$@")

  for dep in "${deps[@]}"; do
    if ! command -v "${dep}" &>/dev/null; then
      echo "Failed to find dependency: ${dep}"
      exit 1
    fi
  done
}

preflight_checks() {
  # Check OS specific dependencies
  case "$(uname -s)" in
  Darwin)
    preflight_check_deps "ifconfig"
    ;;

  Linux)
    preflight_check_deps "ip"
    ;;

  *)
    echo "Unsupported OS: $(uname -s)"
    exit 1
    ;;
  esac
}

# ------------------------------------------------------------------------------
# Utility functions
#
# These functions are used to perform common tasks that are used by the command
# execution functions.
# ------------------------------------------------------------------------------
get_ifname() {
  case "$(uname -s)" in
  Darwin) echo lo0 ;;
  Linux) echo lo ;;
  esac
}

ifname_has_ip() {
  local interface_name=$1
  local ip=$2

  case "$(uname -s)" in
  Darwin)
    ifconfig "${interface_name}" | grep -q "${ip}"
    return $?
    ;;

  Linux)
    ip addr show "${interface_name}" | grep -q "${ip}"
    return $?
    ;;
  esac
}

ifname_add_ip() {
  local interface_name=${1}
  local ip=${2}

  case "$(uname -s)" in
  Darwin)
    sudo ifconfig "${interface_name}" alias "${ip}/32"
    ;;

  Linux)
    sudo ip addr add "${ip}/32" dev "${interface_name}"
    ;;
  esac
}

ifname_del_ip() {
  local interface_name=${1}
  local ip=${2}

  case "$(uname -s)" in
  Darwin)
    sudo ifconfig "${interface_name}" -alias "${ip}"
    ;;

  Linux)
    sudo ip addr del "${ip}/32" dev "${interface_name}"
    ;;
  esac
}

# ------------------------------------------------------------------------------
# Command execution functions
#
# These functions are the actual commands that are executed when the script is
# run with a specific command.
# ------------------------------------------------------------------------------
exec_setup_loopback() {
  local fake_host_ip=${1:-"$FAKE_HOST_IP"}
  local ifname=$(get_ifname)

  echo "Checking if the IP $fake_host_ip is assigned to the interface $ifname..."
  ifname_has_ip "$ifname" "$fake_host_ip"
  if [ $? -ne 0 ]; then
    echo "Assigning the IP $fake_host_ip to the interface $ifname..."
    ifname_add_ip "$ifname" "$fake_host_ip"
  fi
}

exec_cleanup_loopback() {
  local fake_host_ip=${1:-"$FAKE_HOST_IP"}
  local ifname=$(get_ifname)

  echo "Checking if the IP $fake_host_ip is assigned to the interface $ifname..."
  ifname_has_ip "$ifname" "$fake_host_ip"
  if [ $? -eq 0 ]; then
    echo "Removing the IP $fake_host_ip from the interface $ifname..."
    ifname_del_ip "$ifname" "$fake_host_ip"
  fi
}

exec_help() {
  echo "Usage: $(basename $0) [COMMAND|OPTIONS]"
  echo
  echo "COMMANDS:"
  echo "  setup-loopback       Setup the loopback interface with a fake IP"
  echo "  cleanup-loopback     Cleanup the loopback interface from the fake IP"
  echo "  render-templates     Render the configuration templates"
  echo
  echo "OPTIONS:"
  echo "  -h, --help      Print this help message"
  echo "  -v, --version   Print current version"
}

exec_version() {
  grep '^# Version: ' "$0" | cut -d ':' -f 2 | tr -d ' '
}

# ------------------------------------------------------------------------------
# Main execution
#
# This is the main execution of the script. It will parse the command line
# arguments and execute the appropriate command.
# ------------------------------------------------------------------------------
case "$1" in
# commands
setup-loopback) exec_setup_loopback "${@:2}" ;;
cleanup-loopback) exec_cleanup_loopback "${@:2}" ;;

# options
-h | --help) exec_help ;;
-v | --version) exec_version ;;

*)
  echo "Unknown command: $1"
  exec_help
  exit 1
  ;;
esac
