package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewWatchdogResource() resource.Resource { return &watchdogResource{} }

type watchdogResource struct{ factory *ipmi.ClientFactory }

type watchdogModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
	Action         types.String `tfsdk:"action"`
	Stopped        types.Bool   `tfsdk:"stopped"`
	LogEvent       types.Bool   `tfsdk:"log_event"`
	StartOnApply   types.Bool   `tfsdk:"start_on_apply"`
	Running        types.Bool   `tfsdk:"running"`
	ID             types.String `tfsdk:"id"`
}

func (r *watchdogResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_watchdog"
}

func (r *watchdogResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Configure the IPMI watchdog timer. The BMC will perform `action` if the timer " +
			"reaches zero without being reset.\n\n" +
			"The provider uses the SMS/OS timer-use slot. Set `start_on_apply = true` to issue " +
			"`Reset Watchdog Timer` after each Create/Update so the new countdown starts immediately.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"timeout_seconds": schema.Int64Attribute{
				Required:    true,
				Description: "Total countdown in seconds (0..6553). 0 disables.",
			},
			"action": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "What the BMC does on expiry: `none`, `hard_reset` (default), `power_down`, `power_cycle`.",
				Validators:  []validator.String{oneOf("none", "hard_reset", "power_down", "power_cycle")},
			},
			"stopped": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, configure the timer but leave it stopped. Default false.",
			},
			"log_event": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, log expirations to the SEL. Default true.",
			},
			"start_on_apply": schema.BoolAttribute{
				Optional: true,
				Description: "If true, send `Reset Watchdog Timer` after Create/Update so the new " +
					"countdown starts immediately. Default false (the OS or BIOS is expected to drive resets).",
			},

			"running": schema.BoolAttribute{
				Computed:    true,
				Description: "True when the BMC reports a non-zero present countdown and the timer isn't stopped.",
			},
			"id":           schema.StringAttribute{Computed: true},
		},
	}
}

func (r *watchdogResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *watchdogResource) overrideFromPlan(p watchdogModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *watchdogResource) idFor(override ipmi.ConnectionParams) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d/watchdog", merged.Host, port)
}

func (r *watchdogResource) configFromPlan(p watchdogModel) ipmi.WatchdogConfig {
	cfg := ipmi.WatchdogConfig{
		TimeoutSeconds: int(p.TimeoutSeconds.ValueInt64()),
		Action:         ipmi.WatchdogActionHardReset,
		Stopped:        false,
		LogEvent:       true,
	}
	if !p.Action.IsNull() && !p.Action.IsUnknown() {
		cfg.Action = ipmi.WatchdogAction(p.Action.ValueString())
	}
	if !p.Stopped.IsNull() && !p.Stopped.IsUnknown() {
		cfg.Stopped = p.Stopped.ValueBool()
	}
	if !p.LogEvent.IsNull() && !p.LogEvent.IsUnknown() {
		cfg.LogEvent = p.LogEvent.ValueBool()
	}
	return cfg
}

func (r *watchdogResource) writeFromPlan(ctx context.Context, plan *watchdogModel) error {
	client := r.factory.New(r.overrideFromPlan(*plan))
	cfg := r.configFromPlan(*plan)
	if err := client.SetWatchdog(ctx, cfg); err != nil {
		return fmt.Errorf("set watchdog: %w", err)
	}
	if !plan.StartOnApply.IsNull() && plan.StartOnApply.ValueBool() {
		if err := client.ResetWatchdog(ctx); err != nil {
			return fmt.Errorf("reset watchdog: %w", err)
		}
	}
	// Read back to populate Computed fields.
	current, err := client.GetWatchdog(ctx)
	if err != nil {
		return fmt.Errorf("read-after-write: %w", err)
	}
	plan.Action = types.StringValue(string(current.Action))
	plan.Stopped = types.BoolValue(current.Stopped)
	plan.LogEvent = types.BoolValue(current.LogEvent)
	plan.Running = types.BoolValue(current.Running)
	plan.ID = types.StringValue(r.idFor(r.overrideFromPlan(*plan)))
	return nil
}

func (r *watchdogResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan watchdogModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.writeFromPlan(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("failed to configure watchdog", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *watchdogResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state watchdogModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.factory.New(r.overrideFromPlan(state))
	current, err := client.GetWatchdog(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to read watchdog", err.Error())
		return
	}
	state.Action = types.StringValue(string(current.Action))
	state.Stopped = types.BoolValue(current.Stopped)
	state.LogEvent = types.BoolValue(current.LogEvent)
	state.TimeoutSeconds = types.Int64Value(int64(current.TimeoutSeconds))
	state.Running = types.BoolValue(current.Running)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *watchdogResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan watchdogModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.writeFromPlan(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("failed to update watchdog", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete stops the watchdog by writing a Stopped + zero-action config.
func (r *watchdogResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state watchdogModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.factory.New(r.overrideFromPlan(state))
	if err := client.SetWatchdog(ctx, ipmi.WatchdogConfig{
		Stopped: true, Action: ipmi.WatchdogActionNone, TimeoutSeconds: 0,
	}); err != nil {
		resp.Diagnostics.AddError("failed to stop watchdog on destroy", err.Error())
	}
}

func (r *watchdogResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	host, port, err := parseHostPortID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("host"), host)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("port"), int64(port))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
