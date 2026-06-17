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

func NewChannelAccessResource() resource.Resource { return &channelAccessResource{} }

var _ resource.ResourceWithModifyPlan = &channelAccessResource{}

type channelAccessResource struct {
	factory *ipmi.ClientFactory
}

type channelAccessModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	Channel          types.Int64  `tfsdk:"channel"`
	AccessMode       types.String `tfsdk:"access_mode"`
	UserLevelAuth    types.Bool   `tfsdk:"user_level_auth"`
	PerMessageAuth   types.Bool   `tfsdk:"per_message_auth"`
	PEFAlerting      types.Bool   `tfsdk:"pef_alerting"`
	PrivilegeLimit   types.String `tfsdk:"privilege_limit"`
	Persistence      types.String `tfsdk:"persistence"`
	ForceLockoutRisk types.Bool   `tfsdk:"force_lockout_risk"`
	ID               types.String `tfsdk:"id"`
}

func (r *channelAccessResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_channel_access"
}

func (r *channelAccessResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Set Channel Access for one channel — controls whether IPMI-over-LAN " +
			"is enabled, what authentication is required, and the max privilege level.\n\n" +
			"**Lockout warning:** setting `access_mode = \"disabled\"` on channel 1 (the standard " +
			"LAN channel) will lock Terraform out of the BMC. This resource requires an explicit " +
			"`force_lockout_risk = true` to allow it.",
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
			"access_mode": schema.StringAttribute{
				Required:    true,
				Description: "When this channel accepts sessions: `disabled`, `pre_boot`, `always`, `shared`.",
				Validators:  []validator.String{oneOf("disabled", "pre_boot", "always", "shared")},
			},
			"user_level_auth": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, user-level commands require authentication. Default true (safer).",
			},
			"per_message_auth": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, every IPMI message is authenticated. Default true (safer).",
			},
			"pef_alerting": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, Platform Event Filter alerting is enabled on this channel.",
			},
			"privilege_limit": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Max privilege level granted on this channel: `callback`, `user`, `operator`, `administrator`, `oem`.",
				Validators: []validator.String{
					oneOf("callback", "user", "operator", "administrator", "oem"),
				},
			},
			"persistence": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Where to write: `volatile` (active only), `non_volatile` (boot defaults), " +
					"or `both` (default). Reads always return the volatile/active settings.",
				Validators: []validator.String{oneOf("volatile", "non_volatile", "both")},
			},
			"force_lockout_risk": schema.BoolAttribute{
				Optional: true,
				Description: "Set to true to override the channel-1 self-lockout guard. " +
					"Without it, disabling LAN access on the channel Terraform uses is blocked.",
			},
			"id":           schema.StringAttribute{Computed: true},
		},
	}
}

func (r *channelAccessResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan implements the channel-self-lockout guard.
//
// The simple, defensible rule: if the plan would set `access_mode =
// "disabled"` on **channel 1** (the standard LAN channel Terraform
// authenticates over by default), require force_lockout_risk = true.
// This is conservative — users on non-standard channels are responsible
// for knowing if they're cutting their own connection.
func (r *channelAccessResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var plan channelAccessModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ch := plan.Channel.ValueInt64()
	mode := plan.AccessMode.ValueString()
	check := lockoutCheck{
		Triggered: ch == 1 && mode == "disabled",
		Summary:   "channel 1 access_mode = \"disabled\" would lock Terraform out",
		Detail: "This plan disables IPMI access on channel 1, which is the standard LAN " +
			"channel Terraform connects through. The BMC will reject all subsequent IPMI " +
			"sessions until access is re-enabled via the host's serial console or BIOS.",
	}
	resp.Diagnostics.Append(enforceLockoutGuards(plan.ForceLockoutRisk, []lockoutCheck{check})...)
}

func (r *channelAccessResource) overrideFromPlan(p channelAccessModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *channelAccessResource) idFor(override ipmi.ConnectionParams, channel int64) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d/ch%d/access", merged.Host, port, channel)
}

