package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	stringpm "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewUserResource() resource.Resource { return &userResource{} }

// Compile-time assertion: framework will only call ModifyPlan on
// resources that implement ResourceWithModifyPlan explicitly.
var _ resource.ResourceWithModifyPlan = &userResource{}

type userResource struct {
	factory *ipmi.ClientFactory
}

type userModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	UserID            types.Int64  `tfsdk:"user_id"`
	Name              types.String `tfsdk:"name"`
	UserPassword      types.String `tfsdk:"user_password"`
	Privilege         types.String `tfsdk:"privilege"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	Channel           types.Int64  `tfsdk:"channel"`
	ForceLockoutRisk  types.Bool   `tfsdk:"force_lockout_risk"`
	LastUpdated       types.String `tfsdk:"last_updated"`
	ID                types.String `tfsdk:"id"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage one IPMI user slot (typically slots 1–15). Includes a " +
			"self-disable guard: if the user being modified matches the connection " +
			"`username` and the plan would disable the slot, apply errors unless " +
			"`force_lockout_risk = true` is set.\n\n" +
			"**Note:** `user_password` is write-only — the BMC does not return it on " +
			"reads, so the provider cannot detect out-of-band password changes.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"user_id": schema.Int64Attribute{
				Required:    true,
				Description: "User slot ID, typically 1–15. Slot 1 is reserved/anonymous on most BMCs.",
				PlanModifiers: []planmodifier.Int64{
					// Changing user_id forces re-create — different slot is a different resource.
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Username string written to the slot.",
			},
			"user_password": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Password set on the slot. Write-only — never returned by Read.",
				PlanModifiers: []planmodifier.String{
					stringpm.UseStateForUnknown(),
				},
			},
			"privilege": schema.StringAttribute{
				Required: true,
				Description: "Privilege level on this channel: `callback`, `user`, `operator`, " +
					"`administrator`, `oem`, or `no_access`.",
				Validators: []validator.String{
					oneOf("callback", "user", "operator", "administrator", "oem", "no_access"),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the user slot is enabled. Default true.",
			},
			"channel": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Channel number this privilege applies to. Default 1.",
			},
			"force_lockout_risk": schema.BoolAttribute{
				Optional: true,
				Description: "Set to true to override lockout-safety errors when the plan would " +
					"disable the connection user (which would lock Terraform out of the BMC).",
			},
			"last_updated": schema.StringAttribute{Computed: true},
			"id":           schema.StringAttribute{Computed: true},
		},
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan runs the self-disable lockout guard.
//
// The check compares the resource's `name` field against the connection
// `username` (provider default merged with per-resource override). If
// they match AND the plan disables the slot, the apply requires an
// explicit force_lockout_risk = true opt-in.
func (r *userResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.factory == nil || req.Plan.Raw.IsNull() {
		// Nothing to check on destroy plan.
		return
	}
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	connUser := r.connectionUser(plan)
	resourceName := plan.Name.ValueString()
	willDisable := !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() && !plan.Enabled.ValueBool()

	selfDisable := lockoutCheck{
		Triggered: connUser != "" && resourceName == connUser && willDisable,
		Summary:   "self-disable would lock Terraform out of the BMC",
		Detail: fmt.Sprintf(
			"This plan disables the user %q which is the same user this Terraform run "+
				"authenticates with. Applying would prevent subsequent reads/applies "+
				"from talking to the BMC.",
			resourceName,
		),
	}
	resp.Diagnostics.Append(enforceLockoutGuards(plan.ForceLockoutRisk, []lockoutCheck{selfDisable})...)
}

// connectionUser returns the username the BMC connection will use,
// merging per-resource override on top of provider defaults.
func (r *userResource) connectionUser(p userModel) string {
	override := r.overrideFromPlan(p)
	merged := r.factory.Defaults.Merge(override)
	return merged.Username
}

func (r *userResource) overrideFromPlan(p userModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: int(p.Port.ValueInt64()),
		Interface: p.Interface.ValueString(), CipherSuite: int(p.CipherSuite.ValueInt64()),
	}
}

