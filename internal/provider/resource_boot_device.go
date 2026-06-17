package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewBootDeviceResource() resource.Resource { return &bootDeviceResource{} }

type bootDeviceResource struct {
	factory *ipmi.ClientFactory
}

type bootDeviceModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	Device      types.String `tfsdk:"device"`
	Persistent  types.Bool   `tfsdk:"persistent"`
	EFI         types.Bool   `tfsdk:"efi"`
	ID          types.String `tfsdk:"id"`
}

func (r *bootDeviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_boot_device"
}

func (r *bootDeviceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Set the BMC boot-device override.\n\n" +
			"With `persistent = false` (the default), the override is a **one-shot**: BIOS " +
			"consumes the flag on the next boot and clears it automatically (per IPMI 2.0 " +
			"spec, confirmed on Dell 11G in plans/poc-1-bootdev-result.md). One-shots do " +
			"not produce drift on subsequent `terraform plan` runs — the resource is " +
			"considered satisfied by intent, not by post-boot BMC state.\n\n" +
			"With `persistent = true`, the override survives reboots and standard drift " +
			"detection applies: if anything changes the boot device out-of-band, the next " +
			"apply will reconcile.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"device": schema.StringAttribute{
				Required:    true,
				Description: "Boot device: `none`, `pxe`, `disk`, `cdrom`, `bios`, `floppy`.",
				Validators: []validator.String{
					oneOf("none", "pxe", "disk", "cdrom", "bios", "floppy"),
				},
			},
			"persistent": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, override survives reboots. Default false (one-shot).",
			},
			"efi": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, UEFI boot. Default false (legacy BIOS).",
			},
			"id":           schema.StringAttribute{Computed: true},
		},
	}
}

func (r *bootDeviceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *bootDeviceResource) overrideFromPlan(p bootDeviceModel) ipmi.ConnectionParams {
	return ipmi.ConnectionParams{
		Host: p.Host.ValueString(), Username: p.Username.ValueString(),
		Password: p.Password.ValueString(), Port: optionalIntPtr(p.Port),
		Interface: p.Interface.ValueString(), CipherSuite: optionalIntPtr(p.CipherSuite),
	}
}

func (r *bootDeviceResource) idFor(override ipmi.ConnectionParams) string {
	merged := r.factory.Defaults.Merge(override)
	port := 623
	if merged.Port != nil {
		port = *merged.Port
	}
	return fmt.Sprintf("%s:%d", merged.Host, port)
}

func (r *bootDeviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bootDeviceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)

	persistent := plan.Persistent.ValueBool()
	efi := plan.EFI.ValueBool()
	device := ipmi.BootDevice(plan.Device.ValueString())

	if err := client.SetBootDevice(ctx, device, persistent, efi); err != nil {
		resp.Diagnostics.AddError("failed to set boot device", err.Error())
		return
	}

	plan.Persistent = types.BoolValue(persistent)
	plan.EFI = types.BoolValue(efi)
	plan.ID = types.StringValue(r.idFor(override))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements the persistent-vs-one-shot drift semantics:
//   - persistent=false: no drift check. BIOS may have consumed the flag
//     already (POC 1 finding); state stays as the user asked.
//   - persistent=true: query BMC; if device differs, surface as drift.
func (r *bootDeviceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bootDeviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !state.Persistent.ValueBool() {
		// One-shot — trust the original state; consumed-by-BIOS is not drift.
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	client := r.factory.New(r.overrideFromPlan(state))
	flags, err := client.GetBootFlags(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to read boot flags", err.Error())
		return
	}
	// Only reconcile when the BMC still reports a valid persistent override.
	// If BMC reports "no override" but we expected persistent, that is real
	// drift and we want Terraform to plan a re-apply.
	if flags.Valid && flags.Persistent {
		state.Device = types.StringValue(string(flags.Device))
		state.EFI = types.BoolValue(flags.EFI)
		state.Persistent = types.BoolValue(true)
	} else {
		// Drift: persistent override no longer present.
		state.Device = types.StringValue(string(ipmi.BootDeviceNone))
		state.Persistent = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *bootDeviceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan bootDeviceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	override := r.overrideFromPlan(plan)
	client := r.factory.New(override)

	persistent := plan.Persistent.ValueBool()
	efi := plan.EFI.ValueBool()
	device := ipmi.BootDevice(plan.Device.ValueString())

	if err := client.SetBootDevice(ctx, device, persistent, efi); err != nil {
		resp.Diagnostics.AddError("failed to set boot device", err.Error())
		return
	}
	plan.Persistent = types.BoolValue(persistent)
	plan.EFI = types.BoolValue(efi)
	plan.ID = types.StringValue(r.idFor(override))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the boot override. Always safe — leaves BIOS at its
// configured default boot order.
func (r *bootDeviceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bootDeviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.factory.New(r.overrideFromPlan(state))
	if err := client.SetBootDevice(ctx, ipmi.BootDeviceNone, false, false); err != nil {
		resp.Diagnostics.AddError("failed to clear boot device on destroy", err.Error())
	}
}
