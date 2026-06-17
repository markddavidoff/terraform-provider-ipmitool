package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewFRUDataSource() datasource.DataSource { return &fruDataSource{} }

type fruDataSource struct{ factory *ipmi.ClientFactory }

type fruModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	DeviceID types.Int64 `tfsdk:"device_id"` // input + echoed back

	DeviceDescription types.String `tfsdk:"device_description"`
	ChassisType       types.String `tfsdk:"chassis_type"`
	ChassisSerial     types.String `tfsdk:"chassis_serial"`
	ChassisPartNumber types.String `tfsdk:"chassis_part_number"`
	BoardMfg          types.String `tfsdk:"board_mfg"`
	BoardProduct      types.String `tfsdk:"board_product"`
	BoardSerial       types.String `tfsdk:"board_serial"`
	BoardPartNumber   types.String `tfsdk:"board_part_number"`
	ProductMfg        types.String `tfsdk:"product_mfg"`
	ProductName       types.String `tfsdk:"product_name"`
	ProductSerial     types.String `tfsdk:"product_serial"`
	ProductPartNumber types.String `tfsdk:"product_part_number"`
}

func (d *fruDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fru"
}

func (d *fruDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Field-Replaceable Unit inventory data parsed from `ipmitool fru print <id>`. " +
			"FRU device 0 is the built-in board FRU on Dell servers.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"device_id": schema.Int64Attribute{Optional: true, Computed: true,
				Description: "FRU device ID to read; defaults to 0 (built-in)."},

			"device_description":  schema.StringAttribute{Computed: true},
			"chassis_type":        schema.StringAttribute{Computed: true},
			"chassis_serial":      schema.StringAttribute{Computed: true},
			"chassis_part_number": schema.StringAttribute{Computed: true},
			"board_mfg":           schema.StringAttribute{Computed: true},
			"board_product":       schema.StringAttribute{Computed: true},
			"board_serial":        schema.StringAttribute{Computed: true},
			"board_part_number":   schema.StringAttribute{Computed: true},
			"product_mfg":         schema.StringAttribute{Computed: true},
			"product_name":        schema.StringAttribute{Computed: true},
			"product_serial":      schema.StringAttribute{Computed: true},
			"product_part_number": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *fruDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *fruDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data fruModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deviceID := 0
	if !data.DeviceID.IsNull() && !data.DeviceID.IsUnknown() {
		deviceID = int(data.DeviceID.ValueInt64())
	}
	client := d.factory.New(ipmi.ConnectionParams{
		Host: data.Host.ValueString(), Username: data.Username.ValueString(),
		Password: data.Password.ValueString(), Port: optionalIntPtr(data.Port),
		Interface: data.Interface.ValueString(), CipherSuite: optionalIntPtr(data.CipherSuite),
	})
	f, err := client.GetFRU(ctx, deviceID)
	if err != nil {
		resp.Diagnostics.AddError("failed to read FRU", err.Error())
		return
	}
	data.DeviceID = types.Int64Value(int64(f.DeviceID))
	data.DeviceDescription = types.StringValue(f.DeviceDescription)
	data.ChassisType = types.StringValue(f.ChassisType)
	data.ChassisSerial = types.StringValue(f.ChassisSerial)
	data.ChassisPartNumber = types.StringValue(f.ChassisPartNumber)
	data.BoardMfg = types.StringValue(f.BoardMfg)
	data.BoardProduct = types.StringValue(f.BoardProduct)
	data.BoardSerial = types.StringValue(f.BoardSerial)
	data.BoardPartNumber = types.StringValue(f.BoardPartNumber)
	data.ProductMfg = types.StringValue(f.ProductMfg)
	data.ProductName = types.StringValue(f.ProductName)
	data.ProductSerial = types.StringValue(f.ProductSerial)
	data.ProductPartNumber = types.StringValue(f.ProductPartNumber)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
