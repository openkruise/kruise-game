#!/usr/bin/env bash

# Outputs a semver-like identifier for the current git state.
# Prefer annotated tags (vX.Y.Z). Fall back to dev-<sha>[-dirty].

set -euo pipefail

git_describe() {
  git describe --tags --dirty --always 2>/dev/null || true
}

is_release_tag() {
  [[ -n "${1:-}" && "${1}" =~ ^v[0-9] ]]
}

fetch_tags() {
  local remote="$1"
  git config --get "remote.${remote}.url" >/dev/null 2>&1 || return 1
  git fetch "${remote}" --tags --force >/dev/null 2>&1 || return 1
}

describe_after_fetch() {
  local remote="$1" candidate
  fetch_tags "${remote}" || return 1
  candidate="$(git_describe)"
  if is_release_tag "${candidate}"; then
    printf '%s\n' "${candidate}"
    return 0
  fi
  return 1
}

remotes_in_priority() {
  local -a all=()
  mapfile -t all < <(git remote 2>/dev/null) || return 1
  if (( ${#all[@]} == 0 )); then
    return 1
  fi

  local -a ordered=()
  local remote

  add_remote_if_present() {
    local name="$1"
    local existing
    for existing in "${ordered[@]}"; do
      if [[ "${existing}" == "${name}" ]]; then
        return 0
      fi
    done
    for existing in "${all[@]}"; do
      if [[ "${existing}" == "${name}" ]]; then
        ordered+=("${existing}")
        return 0
      fi
    done
    return 1
  }

  add_remote_if_present origin || true
  add_remote_if_present upstream || true

  for remote in "${all[@]}"; do
    local seen=0
    for existing in "${ordered[@]}"; do
      if [[ "${existing}" == "${remote}" ]]; then
        seen=1
        break
      fi
    done
    if (( seen == 0 )); then
      ordered+=("${remote}")
    fi
  done

  printf '%s\n' "${ordered[@]}"
}

ensure_official_remote() {
  local desired="${UPSTREAM_REMOTE_URL:-https://github.com/openkruise/kruise-game.git}"
  if [[ -z "${desired}" ]]; then
    return 1
  fi

  local remote url
  while read -r remote; do
    url="$(git config --get "remote.${remote}.url" 2>/dev/null || true)"
    if [[ "${url}" == "${desired}" ]]; then
      printf '%s\n' "${remote}"
      return 0
    fi
  done < <(git remote 2>/dev/null)

  local base="official-upstream"
  local candidate="${base}"
  local idx=0
  while git config --get "remote.${candidate}.url" >/dev/null 2>&1; do
    url="$(git config --get "remote.${candidate}.url" 2>/dev/null || true)"
    if [[ "${url}" == "${desired}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
    idx=$((idx + 1))
    candidate="${base}-${idx}"
  done

  if git remote add "${candidate}" "${desired}" >/dev/null 2>&1; then
    printf '%s\n' "${candidate}"
    return 0
  fi

  return 1
}

compute_version() {
  local describe
  describe="$(git_describe)"
  if is_release_tag "${describe}"; then
    printf '%s\n' "${describe}"
    return 0
  fi

  if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    printf '%s\n' "${describe}"
    return 0
  fi

  local -a remotes=()
  if mapfile -t remotes < <(remotes_in_priority); then
    local remote
    for remote in "${remotes[@]}"; do
      if describe="$(describe_after_fetch "${remote}")"; then
        printf '%s\n' "${describe}"
        return 0
      fi
    done
  fi

  local official_remote
  if official_remote="$(ensure_official_remote 2>/dev/null || true)"; then
    if [[ -n "${official_remote}" ]]; then
      if describe="$(describe_after_fetch "${official_remote}")"; then
        printf '%s\n' "${describe}"
        return 0
      fi
    fi
  fi

  printf '%s\n' "${describe}"
}

result="$(compute_version)"

if [[ -z "${result}" ]]; then
  result="unknown"
fi

if is_release_tag "${result}"; then
  printf '%s\n' "${result}"
else
  printf 'dev-%s\n' "${result}"
fi
