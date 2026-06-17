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

func NewLanResource() resource.Resource { return &lanResource{} }

var _ resource.ResourceWithModifyPlan = &lanResource{}

type lanResource struct {
	factory *ipmi.ClientFactory
}

type lanModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	Channel          types.Int64  `tfsdk:"channel"`
	IPSource         types.String `tfsdk:"ip_source"`
	IPAddress        types.String `tfsdk:"ip_address"`
	SubnetMask       types.String `tfsdk:"subnet_mask"`
	DefaultGateway   types.String `tfsdk:"default_gateway"`
	BackupGateway    types.String `tfsdk:"backup_gateway"`
	VLANID          types.Int64  `tfsdk:"vlan_id"`
	VLANEnabled     types.Bool   `tfsdk:"vlan_enabled"`
	VLANPriority    types.Int64  `tfsdk:"vlan_priority"`
	PrimaryRMCPPort types.Int64  `tfsdk:"primary_rmcp_port"`
	MACAddress      types.String `tfsdk:"mac_address"`
	ID              types.String `tfsdk:"id"`
}

func (r *lanResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lan"
}

func (r *lanResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage BMC LAN configuration on one channel. Each set field is written; " +
			"omitted fields are left alone on the BMC. Reads are per-selector and tolerate " +
			"BMCs that don't implement every parameter (POC 3 found R210 II supports 20 of 24).\n\n" +
			"**Lockout warning:** changing `ip_address`, `ip_source`, or `vlan_id` on channel 1 " +
			"can break the provider's BMC connection. The plan is blocked unless " +
			"`TF_IPMI_ALLOW_LOCKOUT=1` is set in the runner environment for the apply.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"channel": schema.Int64Attribute{
				Required:    true,
				Description: "IPMI channel number (1-11). The standard LAN channel is 1.",
			},
			"ip_source": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "`static`, `dhcp`, `bios`, or `other`.",
				Validators:  []validator.String{oneOf("unspecified", "static", "dhcp", "bios", "other")},
			},
			"ip_address": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "BMC IPv4 address as `a.b.c.d`.",
			},
			"subnet_mask": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"default_gateway": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"backup_gateway": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"vlan_id": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "VLAN ID (1-4095). Set to 0 to disable VLAN tagging.",
			},
			"vlan_enabled": schema.BoolAttribute{
				Computed:    true,
				Description: "True when the BMC reports VLAN tagging is active.",
			},
			"vlan_priority": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "802.1p priority (0-7).",
			},
			"primary_rmcp_port": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "IPMI/RMCP+ UDP port. Default 623.",
			},
			"mac_address": schema.StringAttribute{
				Computed:    true,
				Description: "Read-only MAC address of the BMC interface.",
			},
			"id": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *lanResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan triggers the IP-self-lockout guard on channel 1 when the