// channelAccessFromPlan applies defaults for any Optional+Computed fields
// the user didn't set explicitly.
func (r *channelAccessResource) accessFromPlan(p channelAccessModel) ipmi.ChannelAccess {
	access := ipmi.ChannelAccess{
		AccessMode:     ipmi.ChannelAccessMode(p.AccessMode.ValueString()),
		UserLevelAuth:  true, // safer default
		PerMessageAuth: true,
		PEFAlerting:    true,
		PrivilegeLimit: ipmi.UserPrivAdministrator,
	}
	if !p.UserLevelAuth.IsNull() && !p.UserLevelAuth.IsUnknown() {
		access.UserLevelAuth = p.UserLevelAuth.ValueBool()
	}
	if !p.PerMessageAuth.IsNull() && !p.PerMessageAuth.IsUnknown() {
		access.PerMessageAuth = p.PerMessageAuth.ValueBool()
	}
	if !p.PEFAlerting.IsNull() && !p.PEFAlerting.IsUnknown() {
		access.PEFAlerting = p.PEFAlerting.ValueBool()
	}
	if !p.PrivilegeLimit.IsNull() && !p.PrivilegeLimit.IsUnknown() {
		access.PrivilegeLimit = ipmi.UserPrivilege(p.PrivilegeLimit.ValueString())
	}
	return access
}

func (r *channelAccessResource) persistenceFromPlan(p channelAccessModel) ipmi.ChannelPersistence {
	if p.Persistence.IsNull() || p.Persistence.IsUnknown() {
		return ipmi.PersistBoth
	}
	return ipmi.ChannelPersistence(p.Persistence.ValueString())
}

func (r *channelAccessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan channelAccessModel
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
	access := r.accessFromPlan(plan)
	persist := r.persistenceFromPlan(plan)

	if err := client.SetChannelAccess(ctx, uint8(ch), access, persist); err != nil {
		resp.Diagnostics.AddError("failed to set channel access", err.Error())
		return
	}

	plan.UserLevelAuth = types.BoolValue(access.UserLevelAuth)
	plan.PerMessageAuth = types.BoolValue(access.PerMessageAuth)
	plan.PEFAlerting = types.BoolValue(access.PEFAlerting)
	plan.PrivilegeLimit = types.StringValue(string(access.PrivilegeLimit))
	plan.Persistence = types.StringValue(string(persist))
	plan.ID = types.StringValue(r.idFor(override, ch))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *channelAccessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state channelAccessModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(state)
	client := r.factory.New(override)
	ch := uint8(state.Channel.ValueInt64())

	// Read volatile (active) settings — that's what affects connectivity now.
	current, err := client.GetChannelAccess(ctx, ch, ipmi.PersistVolatile)
	if err != nil {
		resp.Diagnostics.AddError("failed to read channel access", err.Error())
		return
	}
	state.AccessMode = types.StringValue(string(current.AccessMode))
	state.UserLevelAuth = types.BoolValue(current.UserLevelAuth)
	state.PerMessageAuth = types.BoolValue(current.PerMessageAuth)
	state.PEFAlerting = types.BoolValue(current.PEFAlerting)
	state.PrivilegeLimit = types.StringValue(string(current.PrivilegeLimit))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *channelAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan channelAccessModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)
	ch := uint8(plan.Channel.ValueInt64())
	access := r.accessFromPlan(plan)
	persist := r.persistenceFromPlan(plan)

	if err := client.SetChannelAccess(ctx, ch, access, persist); err != nil {
		resp.Diagnostics.AddError("failed to set channel access", err.Error())
		return
	}
	plan.UserLevelAuth = types.BoolValue(access.UserLevelAuth)
	plan.PerMessageAuth = types.BoolValue(access.PerMessageAuth)
	plan.PEFAlerting = types.BoolValue(access.PEFAlerting)
	plan.PrivilegeLimit = types.StringValue(string(access.PrivilegeLimit))
	plan.Persistence = types.StringValue(string(persist))
	plan.ID = types.StringValue(r.idFor(override, int64(ch)))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete is a no-op — we don't know what the user's previous channel
// state was, and reverting to a "default" would itself be risky. Removing
// the resource from state leaves BMC settings alone.
func (r *channelAccessResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}