func (r *userResource) idFor(override ipmi.ConnectionParams, userID, channel int64) string {
	merged := r.factory.Defaults.Merge(override)
	port := merged.Port
	if port == 0 {
		port = 623
	}
	return fmt.Sprintf("%s:%d/ch%d/user%d", merged.Host, port, channel, userID)
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	enabled := true
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		enabled = plan.Enabled.ValueBool()
	}
	channel := int64(1)
	if !plan.Channel.IsNull() && !plan.Channel.IsUnknown() {
		channel = plan.Channel.ValueInt64()
	}
	userID := plan.UserID.ValueInt64()
	if userID < 1 || userID > 15 {
		resp.Diagnostics.AddAttributeError(path.Root("user_id"),
			"invalid user_id", "must be between 1 and 15")
		return
	}

	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)

	if err := client.SetUserName(ctx, uint8(userID), plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("failed to set user name", err.Error())
		return
	}
	if err := client.SetUserPassword(ctx, uint8(userID), plan.UserPassword.ValueString()); err != nil {
		resp.Diagnostics.AddError("failed to set user password", err.Error())
		return
	}
	priv := ipmi.UserPrivilege(plan.Privilege.ValueString())
	if err := client.SetUserPrivilege(ctx, uint8(userID), uint8(channel), priv); err != nil {
		resp.Diagnostics.AddError("failed to set user privilege", err.Error())
		return
	}
	if enabled {
		if err := client.EnableUser(ctx, uint8(userID)); err != nil {
			resp.Diagnostics.AddError("failed to enable user", err.Error())
			return
		}
	} else {
		if err := client.DisableUser(ctx, uint8(userID)); err != nil {
			resp.Diagnostics.AddError("failed to disable user", err.Error())
			return
		}
	}

	plan.Enabled = types.BoolValue(enabled)
	plan.Channel = types.Int64Value(channel)
	plan.LastUpdated = types.StringValue(time.Now().UTC().Format(time.RFC3339))
	plan.ID = types.StringValue(r.idFor(override, userID, channel))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(state)
	client := r.factory.New(override)

	channel := state.Channel.ValueInt64()
	if channel == 0 {
		channel = 1
	}
	users, err := client.GetUsers(ctx, uint8(channel))
	if err != nil {
		resp.Diagnostics.AddError("failed to read users", err.Error())
		return
	}
	wantID := state.UserID.ValueInt64()
	var found *ipmi.User
	for i := range users {
		if int64(users[i].ID) == wantID {
			found = &users[i]
			break
		}
	}
	if found == nil {
		// User slot returned no row — treat as removed-out-of-band.
		resp.State.RemoveResource(ctx)
		return
	}
	state.Name = types.StringValue(found.Name)
	if found.Privilege != "" {
		state.Privilege = types.StringValue(string(found.Privilege))
	}
	state.Enabled = types.BoolValue(found.Enabled)
	state.Channel = types.Int64Value(channel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Same as Create for the parts that matter — IPMI user mgmt is
	// idempotent set-by-id semantics. Re-set everything.
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enabled := true
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		enabled = plan.Enabled.ValueBool()
	}
	channel := int64(1)
	if !plan.Channel.IsNull() && !plan.Channel.IsUnknown() {
		channel = plan.Channel.ValueInt64()
	}
	userID := plan.UserID.ValueInt64()
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)

	if err := client.SetUserName(ctx, uint8(userID), plan.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("failed to set user name", err.Error())
		return
	}
	// Only re-set password if changed (plan != state) — but framework gives
	// us the plan value either way; setting always is safe + idempotent.
	if err := client.SetUserPassword(ctx, uint8(userID), plan.UserPassword.ValueString()); err != nil {
		resp.Diagnostics.AddError("failed to set user password", err.Error())
		return
	}
	priv := ipmi.UserPrivilege(plan.Privilege.ValueString())
	if err := client.SetUserPrivilege(ctx, uint8(userID), uint8(channel), priv); err != nil {
		resp.Diagnostics.AddError("failed to set user privilege", err.Error())
		return
	}
	if enabled {
		if err := client.EnableUser(ctx, uint8(userID)); err != nil {
			resp.Diagnostics.AddError("failed to enable user", err.Error())
			return
		}
	} else {
		if err := client.DisableUser(ctx, uint8(userID)); err != nil {
			resp.Diagnostics.AddError("failed to disable user", err.Error())
			return
		}
	}
	plan.Enabled = types.BoolValue(enabled)
	plan.Channel = types.Int64Value(channel)
	plan.LastUpdated = types.StringValue(time.Now().UTC().Format(time.RFC3339))
	plan.ID = types.StringValue(r.idFor(override, userID, channel))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(state)
	client := r.factory.New(override)
	userID := state.UserID.ValueInt64()
	// Disable the slot. Don't try to clear the name — some BMCs require
	// a non-empty name.
	if err := client.DisableUser(ctx, uint8(userID)); err != nil {
		resp.Diagnostics.AddError("failed to disable user on destroy", err.Error())
	}
}