// plan changes ip_address, ip_source, or vlan_id from the prior state.
func (r *lanResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.factory == nil || req.Plan.Raw.IsNull() {
		return // destroy or not configured yet
	}
	var plan lanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Channel.ValueInt64() != 1 {
		return // only guard channel 1
	}

	var checks []lockoutCheck

	// Treat any explicitly-set value on channel 1 as a potential lockout.
	// Computed-only (null) means the user didn't request a change → safe.
	if !plan.IPAddress.IsNull() && !plan.IPAddress.IsUnknown() {
		// For Create, prior state is empty; for Update, framework gives us
		// the prior state separately. For v0.1 keep it simple: any
		// explicitly-set ip_address on channel 1 is gated.
		var prior lanModel
		if !req.State.Raw.IsNull() {
			resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
		}
		if prior.IPAddress.IsNull() || prior.IPAddress.ValueString() != plan.IPAddress.ValueString() {
			checks = append(checks, lockoutCheck{
				Triggered: true,
				Summary:   "channel 1 ip_address change may break BMC connectivity",
				Detail: fmt.Sprintf(
					"This plan sets the BMC's channel-1 IP to %q. If that differs from "+
						"the IP this Terraform run uses to reach the BMC (provider `host`), "+
						"subsequent reads/applies will fail.",
					plan.IPAddress.ValueString()),
			})
		}
	}

	if !plan.IPSource.IsNull() && !plan.IPSource.IsUnknown() &&
		plan.IPSource.ValueString() == "dhcp" {
		checks = append(checks, lockoutCheck{
			Triggered: true,
			Summary:   "channel 1 ip_source = \"dhcp\" may break BMC connectivity",
			Detail: "Switching the BMC to DHCP releases the current static IP and may " +
				"land it on a different address that the provider cannot reach.",
		})
	}

	if !plan.VLANID.IsNull() && !plan.VLANID.IsUnknown() {
		var prior lanModel
		if !req.State.Raw.IsNull() {
			resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
		}
		planV := plan.VLANID.ValueInt64()
		priorV := prior.VLANID.ValueInt64()
		// Toggling VLAN on or changing to a different VLAN is gated.
		if priorV != planV && (planV != 0 || priorV != 0) {
			checks = append(checks, lockoutCheck{
				Triggered: true,
				Summary:   "channel 1 VLAN change may break BMC connectivity",
				Detail: fmt.Sprintf(
					"This plan changes VLAN ID from %d to %d on channel 1. If the "+
						"new VLAN isn't reachable from the network this Terraform run "+
						"uses, BMC access will be lost.", priorV, planV),
			})
		}
	}

	merged := r.factory.Defaults.Merge(r.overrideFromPlan(plan))
	resp.Diagnostics.Append(enforceLockoutGuards(ctx, "ipmi_lan", merged.Host, checks)...)
}

func (r *lanResource) overrideFromPlan(p lanModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *lanResource) idFor(override ipmi.ConnectionParams, channel int64) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d/ch%d/lan", merged.Host, port, channel)
}

// updateFromPlan walks the plan and returns a LanConfigUpdate with only
// the user-set fields as non-nil pointers. Null/Unknown fields are
// "don't touch this parameter."
func (r *lanResource) updateFromPlan(p lanModel) ipmi.LanConfigUpdate {
	var u ipmi.LanConfigUpdate
	if !p.IPSource.IsNull() && !p.IPSource.IsUnknown() {
		s := ipmi.LanIPSource(p.IPSource.ValueString())
		u.IPSource = &s
	}
	if !p.IPAddress.IsNull() && !p.IPAddress.IsUnknown() {
		s := p.IPAddress.ValueString()
		u.IPAddress = &s
	}
	if !p.SubnetMask.IsNull() && !p.SubnetMask.IsUnknown() {
		s := p.SubnetMask.ValueString()
		u.SubnetMask = &s
	}
	if !p.DefaultGateway.IsNull() && !p.DefaultGateway.IsUnknown() {
		s := p.DefaultGateway.ValueString()
		u.DefaultGateway = &s
	}
	if !p.BackupGateway.IsNull() && !p.BackupGateway.IsUnknown() {
		s := p.BackupGateway.ValueString()
		u.BackupGateway = &s
	}
	if !p.VLANID.IsNull() && !p.VLANID.IsUnknown() {
		v := int(p.VLANID.ValueInt64())
		u.VLANID = &v
	}
	if !p.VLANPriority.IsNull() && !p.VLANPriority.IsUnknown() {
		v := int(p.VLANPriority.ValueInt64())
		u.VLANPriority = &v
	}
	if !p.PrimaryRMCPPort.IsNull() && !p.PrimaryRMCPPort.IsUnknown() {
		v := int(p.PrimaryRMCPPort.ValueInt64())
		u.PrimaryRMCPPort = &v
	}
	return u
}

