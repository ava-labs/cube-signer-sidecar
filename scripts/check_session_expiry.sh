#!/usr/bin/env bash

# Reports whether the CubeSigner session used by the e2e tests is still usable.
#
# The sidecar serves with the auth token and swaps it for a new one with the
# refresh token, so it exits at startup only once both have expired. When that
# happens the suite fails with a connection refused error against a process that
# is already gone, which says nothing about the actual cause: check the session
# up front and name it instead.
#
# The sidecar is the authority on what it can read, so anything this script
# can't parse is reported as a warning and left to the suite itself. The only
# hard failure is a session that has provably expired.

set -o errexit
set -o nounset
set -o pipefail

SESSION_FILE=${1:-e2e_session.json}

# How far ahead of the refresh token's expiry to start warning, so the secret
# can be rotated before a run fails.
WARN_WINDOW_SECONDS=${WARN_WINDOW_SECONDS:-$((7 * 24 * 60 * 60))}

if [[ ! -f "${SESSION_FILE}" ]]; then
    echo "::warning::${SESSION_FILE} does not exist; skipping the session expiry check"
    exit 0
fi

if ! command -v python3 >/dev/null; then
    echo "::warning::python3 is not available; skipping the session expiry check"
    exit 0
fi

# Read the expiries the way the sidecar does: encoding/json decodes the first
# value in the file and ignores whatever follows, so a stricter parser here
# would reject sessions that work fine.
if ! expiries=$(python3 - "${SESSION_FILE}" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "rb") as handle:
    text = handle.read().decode("utf-8", "replace").lstrip()

try:
    session, end = json.JSONDecoder().raw_decode(text)
    info = session["session_info"]
    expiries = int(info["auth_token_exp"]), int(info["refresh_token_exp"])
except Exception as err:
    # Position and type only: the session file holds live credentials.
    print(f"{type(err).__name__}: {err}", file=sys.stderr)
    raise SystemExit(1)

print(*expiries)

trailing = len(text) - end
if trailing:
    print(
        f"::warning::{path} has {trailing} byte(s) after the session JSON. "
        "The sidecar ignores them, but the CUBIST_SIGNER_TOKEN secret should be "
        "the base64 of exactly the JSON written by 'cs token create'.",
        file=sys.stderr,
    )
PY
); then
    echo "::warning::could not read the session expiries from ${SESSION_FILE}; skipping the check. The CUBIST_SIGNER_TOKEN secret should be the base64 of the JSON written by 'cs token create'."
    exit 0
fi

read -r auth_exp refresh_exp <<<"${expiries}"

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
