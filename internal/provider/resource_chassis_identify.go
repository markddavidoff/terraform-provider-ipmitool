package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewChassisIdentifyResource() resource.Resource { return &chassisIdentifyResource{} }

type chassisIdentifyResource struct{ factory *ipmi.ClientFactory }

type chassisIdentifyModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	DurationSeconds types.Int64  `tfsdk:"duration_seconds"`
	Indefinite      types.Bool   `tfsdk:"indefinite"`
	OffOnDestroy    types.Bool   `tfsdk:"off_on_destroy"`
	ID              types.String `tfsdk:"id"`
}

func (r *chassisIdentifyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_chassis_identify"
}

func (r *chassisIdentifyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Blink the chassis identify LED. Operational convenience for spot-locating " +
			"a host in a rack.\n\n" +
			"This is an **imperative resource**: there's no IPMI \"get identify duration\" " +
			"command, so Read is a no-op and drift is not detected. Use Terraform's " +
			"`replace_triggered_by` if you want to re-trigger the LED on every apply.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"duration_seconds": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "How long the LED blinks (1..255). Ignored when `indefinite = true`. Default 15.",
			},
			"indefinite": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, the LED stays on until explicitly cleared (or `destroy` if `off_on_destroy` is set).",
			},
			"off_on_destroy": schema.BoolAttribute{
				Optional: true,
				Description: "If true (default), `terraform destroy` clears the LED. Set false to leave " +
					"the LED in whatever state the BMC last ran.",
			},
			"id":           schema.StringAttribute{Computed: true},
		},
	}
}

func (r *chassisIdentifyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *chassisIdentifyResource) overrideFromPlan(p chassisIdentifyModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *chassisIdentifyResource) idFor(override ipmi.ConnectionParams) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d/identify", merged.Host, port)
}

func (r *chassisIdentifyResource) writeFromPlan(ctx context.Context, plan *chassisIdentifyModel) error {
	client := r.factory.New(r.overrideFromPlan(*plan))
	duration := 15
	if !plan.DurationSeconds.IsNull() && !plan.DurationSeconds.IsUnknown() {
		duration = int(plan.DurationSeconds.ValueInt64())
	}
	indefinite := false
	if !plan.Indefinite.IsNull() && !plan.Indefinite.IsUnknown() {
		indefinite = plan.Indefinite.ValueBool()
	}
	if err := client.ChassisIdentify(ctx, duration, indefinite); err != nil {
		return err
	}
	plan.DurationSeconds = types.Int64Value(int64(duration))
	plan.Indefinite = types.BoolValue(indefinite)
	plan.ID = types.StringValue(r.idFor(r.overrideFromPlan(*plan)))
	return nil
}

func (r *chassisIdentifyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan chassisIdentifyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.writeFromPlan(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("failed to trigger chassis identify", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read is a no-op — IPMI has no standard "get identify duration" command,
// and our ChassisStatus parser doesn't decode the identify state byte
// either. Drift detection is out of scope for v0.1.
func (r *chassisIdentifyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state chassisIdentifyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *chassisIdentifyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan chassisIdentifyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.writeFromPlan(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("failed to retrigger chassis identify", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the LED if `off_on_destroy` is true (the default).
func (r *chassisIdentifyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state chassisIdentifyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Default behavior: clear the LED on destroy.
	off := true
	if !state.OffOnDestroy.IsNull() && !state.OffOnDestroy.IsUnknown() {
		off = state.OffOnDestroy.ValueBool()
	}
	if !off {
		return
	}
	client := r.factory.New(r.overrideFromPlan(state))
	if err := client.ChassisIdentify(ctx, 0, false); err != nil {
		resp.Diagnostics.AddError("failed to clear chassis identify on destroy", err.Error())
	}
}

func (r *chassisIdentifyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	host, port, err := parseHostPortID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("host"), host)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("port"), int64(port))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
