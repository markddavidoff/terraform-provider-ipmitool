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

func NewSELDataSource() datasource.DataSource { return &selDataSource{} }

type selDataSource struct{ factory *ipmi.ClientFactory }

type selModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	Port        types.Int64  `tfsdk:"port"`
	Interface   types.String `tfsdk:"interface"`
	CipherSuite types.Int64  `tfsdk:"cipher_suite"`

	MaxEntries types.Int64 `tfsdk:"max_entries"`
	Entries    types.List  `tfsdk:"entries"`
}

// selEntryObjectType is the Terraform object type for one SEL record.
// Declared once so both the schema and the value-marshaling agree.
var selEntryObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"record_id":   types.StringType,
	"timestamp":   types.StringType,
	"sensor":      types.StringType,
	"event_type":  types.StringType,
	"direction":   types.StringType,
	"description": types.StringType,
}}

func (d *selDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sel"
}

func (d *selDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Up to `max_entries` most-recent System Event Log records from " +
			"`ipmitool sel elist last <N>`.",
		Attributes: map[string]schema.Attribute{
			"host":         schema.StringAttribute{Optional: true},
			"username":     schema.StringAttribute{Optional: true},
			"password":     schema.StringAttribute{Optional: true, Sensitive: true},
			"port":         schema.Int64Attribute{Optional: true},
			"interface":    schema.StringAttribute{Optional: true},
			"cipher_suite": schema.Int64Attribute{Optional: true},

			"max_entries": schema.Int64Attribute{Optional: true, Computed: true,
				Description: "Most-recent N entries to fetch; default 100."},

			"entries": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Newest-first list of SEL records.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"record_id":   schema.StringAttribute{Computed: true},
						"timestamp":   schema.StringAttribute{Computed: true},
						"sensor":      schema.StringAttribute{Computed: true},
						"event_type":  schema.StringAttribute{Computed: true},
						"direction":   schema.StringAttribute{Computed: true},
						"description": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *selDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *selDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data selModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	maxEntries := 100
	if !data.MaxEntries.IsNull() && !data.MaxEntries.IsUnknown() {
		maxEntries = int(data.MaxEntries.ValueInt64())
	}

	client := d.factory.New(ipmi.ConnectionParams{
		Host: data.Host.ValueString(), Username: data.Username.ValueString(),
		Password: data.Password.ValueString(), Port: int(data.Port.ValueInt64()),
		Interface: data.Interface.ValueString(), CipherSuite: int(data.CipherSuite.ValueInt64()),
	})

	entries, err := client.GetSEL(ctx, maxEntries)
	if err != nil {
		resp.Diagnostics.AddError("failed to read SEL", err.Error())
		return
	}

	objs := make([]attr.Value, 0, len(entries))
	for _, e := range entries {
		obj, diags := types.ObjectValue(selEntryObjectType.AttrTypes, map[string]attr.Value{
			"record_id":   types.StringValue(e.RecordID),
			"timestamp":   types.StringValue(e.Timestamp),
			"sensor":      types.StringValue(e.Sensor),
			"event_type":  types.StringValue(e.EventType),
			"direction":   types.StringValue(e.Direction),
			"description": types.StringValue(e.Description),
		})
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		objs = append(objs, obj)
	}
	list, diags := types.ListValue(selEntryObjectType, objs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Entries = list
	data.MaxEntries = types.Int64Value(int64(maxEntries))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
