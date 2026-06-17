package provider

import (
	"context"
	"testing"
)

func TestEnforceLockoutGuards(t *testing.T) {
	check := lockoutCheck{
		Triggered: true,
		Summary:   "you are about to lock yourself out",
		Detail:    "this is bad",
	}

	t.Run("not triggered → no diagnostics", func(t *testing.T) {
		t.Setenv("TF_IPMI_ALLOW_LOCKOUT", "")
		d := enforceLockoutGuards(context.Background(), "ipmi_user", "h", []lockoutCheck{{Triggered: false}})
		if len(d) != 0 {
			t.Errorf("got %d diags, want 0", len(d))
		}
	})

	t.Run("triggered + env unset → error", func(t *testing.T) {
		t.Setenv("TF_IPMI_ALLOW_LOCKOUT", "")
		d := enforceLockoutGuards(context.Background(), "ipmi_user", "h", []lockoutCheck{check})
		if !d.HasError() {
			t.Errorf("expected an error diagnostic, got %v", d)
		}
	})

	t.Run("triggered + env=1 → warning", func(t *testing.T) {
		t.Setenv("TF_IPMI_ALLOW_LOCKOUT", "1")
		d := enforceLockoutGuards(context.Background(), "ipmi_user", "h", []lockoutCheck{check})
		if d.HasError() {
			t.Errorf("env-bypass should be warning-only, got %v", d)
		}
		if d.WarningsCount() != 1 {
			t.Errorf("expected 1 warning, got %d", d.WarningsCount())
		}
	})

	t.Run("triggered + env=0 → error (only \"1\" bypasses)", func(t *testing.T) {
		t.Setenv("TF_IPMI_ALLOW_LOCKOUT", "0")
		d := enforceLockoutGuards(context.Background(), "ipmi_user", "h", []lockoutCheck{check})
		if !d.HasError() {
			t.Errorf("TF_IPMI_ALLOW_LOCKOUT=0 should NOT bypass, got %v", d)
		}
	})

	t.Run("multiple distinct triggered + env unset → multiple errors", func(t *testing.T) {
		t.Setenv("TF_IPMI_ALLOW_LOCKOUT", "")
		d := enforceLockoutGuards(context.Background(), "ipmi_user", "h", []lockoutCheck{
			{Triggered: true, Summary: "first risk", Detail: "x"},
			{Triggered: true, Summary: "second risk", Detail: "y"},
			{Triggered: false, Summary: "not triggered", Detail: "z"},
		})
		if d.ErrorsCount() != 2 {
			t.Errorf("expected 2 errors, got %d (%v)", d.ErrorsCount(), d)
		}
	})
}
