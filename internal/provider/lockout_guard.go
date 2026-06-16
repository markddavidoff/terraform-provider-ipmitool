package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// enforceLockoutGuards turns triggered checks into diagnostics:
//
//   - force_lockout_risk = true   → warning (the apply proceeds)
//   - force_lockout_risk = false  → attribute error on force_lockout_risk
//
// The diagnostic always points at the force_lockout_risk attribute so the
// user knows exactly how to opt in.
func enforceLockoutGuards(force types.Bool, checks []lockoutCheck) diag.Diagnostics {
	var diags diag.Diagnostics
	forced := force.ValueBool()
	for _, c := range checks {
		if !c.Triggered {
			continue
		}
		if forced {
			diags.AddWarning(c.Summary,
				c.Detail+"\n\nProceeding because force_lockout_risk = true.")
			continue
		}
		diags.AddAttributeError(
			path.Root("force_lockout_risk"),
			c.Summary,
			c.Detail+"\n\nTo proceed anyway, set force_lockout_risk = true.",
		)
	}
	return diags
}
