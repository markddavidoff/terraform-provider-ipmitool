package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

// NewPowerResource is the factory the provider registers.
func NewPowerResource() resource.Resource { return &powerResource{} }

type powerResource struct {
	factory *ipmi.ClientFactory
}

type powerModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	State              types.String `tfsdk:"state"`
	PowerOffOnDestroy  types.Bool   `tfsdk:"power_off_on_destroy"`
	CurrentState       types.String `tfsdk:"current_state"`
	LastUpdated        types.String `tfsdk:"last_updated"`
	ID                 types.String `tfsdk:"id"`
}

func (r *powerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_power"
}

func (r *powerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Declaratively manage BMC chassis power state. Reconciles drift on each " +
			"refresh — if the host gets powered off out-of-band, the next apply turns it back on.\n\n" +
			"Only steady states (`on`, `off`) are supported. Imperative actions like `cycle` " +
			"and `reset` will be a separate `ipmi_power_action` resource in v0.2.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"state": schema.StringAttribute{
				Required:    true,
				Description: "Desired power state: `on` or `off`.",
				Validators:  []validator.String{oneOf("on", "off")},
			},
			"power_off_on_destroy": schema.BoolAttribute{
				Optional: true,
				Description: "If true, `terraform destroy` powers the host off. " +
					"Default false (destroy leaves the host running).",
			},
			"current_state": schema.StringAttribute{
				Computed:    true,
				Description: "Observed chassis power state at last read.",
			},
			"last_updated": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp of the last SetPowerState call.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Resource ID — `<host>:<port>` of the managed BMC.",
			},
		},
	}
}

func (r *powerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	factory, ok := req.ProviderData.(*ipmi.ClientFactory)
	if !ok {
		resp.Diagnostics.AddError("unexpected provider data type",
			fmt.Sprintf("expected *ipmi.ClientFactory, got %T (bug)", req.ProviderData))
		return
	}
	r.factory = factory
}

func (r *powerResource) overrideFromPlan(p powerModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *powerResource) idFor(override ipmi.ConnectionParams) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d", merged.Host, port)
}

func (r *powerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan powerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)

	desired := ipmi.PowerState(plan.State.ValueString())
	if err := client.SetPowerState(ctx, desired); err != nil {
		resp.Diagnostics.AddError("failed to set power state", err.Error())
		return
	}

	status, err := client.GetChassisStatus(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to refresh chassis status after Create", err.Error())
		return
	}

	plan.CurrentState = types.StringValue(currentStateString(status.PowerOn))
	plan.LastUpdated = types.StringValue(time.Now().UTC().Format(time.RFC3339))
	plan.ID = types.StringValue(r.idFor(override))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *powerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state powerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(state)
	client := r.factory.New(override)

	status, err := client.GetChassisStatus(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to refresh chassis status", err.Error())
		return
	}
	state.CurrentState = types.StringValue(currentStateString(status.PowerOn))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *powerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan powerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)

	desired := ipmi.PowerState(plan.State.ValueString())
	if err := client.SetPowerState(ctx, desired); err != nil {
		resp.Diagnostics.AddError("failed to set power state", err.Error())
		return
	}

	status, err := client.GetChassisStatus(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to refresh chassis status after Update", err.Error())
		return
	}
	plan.CurrentState = types.StringValue(currentStateString(status.PowerOn))
	plan.LastUpdated = types.StringValue(time.Now().UTC().Format(time.RFC3339))
	plan.ID = types.StringValue(r.idFor(override))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *powerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state powerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !state.PowerOffOnDestroy.ValueBool() {
		return // safest default — leave the host running
	}
	client := r.factory.New(r.overrideFromPlan(state))
	if err := client.SetPowerState(ctx, ipmi.PowerOff); err != nil {
		resp.Diagnostics.AddError("failed to power off on destroy", err.Error())
		return
	}
}

func currentStateString(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// ───────────── tiny inline validator ─────────────

// oneOfValidator validates that a string is one of the allowed values.
// Inline (vs. importing terraform-plugin-framework-validators) to avoid
// adding a dep for a 10-line helper.
type oneOfValidator struct{ allowed []string }

func oneOf(values ...string) validator.String { return oneOfValidator{allowed: values} }

func (v oneOfValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be one of %v", v.allowed)
}
func (v oneOfValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (v oneOfValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, a := range v.allowed {
		if got == a {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "invalid value",
		fmt.Sprintf("got %q; want one of %v", got, v.allowed))
}
