package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

// NewChassisStatusDataSource is the factory the provider registers.
func NewChassisStatusDataSource() datasource.DataSource {
	return &chassisStatusDataSource{}
}

type chassisStatusDataSource struct {
	factory *ipmi.ClientFactory
}

// chassisStatusModel mirrors the data source schema for tfsdk decode.
//
// Connection attrs are all Optional — when set they override the
// provider's defaults for this one read. This is what enables a single
// provider block to manage many BMCs via for_each.
type chassisStatusModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	PowerOn          types.Bool `tfsdk:"power_on"`
	PowerOverload    types.Bool `tfsdk:"power_overload"`
	PowerFault       types.Bool `tfsdk:"power_fault"`
	ChassisIntrusion types.Bool `tfsdk:"chassis_intrusion"`
}

func (d *chassisStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_chassis_status"
}

func (d *chassisStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read the chassis power state and fault indicators from a BMC via " +
			"`ipmitool chassis status`. Connection attributes here override the provider " +
			"defaults for this read; omit them to use the provider block's defaults.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true, Description: "Override provider host."},
			"username":     schema.StringAttribute{Optional: true, Description: "Override provider username."},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true, Description: "Override provider password."},
			"port":         schema.Int64Attribute{Optional: true, Description: "Override provider port."},
			"interface":    schema.StringAttribute{Optional: true, Description: "Override provider interface."},
			"cipher_suite": schema.Int64Attribute{Optional: true, Description: "Override provider cipher_suite."},

			"power_on":          schema.BoolAttribute{Computed: true, Description: "True if system power is on."},
			"power_overload":    schema.BoolAttribute{Computed: true, Description: "True if power overload condition is active."},
			"power_fault":       schema.BoolAttribute{Computed: true, Description: "True if main power fault is reported."},
			"chassis_intrusion": schema.BoolAttribute{Computed: true, Description: "True if chassis intrusion is active."},
		},
	}
}

func (d *chassisStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		// Configure is called twice: once before provider.Configure runs
		// (ProviderData is nil), and once after. Skip the first.
		return
	}
	factory, ok := req.ProviderData.(*ipmi.ClientFactory)
	if !ok {
		resp.Diagnostics.AddError(
			"unexpected provider data type",
			fmt.Sprintf("expected *ipmi.ClientFactory, got %T (provider bug)", req.ProviderData),
		)
		return
	}
	d.factory = factory
}

func (d *chassisStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data chassisStatusModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	override := ipmi.ConnectionParams{
		Host:        data.Host.ValueString(),
		Username:    data.Username.ValueString(),
		Password:    data.Password.ValueString(),
		Port:        int(data.Port.ValueInt64()),
		Interface:   data.Interface.ValueString(),
		CipherSuite: int(data.CipherSuite.ValueInt64()),
	}
	client := d.factory.New(override)

	status, err := client.GetChassisStatus(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to get chassis status", err.Error())
		return
	}

	data.PowerOn = types.BoolValue(status.PowerOn)
	data.PowerOverload = types.BoolValue(status.PowerOverload)
	data.PowerFault = types.BoolValue(status.PowerFault)
	data.ChassisIntrusion = types.BoolValue(status.ChassisIntrusion)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
