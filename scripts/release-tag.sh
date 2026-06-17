#!/usr/bin/env bash
#
# release-tag.sh — sign and (optionally) push a semver release tag using
# the GPG release key whose fingerprint lives in this script.
#
# Wraps the rituals discovered the hard way in the v0.2.0 release:
#   - gpg-agent's pinentry-curses crashes on "Inappropriate ioctl for
#     device" in many terminal setups
#   - copy-pasting a base64 passphrase into pinentry corrupts on bracketed
#     paste / smart-quote substitution
#   - git tag -s doesn't accept a --passphrase flag; we have to wire it
#     through gpg.program
#
# This script bypasses all three by feeding gpg a passphrase via a temp
# wrapper that uses `gpg --batch --pinentry-mode loopback
# --passphrase-file <tempfile>`. The wrapper + file live in a mktemp dir
# with mode 600 and are wiped on any exit (including Ctrl-C).
#
# Passphrase sources, in priority order:
#   1. $RELEASE_KEY_PASSPHRASE_FILE   — explicit override, e.g.
#      "RELEASE_KEY_PASSPHRASE_FILE=$HOME/.config/release.pass make release-tag"
#   2. ~/.gnupg/release-key-bootstrap-v2/passphrase.txt — the path
#      docs/RELEASING.md writes during key generation. If you haven't
#      shredded it yet (or you keep it intentionally for `make release-tag`
#      convenience), the script picks it up automatically.
#   3. Interactive prompt via `read -rs` — no stty hackery, just bash's
#      builtin silent read. Used only as a last resort.
#
# Usage:
#   scripts/release-tag.sh <version>
#   make release-tag VERSION=v0.3.0
#
# The script refuses to run unless:
#   - you're on main
#   - working tree is clean
#   - local main == origin/main (so the tag points at what the world sees)
#   - the version tag doesn't already exist locally or on origin
#   - VERSION matches strict semver (vMAJOR.MINOR.PATCH[-PRERELEASE])

set -euo pipefail

# The release key. If this is ever rotated, bump here AND in SECURITY.md
# AND in the Terraform Registry's GPG keys list AND the GH Actions
# GPG_PRIVATE_KEY / PASSPHRASE secrets.
RELEASE_KEY=CFFA81EDF77A943B74FE42D50B99E1BA1894B507  # gitleaks:allow (GPG public-key fingerprint, also in SECURITY.md)

if [[ $# -ne 1 ]]; then
    cat >&2 <<EOF
usage: $0 <version>
       make release-tag VERSION=v0.3.0

VERSION must match strict semver: vMAJOR.MINOR.PATCH or
vMAJOR.MINOR.PATCH-PRERELEASE.
EOF
    exit 2
fi

VERSION="$1"

if ! [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.-]+)?$ ]]; then
    echo "ERROR: VERSION '$VERSION' must look like v0.3.0 or v0.3.0-rc1" >&2
    exit 2
fi

branch=$(git rev-parse --abbrev-ref HEAD)
if [[ "$branch" != "main" ]]; then
    echo "ERROR: not on main (currently on '$branch')." >&2
    echo "       git checkout main && git pull --ff-only" >&2
    exit 1
fi

if ! git diff-index --quiet HEAD --; then
    echo "ERROR: working tree has uncommitted changes." >&2
    echo "       commit or stash, then retry." >&2
    exit 1
fi

if git tag -l | grep -qx "$VERSION"; then
    echo "ERROR: tag '$VERSION' already exists locally." >&2
    echo "       delete first: git tag -d $VERSION" >&2
    exit 1
fi

git fetch --quiet origin
local_main=$(git rev-parse HEAD)
remote_main=$(git rev-parse origin/main)
if [[ "$local_main" != "$remote_main" ]]; then
    echo "ERROR: local main ($local_main) differs from origin/main ($remote_main)." >&2
    echo "       git pull --ff-only origin main" >&2
    exit 1
fi

if git ls-remote --tags origin "refs/tags/$VERSION" | grep -q "$VERSION"; then
    echo "ERROR: tag '$VERSION' already exists on origin." >&2
    echo "       delete on origin first: git push origin :refs/tags/$VERSION" >&2
    exit 1
fi

if ! gpg --list-secret-keys "$RELEASE_KEY" >/dev/null 2>&1; then
    echo "ERROR: release key $RELEASE_KEY not in gpg keyring." >&2
    echo "       see docs/RELEASING.md for the bootstrap procedure." >&2
    exit 1
fi

echo "==> Tagging $VERSION at $local_main"
echo "    with key $RELEASE_KEY"
echo

# Temp dir gets cleaned up on ANY exit path including Ctrl-C.
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

PASSFILE="$TMPDIR/passphrase"

# Resolve passphrase source. Priority: env var → bootstrap file → prompt.
DEFAULT_BOOTSTRAP="$HOME/.gnupg/release-key-bootstrap-v2/passphrase.txt"
SOURCE_FILE="${RELEASE_KEY_PASSPHRASE_FILE:-$DEFAULT_BOOTSTRAP}"

if [[ -r "$SOURCE_FILE" ]]; then
    echo "    passphrase: from $SOURCE_FILE"
    # Copy with a single read+write so we don't leak content via shell history
    # or process listings.
    cat "$SOURCE_FILE" > "$PASSFILE"
else
    echo "    passphrase: interactive prompt (no readable file at $SOURCE_FILE)"
    # bash builtin `read -rs` does the silent prompt without stty hackery.
    # The stty -echo + read pattern caused hangs under `make` with zsh as
    # the parent shell; -s is more portable.
    read -rsp 'GPG passphrase for release key: ' PASSPHRASE
    printf '\n'
    if [[ -z "$PASSPHRASE" ]]; then
        echo "ERROR: empty passphrase. Aborting." >&2
        exit 1
    fi
    printf '%s' "$PASSPHRASE" > "$PASSFILE"
    unset PASSPHRASE
fi
chmod 600 "$PASSFILE"
echo

WRAPPER="$TMPDIR/gpg-wrapper"
cat > "$WRAPPER" <<EOF
#!/usr/bin/env bash
exec gpg --batch --pinentry-mode loopback --passphrase-file "$PASSFILE" "\$@"
EOF
chmod 700 "$WRAPPER"

# Pre-flight: confirm the key + passphrase pair actually works BEFORE we
# get into git tag. A clear error here is friendlier than git tag failing
# with a vague "gpg failed to sign" later.
if ! echo preflight | "$WRAPPER" --local-user "$RELEASE_KEY" --clearsign \
        > /dev/null 2>"$TMPDIR/preflight.err"; then
    echo "ERROR: gpg pre-flight signature failed. Likely a wrong passphrase." >&2
    echo "       gpg stderr:" >&2
    sed 's/^/         /' "$TMPDIR/preflight.err" >&2
    exit 1
fi

# Tag, signed via the wrapper.
git -c gpg.program="$WRAPPER" tag -s -u "$RELEASE_KEY" "$VERSION" -m "$VERSION"

echo
echo "✓ Tag $VERSION signed locally."
git show "$VERSION" --no-patch --stat | head -8
echo

read -rp "Push $VERSION to origin and trigger the release workflow? [y/N] " confirm
case "$confirm" in
    y|Y|yes|YES)
        git push origin "$VERSION"
        echo
        echo "✓ Pushed. The release workflow is now running."
        echo "  Watch:  gh run watch"
        ;;
    *)
        echo
        echo "Tag created locally but NOT pushed. Push later with:"
        echo "  git push origin $VERSION"
        echo "Or undo with:"
        echo "  git tag -d $VERSION"
        ;;
esac
