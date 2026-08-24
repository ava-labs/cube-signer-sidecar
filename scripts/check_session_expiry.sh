#!/usr/bin/env bash

# Reports whether the CubeSigner session used by the e2e tests is still usable.
#
# The sidecar serves with the auth token and swaps it for a new one with the
# refresh token, so it exits at startup only once both have expired. When that
# happens the suite fails with a connection refused error against a process that
# is already gone, which says nothing about the actual cause: check the session
# up front and name it instead.

set -o errexit
set -o nounset
set -o pipefail

SESSION_FILE=${1:-e2e_session.json}

# How far ahead of the refresh token's expiry to start warning, so the secret
# can be rotated before a run fails.
WARN_WINDOW_SECONDS=${WARN_WINDOW_SECONDS:-$((7 * 24 * 60 * 60))}

if [[ ! -f "${SESSION_FILE}" ]]; then
    echo "::error::${SESSION_FILE} does not exist"
    exit 1
fi

auth_exp=$(jq -r '.session_info.auth_token_exp // empty' "${SESSION_FILE}")
refresh_exp=$(jq -r '.session_info.refresh_token_exp // empty' "${SESSION_FILE}")

if [[ ! ${auth_exp} =~ ^[0-9]+$ || ! ${refresh_exp} =~ ^[0-9]+$ ]]; then
    echo "::error::${SESSION_FILE} is not a CubeSigner session file: session_info.auth_token_exp and session_info.refresh_token_exp are missing or are not epoch seconds. The CUBIST_SIGNER_TOKEN secret must be the base64 of the JSON written by 'cs token create'."
    exit 1
fi

# GNU date on CI, BSD date when run locally.
fmt() {
    date -u -d "@$1" +'%Y-%m-%d %H:%M:%S UTC' 2>/dev/null || date -u -r "$1" +'%Y-%m-%d %H:%M:%S UTC'
}

now=$(date -u +%s)

echo "auth token expires:    $(fmt "${auth_exp}")"
echo "refresh token expires: $(fmt "${refresh_exp}")"

if ((auth_exp <= now && refresh_exp <= now)); then
    expired_on=$((auth_exp > refresh_exp ? auth_exp : refresh_exp))
    echo "::error::the CubeSigner session secret expired on $(fmt "${expired_on}") (auth token $(fmt "${auth_exp}"), refresh token $(fmt "${refresh_exp}")). Create a new session with 'cs token create', base64 it, and update the CUBIST_SIGNER_TOKEN secret."
    exit 1
fi

if ((refresh_exp <= now)); then
    echo "::warning::the CubeSigner session secret's refresh token expired on $(fmt "${refresh_exp}"). This run only works because the auth token is still valid; e2e will start failing after $(fmt "${auth_exp}"). Rotate the CUBIST_SIGNER_TOKEN secret."
    exit 0
fi

if ((refresh_exp - now <= WARN_WINDOW_SECONDS)); then
    echo "::warning::the CubeSigner session secret expires on $(fmt "${refresh_exp}"). Rotate the CUBIST_SIGNER_TOKEN secret before then."
fi
