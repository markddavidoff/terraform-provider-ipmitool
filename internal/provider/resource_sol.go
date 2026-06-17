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

func NewSOLResource() resource.Resource { return &solResource{} }

type solResource struct{ factory *ipmi.ClientFactory }

type solModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	Channel             types.Int64  `tfsdk:"channel"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	Bitrate             types.String `tfsdk:"bitrate"`
	PrivilegeLimit      types.String `tfsdk:"privilege_limit"`
	ForceAuthentication types.Bool   `tfsdk:"force_authentication"`
	ForceEncryption     types.Bool   `tfsdk:"force_encryption"`
	LastUpdated         types.String `tfsdk:"last_updated"`
	ID                  types.String `tfsdk:"id"`
}

func (r *solResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sol"
}

func (r *solResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Serial-over-LAN configuration on one channel. Each set field is " +
			"written; omitted fields are left alone. Bitrate is applied to both volatile " +
			"and non-volatile settings since users typically want them in sync.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"channel": schema.Int64Attribute{
				Required:    true,
				Description: "IPMI channel (typically 1).",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether SOL payloads are accepted on this channel.",
			},
			"bitrate": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Serial baud rate. One of `9600`, `19200`, `38400`, `57600`, `115200`.",
				Validators:  []validator.String{oneOf("9600", "19200", "38400", "57600", "115200")},
			},
			"privilege_limit": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Min privilege required to open a SOL session: `callback`, `user`, `operator`, `administrator`, `oem`.",
				Validators:  []validator.String{oneOf("callback", "user", "operator", "administrator", "oem")},
			},
			"force_authentication": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Require authentication for SOL payloads. Default true.",
			},
			"force_encryption": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Require encrypted SOL payloads. Default true.",
			},

			"last_updated": schema.StringAttribute{Computed: true},
			"id":           schema.StringAttribute{Computed: true},
		},
	}
}

func (r *solResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *solResource) overrideFromPlan(p solModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *solResource) idFor(override ipmi.ConnectionParams, channel int64) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d/ch%d/sol", merged.Host, port, channel)
}

func (r *solResource) updateFromPlan(p solModel) ipmi.SOLConfigUpdate {
	var u ipmi.SOLConfigUpdate
	if !p.Enabled.IsNull() && !p.Enabled.IsUnknown() {
		v := p.Enabled.ValueBool()
		u.Enabled = &v
	}
	if !p.Bitrate.IsNull() && !p.Bitrate.IsUnknown() {
		b := ipmi.SOLBitrate(p.Bitrate.ValueString())
		u.Bitrate = &b
	}
	if !p.PrivilegeLimit.IsNull() && !p.PrivilegeLimit.IsUnknown() {
		pv := ipmi.UserPrivilege(p.PrivilegeLimit.ValueString())
		u.PrivilegeLimit = &pv
	}
	if !p.ForceAuthentication.IsNull() && !p.ForceAuthentication.IsUnknown() {
		v := p.ForceAuthentication.ValueBool()
		u.ForceAuthentication = &v
	}
	if !p.ForceEncryption.IsNull() && !p.ForceEncryption.IsUnknown() {
		v := p.ForceEncryption.ValueBool()
		u.ForceEncryption = &v
	}
	return u
}

func (r *solResource) applyConfigToState(m *solModel, cfg *ipmi.SOLConfig) {
	if cfg.Supported[1] {
		m.Enabled = types.BoolValue(cfg.Enabled)
	}
	if cfg.Supported[2] {
		m.PrivilegeLimit = types.StringValue(string(cfg.PrivilegeLimit))
		m.ForceAuthentication = types.BoolValue(cfg.ForceAuthentication)
		m.ForceEncryption = types.BoolValue(cfg.ForceEncryption)
	}
	if cfg.Supported[5] {
		// Use non-volatile as the canonical reported value.
		m.Bitrate = types.StringValue(string(cfg.BitrateNonVolatile))
	}
	// Null any Unknown computed fields the BMC didn't report.
	if m.Enabled.IsUnknown() {
		m.Enabled = types.BoolNull()
	}
	if m.Bitrate.IsUnknown() {
		m.Bitrate = types.StringNull()
	}
	if m.PrivilegeLimit.IsUnknown() {
		m.PrivilegeLimit = types.StringNull()
	}
	if m.ForceAuthentication.IsUnknown() {
		m.ForceAuthentication = types.BoolNull()
	}
	if m.ForceEncryption.IsUnknown() {
		m.ForceEncryption = types.BoolNull()
	}
}

func (r *solResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan solModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)
	ch := uint8(plan.Channel.ValueInt64())
	if err := client.ApplySOL(ctx, ch, r.updateFromPlan(plan)); err != nil {
		resp.Diagnostics.AddError("failed to apply SOL config", err.Error())
		return
	}
	cfg, err := client.GetSOL(ctx, ch)
	if err != nil {
		resp.Diagnostics.AddError("failed to read SOL config after Create", err.Error())
		return
	}
	r.applyConfigToState(&plan, cfg)
	plan.LastUpdated = types.StringValue(time.Now().UTC().Format(time.RFC3339))
	plan.ID = types.StringValue(r.idFor(override, int64(ch)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *solResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state solModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.factory.New(r.overrideFromPlan(state))
	cfg, err := client.GetSOL(ctx, uint8(state.Channel.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("failed to read SOL config", err.Error())
		return
	}
	r.applyConfigToState(&state, cfg)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *solResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan solModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)
	ch := uint8(plan.Channel.ValueInt64())
	if err := client.ApplySOL(ctx, ch, r.updateFromPlan(plan)); err != nil {
		resp.Diagnostics.AddError("failed to apply SOL config", err.Error())
		return
	}
	cfg, err := client.GetSOL(ctx, ch)
	if err != nil {
		resp.Diagnostics.AddError("failed to read SOL config after Update", err.Error())
		return
	}
	r.applyConfigToState(&plan, cfg)
	plan.LastUpdated = types.StringValue(time.Now().UTC().Format(time.RFC3339))
	plan.ID = types.StringValue(r.idFor(override, int64(ch)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op — reverting SOL settings is itself disruptive.
func (r *solResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}
