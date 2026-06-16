package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func TestCurrentStateString(t *testing.T) {
	t.Parallel()
	if got := currentStateString(true); got != "on" {
		t.Errorf("currentStateString(true) = %q; want \"on\"", got)
	}
	if got := currentStateString(false); got != "off" {
		t.Errorf("currentStateString(false) = %q; want \"off\"", got)
	}
}

func TestPowerResource_idFor(t *testing.T) {
	t.Parallel()
	r := &powerResource{
		factory: &ipmi.ClientFactory{
			Defaults: ipmi.ConnectionParams{Host: "default-host", Port: 623},
		},
	}

	t.Run("override host wins", func(t *testing.T) {
		got := r.idFor(ipmi.ConnectionParams{Host: "override-host"})
		if got != "override-host:623" {
			t.Errorf("got %q, want override-host:623", got)
		}
	})

	t.Run("default host when no override", func(t *testing.T) {
		got := r.idFor(ipmi.ConnectionParams{})
		if got != "default-host:623" {
			t.Errorf("got %q, want default-host:623", got)
		}
	})

	t.Run("override port wins", func(t *testing.T) {
		got := r.idFor(ipmi.ConnectionParams{Host: "h", Port: 6230})
		if got != "h:6230" {
			t.Errorf("got %q, want h:6230", got)
		}
	})

	t.Run("zero port falls back to 623", func(t *testing.T) {
		// Factory defaults already have 623 so this just exercises the
		// guard in idFor.
		empty := &powerResource{factory: &ipmi.ClientFactory{}}
		got := empty.idFor(ipmi.ConnectionParams{Host: "h"})
		if got != "h:623" {
			t.Errorf("got %q, want h:623", got)
		}
	})
}

func TestOneOfValidator(t *testing.T) {
	t.Parallel()
	v := oneOf("on", "off")

	cases := []struct {
		name     string
		value    types.String
		wantErrs int
	}{
		{"valid on", types.StringValue("on"), 0},
		{"valid off", types.StringValue("off"), 0},
		{"invalid value", types.StringValue("cycle"), 1},
		{"null is allowed (validators don't run on null)", types.StringNull(), 0},
		{"unknown is allowed", types.StringUnknown(), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.StringRequest{
				Path:        path.Root("state"),
				ConfigValue: tc.value,
			}
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), req, resp)
			if got := len(resp.Diagnostics); got != tc.wantErrs {
				t.Errorf("got %d diagnostics, want %d (%v)", got, tc.wantErrs, resp.Diagnostics)
			}
		})
	}
}
