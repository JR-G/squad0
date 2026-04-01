#!/usr/bin/env bash
# Prevents --no-verify from being used. Install as a wrapper.
# This script is sourced by the shell profile to intercept git commands.

_original_git=$(which git)

git() {
  for arg in "$@"; do
    if [[ "$arg" == "--no-verify" || "$arg" == "--no-gpg-sign" ]]; then
      echo "BLOCKED: --no-verify and --no-gpg-sign are not allowed in this repo."
      echo "Fix the underlying issue instead of bypassing hooks."
      return 1
    fi
  done
  "$_original_git" "$@"
}
