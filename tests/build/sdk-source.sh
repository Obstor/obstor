#!/bin/bash
# Shared pinned SDK source checkout

saved_sdk_ref() {
    local ref_file="$1/sdk-ref.txt" ref
    if [[ ! -f "$ref_file" ]]; then
        echo "missing SDK source ref: $ref_file" >&2
        return 1
    fi
    ref="$(cat "$ref_file")"
    if [[ ! "$ref" =~ ^[[:xdigit:]]{40}$ ]]; then
        echo "SDK source ref must be a full commit ID: $ref_file" >&2
        return 1
    fi
    printf '%s\n' "$ref"
}

# Local repos for offline builds
checkout_sdk() (
    set -euo pipefail
    local sdk="$1" destination="$2" ref="$3" repository
    case "$sdk" in
        obstor-js|obstor-py|obstor-java) ;;
        *) echo "unsupported SDK: $sdk" >&2; exit 1 ;;
    esac
    if [[ -z "$ref" || "$ref" == -* || "$ref" == *[[:space:]]* ]]; then
        echo "invalid source ref for $sdk" >&2
        exit 1
    fi
    if [[ -e "$destination" ]]; then
        echo "SDK destination already exists: $destination" >&2
        exit 1
    fi
    if [[ -n "${OBSTOR_SDK_SOURCE_ROOT:-}" ]]; then
        repository="${OBSTOR_SDK_SOURCE_ROOT}/$sdk"
        git -C "$repository" rev-parse --verify "${ref}^{commit}" >/dev/null
        mkdir -p "$destination"
        git -C "$repository" archive "$ref" | tar -xf - -C "$destination"
    else
        repository="https://github.com/obstor/$sdk.git"
        git init --quiet "$destination"
        git -C "$destination" remote add origin "$repository"
        git -C "$destination" fetch --quiet --depth=1 origin "$ref"
        git -C "$destination" checkout --quiet --detach FETCH_HEAD
    fi
)
