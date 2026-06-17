#!/usr/bin/env bash
#
# detect-cipher.sh — probe an IPMI BMC for the highest-supported RMCP+
# cipher suite. Use the value it prints in your provider block:
#
#     provider "ipmi" {
#       host         = "192.0.2.10"
#       username     = "root"
#       password     = var.bmc_password
#       cipher_suite = <NUMBER PRINTED BY THIS SCRIPT>
#     }
#
# usage: scripts/detect-cipher.sh <host> <user> <password|->
#   if password is `-`, the script prompts for it (echo suppressed).
#
# ╭─────────────────────────────  WARNING  ─────────────────────────────╮
# │ Each failed cipher probe is one failed authentication attempt       │
# │ against the BMC. Most BMCs lock the user account after 3–5 failed   │
# │ attempts:                                                           │
# │   - iDRAC default: 3 failures → 10 minute lockout                   │
# │   - SuperMicro:    5 failures → 5 minute lockout                    │
# │                                                                     │
# │ Mitigation:                                                         │
# │   - Use a known-good username/password (the credentials must be     │
# │     valid for every probe; the failure is on the cipher, not auth). │
# │   - Probe ONE HOST AT A TIME. If onboarding a fleet, consider       │
# │     temporarily raising the lockout threshold via ipmitool user     │
# │     set or RACADM (Dell).                                           │
# │   - If a probe locks you out, wait for the lockout window to clear  │
# │     before re-running.                                              │
# ╰─────────────────────────────────────────────────────────────────────╯

set -euo pipefail

if [[ $# -lt 3 ]]; then
    echo "usage: $0 <host> <user> <password|->" >&2
    echo "  password '-' reads from stdin (echo suppressed)" >&2
    exit 2
fi

host="$1"
user="$2"
pass="$3"
if [[ "$pass" == "-" ]]; then
    read -rsp "Password for $user@$host: " pass
    echo
fi

# Probe order: highest-strength to lowest. Stop at the first one that
# successfully runs `mc info`.
ciphers=(17 12 8 3 0)

echo "==> Probing $host as $user"
echo "    (see header warning about BMC account lockout)"
echo

for c in "${ciphers[@]}"; do
    printf "  cipher %2d: " "$c"
    if IPMI_PASSWORD="$pass" ipmitool -I lanplus -C "$c" -H "$host" -U "$user" -E mc info >/dev/null 2>&1; then
        echo "OK  ✓ highest supported"
        echo
        echo "Add to your provider block:"
        echo "  cipher_suite = $c"
        if [[ "$c" -eq 0 ]]; then
            echo "  allow_unauthenticated = true   # cipher 0 = no auth, no integrity"
        fi
        exit 0
    else
        echo "FAIL"
    fi
done

echo
echo "ERROR: no supported cipher suite responded." >&2
echo "Check host reachability (ping $host), credentials, and that the BMC" >&2
echo "actually speaks RMCP+ (some old BMCs only do RMCP). If you saw" >&2
echo "auth errors during the probe, your account may now be locked out;" >&2
echo "wait for the BMC's lockout window to clear before retrying." >&2
exit 1
