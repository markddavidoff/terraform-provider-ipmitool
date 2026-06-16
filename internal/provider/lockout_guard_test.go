package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestEnforceLockoutGuards(t *testing.T) {
	t.Parallel()

	check := lockoutCheck{
		Triggered: true,
		Summary:   "you are about to lock yourself out",
		Detail:    "this is bad",
	}

	t.Run("not triggered → no diagnostics", func(t *testing.T) {
		d := enforceLockoutGuards(types.BoolValue(false), []lockoutCheck{{Triggered: false}})
		if len(d) != 0 {
			t.Errorf("got %d diags, want 0", len(d))
		}
	})

	t.Run("triggered + not forced → error", func(t *testing.T) {
		d := enforceLockoutGuards(types.BoolNull(), []lockoutCheck{check})
		if !d.HasError() {
			t.Errorf("expected an error diagnostic, got %v", d)
		}
	})

	t.Run("triggered + forced → warning", func(t *testing.T) {
		d := enforceLockoutGuards(types.BoolValue(true), []lockoutCheck{check})
		if d.HasError() {
			t.Errorf("forced should be warning-only, got %v", d)
		}
		if d.WarningsCount() != 1 {
			t.Errorf("expected 1 warning, got %d", d.WarningsCount())
		}
	})

	t.Run("multiple distinct triggered + not forced → multiple errors", func(t *testing.T) {
		d := enforceLockoutGuards(types.BoolValue(false), []lockoutCheck{
			{Triggered: true, Summary: "first risk", Detail: "x"},
			{Triggered: true, Summary: "second risk", Detail: "y"},
			{Triggered: false, Summary: "not triggered", Detail: "z"},
		})
		if d.ErrorsCount() != 2 {
			t.Errorf("expected 2 errors, got %d (%v)", d.ErrorsCount(), d)
		}
	})
}
