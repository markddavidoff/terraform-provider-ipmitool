#!/usr/bin/env bash
# Set or update a single key in a sops-encrypted dotenv file without
# touching the others. Prompts for the value (hidden input) — never
# pass secrets on the command line.
#
# Usage:
#   scripts/secrets-set-one.sh <KEY> [<sops-encrypted-dotenv>]
#
# Default file: secrets/idrac.enc.env

set -euo pipefail

KEY="${1:-}"
FILE="${2:-secrets/idrac.enc.env}"

err() { printf "\033[31m✗ %s\033[0m\n" "$*" >&2; }
ok()  { printf "\033[32m✓ %s\033[0m\n" "$*" >&2; }

[ -n "$KEY" ] || { err "Usage: $0 <KEY> [<file>]"; exit 1; }
[[ "$KEY" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || { err "Invalid env var name: $KEY"; exit 1; }

# If file doesn't exist, start from the example and encrypt fresh.
if [ ! -f "$FILE" ]; then
  if [ ! -f secrets/idrac.env.example ]; then
    err "No $FILE and no example to bootstrap from"; exit 1
  fi
  ok "No $FILE yet — bootstrapping from secrets/idrac.env.example"
  BOOT_PLAIN="${FILE%.enc.env}.env"
  # Clean partial bootstrap artifacts on any failure.
  trap 'rm -f "$BOOT_PLAIN" "$FILE"' ERR
  cp secrets/idrac.env.example "$BOOT_PLAIN"
  sops --encrypt --input-type dotenv --output-type dotenv \
       "$BOOT_PLAIN" > "$FILE"
  rm -f "$BOOT_PLAIN"
  trap - ERR
fi

# Warn if KEY isn't one of the IPMI_* names the POC consumes.
case "$KEY" in
  IPMI_HOST|IPMI_USER|IPMI_PASS|IPMI_PORT|IPMI_CIPHER) ;;
  *) printf "\033[33m⚠ %s is not one of IPMI_HOST/USER/PASS/PORT/CIPHER — proceeding anyway.\033[0m\n" "$KEY" >&2 ;;
esac

printf "Value for %s (input hidden): " "$KEY" >&2
IFS= read -rs VALUE
printf "\n" >&2
[ -n "$VALUE" ] || { err "Empty value; aborting"; exit 1; }

# Decrypt to a tmp file, replace/append KEY=VALUE, re-encrypt, swap atomically.
# Tempfiles live under secrets/ so the sops creation_rules regex matches them.
# secrets/.*.tmp.* is gitignored.
TMP_PLAIN="secrets/.$$.plain.env"
TMP_ENC="secrets/.$$.enc.env"
trap 'shred -u "$TMP_PLAIN" 2>/dev/null || rm -f "$TMP_PLAIN"; rm -f "$TMP_ENC"' EXIT

sops --decrypt "$FILE" > "$TMP_PLAIN"

if grep -qE "^${KEY}=" "$TMP_PLAIN"; then
  # Replace existing line.
  awk -v k="$KEY" -v v="$VALUE" 'BEGIN{FS=OFS="="} $1==k{print k"="v; next} {print}' \
      "$TMP_PLAIN" > "$TMP_PLAIN.new"
  mv "$TMP_PLAIN.new" "$TMP_PLAIN"
  ACTION="updated"
else
  printf "%s=%s\n" "$KEY" "$VALUE" >> "$TMP_PLAIN"
  ACTION="added"
fi

sops --encrypt --input-type dotenv --output-type dotenv "$TMP_PLAIN" > "$TMP_ENC"
mv "$TMP_ENC" "$FILE"

ok "$ACTION $KEY in $FILE"
