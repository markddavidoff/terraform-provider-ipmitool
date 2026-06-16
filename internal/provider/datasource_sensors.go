package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

func NewSensorsDataSource() datasource.DataSource { return &sensorsDataSource{} }

type sensorsDataSource struct{ factory *ipmi.ClientFactory }

type sensorsModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	Sensors types.List `tfsdk:"sensors"`
}

var sensorObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"name":    types.StringType,
	"reading": types.StringType,
	"unit":    types.StringType,
	"status":  types.StringType,
}}

func (d *sensorsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sensors"
}

func (d *sensorsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "All Sensor Data Records (SDR) from `ipmitool sdr list` — temperatures, " +
			"fan RPMs, voltages, currents, discrete state sensors.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"sensors": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":    schema.StringAttribute{Computed: true},
						"reading": schema.StringAttribute{Computed: true, Description: "Numeric value, or raw hex for discrete sensors."},
						"unit":    schema.StringAttribute{Computed: true, Description: "Empty for discrete sensors."},
						"status":  schema.StringAttribute{Computed: true, Description: "ok, nc, cr, nr, ns."},
					},
				},
			},
		},
	}
}

func (d *sensorsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sensorsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data sensorsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := d.factory.New(ipmi.ConnectionParams{
		Host: data.Host.ValueString(), Username: data.Username.ValueString(),
		Password: data.Password.ValueString(), Port: int(data.Port.ValueInt64()),
		Interface: data.Interface.ValueString(), CipherSuite: int(data.CipherSuite.ValueInt64()),
	})
	sensors, err := client.GetSensors(ctx)
	if err != nil {
		resp.Diagnostics.AddError("failed to read sensors", err.Error())
		return
	}

	objs := make([]attr.Value, 0, len(sensors))
	for _, s := range sensors {
		obj, diags := types.ObjectValue(sensorObjectType.AttrTypes, map[string]attr.Value{
			"name":    types.StringValue(s.Name),
			"reading": types.StringValue(s.Reading),
			"unit":    types.StringValue(s.Unit),
			"status":  types.StringValue(s.Status),
		})
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		objs = append(objs, obj)
	}
	list, diags := types.ListValue(sensorObjectType, objs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Sensors = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
