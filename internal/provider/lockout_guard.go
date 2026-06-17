package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// lockoutCheck describes a single self-destructive condition.
//
// Triggered should be set by the resource's ModifyPlan after comparing
// plan vs prior state vs the active connection settings (host / user /
// channel). Summary is a short title, Detail explains the risk and the
// remedy. Both are surfaced verbatim to the user.
type lockoutCheck struct {
	Triggered bool
	Summary   string
	Detail    string
}

// lockoutBypassEnv is the operational env var that lets a plan with a
// triggered lockout guard proceed. Operationally scoped to one apply
// — when the runner shell exits, the bypass is gone.
const lockoutBypassEnv = "TF_IPMI_ALLOW_LOCKOUT"

// allowLockoutBypassed reports whether the bypass env var is set to "1".
func allowLockoutBypassed() bool {
	return os.Getenv(lockoutBypassEnv) == "1"
}

// enforceLockoutGuards turns triggered checks into diagnostics:
//
//   - TF_IPMI_ALLOW_LOCKOUT=1 in the env → warning (the apply proceeds)
//   - otherwise                         → error (the apply is blocked)
//
// Every bypass emits BOTH a Diagnostics warning (visible in plan output)
// AND a tflog.Warn structured event so the SIEM trail records who was
// running, on what host, and why the guard was bypassed.
//
// resourceType / host are used for the structured event; pass the
// resource's TypeName and the merged connection host.
func enforceLockoutGuards(
	ctx context.Context,
	resourceType, host string,
	checks []lockoutCheck,
) diag.Diagnostics {
	var diags diag.Diagnostics
	bypassed := allowLockoutBypassed()
	for _, c := range checks {
		if !c.Triggered {
			continue
		}
		if bypassed {
			diags.AddWarning(
				"Lockout guard bypassed (TF_IPMI_ALLOW_LOCKOUT=1): "+c.Summary,
				c.Detail+"\n\nProceeding because TF_IPMI_ALLOW_LOCKOUT=1 is set "+
					"in the runner environment. This is your only warning — "+
					"the next apply may fail if you've actually locked yourself "+
					"out of this BMC.",
			)
			tflog.Warn(ctx, "lockout guard bypassed", map[string]any{
				"resource_type": resourceType,
				"host":          host,
				"reason":        c.Summary,
				"bypass_via":    lockoutBypassEnv,
			})
			continue
		}
		diags.AddError(
			c.Summary,
			c.Detail+"\n\nTo proceed anyway, set "+lockoutBypassEnv+"=1 "+
				"in the runner environment for this apply only. The env-var "+
				"opt-in is operational (per-apply) rather than declarative "+
				"(in .tf) so it can't be accidentally left set across runs.",
		)
	}
	return diags
}