func (r *lanResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ch := plan.Channel.ValueInt64()
	if ch < 1 || ch > 11 {
		resp.Diagnostics.AddAttributeError(path.Root("channel"),
			"invalid channel", "must be 1..11")
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)
	update := r.updateFromPlan(plan)

	if err := client.ApplyLanConfig(ctx, uint8(ch), update); err != nil {
		resp.Diagnostics.AddError("failed to apply LAN config", err.Error())
		return
	}

	// Read back to populate Computed fields.
	cfg, err := client.GetLanConfig(ctx, uint8(ch))
	if err != nil {
		resp.Diagnostics.AddError("failed to read LAN config after Create", err.Error())
		return
	}
	r.applyConfigToState(&plan, cfg)
	plan.ID = types.StringValue(r.idFor(override, ch))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *lanResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lanModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(state)
	client := r.factory.New(override)
	ch := uint8(state.Channel.ValueInt64())

	cfg, err := client.GetLanConfig(ctx, ch)
	if err != nil {
		resp.Diagnostics.AddError("failed to read LAN config", err.Error())
		return
	}
	r.applyConfigToState(&state, cfg)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lanResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan lanModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)
	ch := uint8(plan.Channel.ValueInt64())
	update := r.updateFromPlan(plan)

	if err := client.ApplyLanConfig(ctx, ch, update); err != nil {
		resp.Diagnostics.AddError("failed to apply LAN config", err.Error())
		return
	}
	cfg, err := client.GetLanConfig(ctx, ch)
	if err != nil {
		resp.Diagnostics.AddError("failed to read LAN config after Update", err.Error())
		return
	}
	r.applyConfigToState(&plan, cfg)
	plan.ID = types.StringValue(r.idFor(override, int64(ch)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op — reverting to a "default" LAN config is itself a
// lockout risk. The user should explicitly opt-in to clearing settings
// by Updating to the desired safe values before destroying.
func (r *lanResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

// applyConfigToState writes BMC-returned values to model fields, but
// only for parameters the BMC actually supports. Unsupported selectors
// leave the corresponding field at its prior (or null) value.
func (r *lanResource) applyConfigToState(m *lanModel, cfg *ipmi.LanConfig) {
	supported := cfg.Supported
	if supported[4] /* IPSource */ {
		m.IPSource = types.StringValue(string(cfg.IPSource))
	}
	if supported[3] /* IP */ {
		m.IPAddress = types.StringValue(cfg.IPAddress)
	}
	if supported[6] /* SubnetMask */ {
		m.SubnetMask = types.StringValue(cfg.SubnetMask)
	}
	if supported[12] /* DefaultGateway */ {
		m.DefaultGateway = types.StringValue(cfg.DefaultGateway)
	}
	if supported[14] /* BackupGateway */ {
		m.BackupGateway = types.StringValue(cfg.BackupGateway)
	}
	if supported[5] /* MAC */ {
		m.MACAddress = types.StringValue(cfg.MAC)
	}
	if supported[20] /* VLANID */ {
		m.VLANID = types.Int64Value(int64(cfg.VLANID))
		m.VLANEnabled = types.BoolValue(cfg.VLANEnabled)
	}
	if supported[21] /* VLANPriority */ {
		m.VLANPriority = types.Int64Value(int64(cfg.VLANPriority))
	}
	if supported[8] /* PrimaryRMCPPort */ {
		m.PrimaryRMCPPort = types.Int64Value(int64(cfg.PrimaryRMCPPort))
	}
	// Fields that were not read (null/unset) get null values so framework
	// doesn't complain about Computed attrs being missing.
	if m.IPSource.IsUnknown() {
		m.IPSource = types.StringNull()
	}
	if m.IPAddress.IsUnknown() {
		m.IPAddress = types.StringNull()
	}
	if m.SubnetMask.IsUnknown() {
		m.SubnetMask = types.StringNull()
	}
	if m.DefaultGateway.IsUnknown() {
		m.DefaultGateway = types.StringNull()
	}
	if m.BackupGateway.IsUnknown() {
		m.BackupGateway = types.StringNull()
	}
	if m.MACAddress.IsUnknown() {
		m.MACAddress = types.StringNull()
	}
	if m.VLANID.IsUnknown() {
		m.VLANID = types.Int64Null()
	}
	if m.VLANEnabled.IsUnknown() {
		m.VLANEnabled = types.BoolNull()
	}
	if m.VLANPriority.IsUnknown() {
		m.VLANPriority = types.Int64Null()
	}
	if m.PrimaryRMCPPort.IsUnknown() {
		m.PrimaryRMCPPort = types.Int64Null()
	}
}

func (r *lanResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	host, port, channel, err := parseHostPortChannelID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("host"), host)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("port"), int64(port))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("channel"), int64(channel))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
