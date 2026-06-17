package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewBMCInfoDataSource() datasource.DataSource { return &bmcInfoDataSource{} }

type bmcInfoDataSource struct{ factory *ipmi.ClientFactory }

type bmcInfoModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	DeviceID         types.Int64  `tfsdk:"device_id"`
	DeviceRevision   types.Int64  `tfsdk:"device_revision"`
	FirmwareVersion  types.String `tfsdk:"firmware_version"`
	IPMIVersion      types.String `tfsdk:"ipmi_version"`
	ManufacturerID   types.Int64  `tfsdk:"manufacturer_id"`
	ManufacturerName types.String `tfsdk:"manufacturer_name"`
	ProductID        types.Int64  `tfsdk:"product_id"`
	ProductName      types.String `tfsdk:"product_name"`
	DeviceAvailable  types.Bool   `tfsdk:"device_available"`
}

func (d *bmcInfoDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bmc_info"
}

func (d *bmcInfoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "BMC firmware identification + capabilities, parsed from `ipmitool mc info`.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"device_id":         schema.Int64Attribute{Computed: true},
			"device_revision":   schema.Int64Attribute{Computed: true},
			"firmware_version":  schema.StringAttribute{Computed: true},
			"ipmi_version":      schema.StringAttribute{Computed: true},
			"manufacturer_id":   schema.Int64Attribute{Computed: true},
			"manufacturer_name": schema.StringAttribute{Computed: true},
			"product_id":        schema.Int64Attribute{Computed: true},
			"product_name":      schema.StringAttribute{Computed: true},
			"device_available":  schema.BoolAttribute{Computed: true},
		},
	}
}

func (d *bmcInfoDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	factory, ok := req.ProviderData.(*ipmi.ClientFactory)
	if !ok {
		resp.Diagnostics.AddError("unexpected provider data type",
			fmt.Sprintf("expected *ipmi.ClientFactory, got %T (bug)", req.ProviderData))
		return
	}
	d.factory = factory
}

func (d *bmcInfoDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data bmcInfoModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := d.factory.New(ipmi.ConnectionParams{
		Host: data.Host.ValueString(), Username: data.Username.ValueString(),
		Password: data.Password.ValueString(), Port: optionalIntPtr(data.Port),
		Interface: data.Interface.ValueString(), CipherSuite: optionalIntPtr(data.CipherSuite),
	})
	info, err := client.GetBMCInfo(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to get BMC info", err.Error())
		return
	}
	data.DeviceID = types.Int64Value(int64(info.DeviceID))
	data.DeviceRevision = types.Int64Value(int64(info.DeviceRevision))
	data.FirmwareVersion = types.StringValue(info.FirmwareVersion)
	data.IPMIVersion = types.StringValue(info.IPMIVersion)
	data.ManufacturerID = types.Int64Value(int64(info.ManufacturerID))
	data.ManufacturerName = types.StringValue(info.ManufacturerName)
	data.ProductID = types.Int64Value(int64(info.ProductID))
	data.ProductName = types.StringValue(info.ProductName)
	data.DeviceAvailable = types.BoolValue(info.DeviceAvailable)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
