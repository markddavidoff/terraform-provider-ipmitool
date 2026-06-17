// Package provider wires up the Terraform Plugin Framework provider type,
// schema, and Configure step. Resource and data-source registration
// happens here too once the first Tier 1 resource lands.
package provider

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/ipmi"
)

// New is the factory the plugin server calls.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ipmiProvider{version: version}
	}
}

type ipmiProvider struct {
	version string
}

// providerConfigModel mirrors the provider block schema for tfsdk decode.
type providerConfigModel struct {
	Host                      types.String `tfsdk:"host"`
	Username                  types.String `tfsdk:"username"`
	Password                  types.String `tfsdk:"password"`
	Port                      types.Int64  `tfsdk:"port"`
	Interface                 types.String `tfsdk:"interface"`
	CipherSuite               types.Int64  `tfsdk:"cipher_suite"`
	TimeoutSeconds            types.Int64  `tfsdk:"timeout_seconds"`
	AllowUnauthenticated      types.Bool   `tfsdk:"allow_unauthenticated"`
	MaxConcurrentCallsPerHost types.Int64  `tfsdk:"max_concurrent_calls_per_host"`
	HealthCheck               types.Bool   `tfsdk:"health_check"`
}

func (p *ipmiProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "ipmi"
	resp.Version = p.version
}

func (p *ipmiProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage BMC/iDRAC hardware via IPMI 2.0 (LAN+) by wrapping the ipmitool CLI.\n\n" +
			"Resources can override any of these fields per-host for multi-BMC fleets.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional:    true,
				Description: "Default BMC IP or hostname. Resources may override.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "Default IPMI username.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Default IPMI password.",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "Default IPMI UDP port. Defaults to 623.",
			},
			"interface": schema.StringAttribute{
				Optional:    true,
				Description: "ipmitool interface: lanplus (default), lan, or open.",
			},
			"cipher_suite": schema.Int64Attribute{
				Required: true,
				Description: "RMCP+ cipher suite ID. **Required** — no safe " +
					"default. Common values:\n" +
					"  - `3`: RAKP-HMAC-SHA1 + AES-CBC-128. Legacy Dell 11G, " +
					"iDRAC6.\n" +
					"  - `17`: RAKP-HMAC-SHA256 + AES-CBC-128. iDRAC7+, " +
					"SuperMicro X10+, AsRock Rack, modern hardware.\n" +
					"  - `0`: no auth, no integrity. Requires " +
					"`allow_unauthenticated = true`.\n\n" +
					"Run `make detect-cipher HOST=... USER=...` against a BMC " +
					"to probe (warning: each failed probe is one failed auth " +
					"attempt; iDRAC default lockout = 3 strikes).",
			},
			"timeout_seconds": schema.Int64Attribute{
				Optional: true,
				Description: "Per-call timeout for ipmitool subprocess. Defaults to 60s — " +
					"older BMCs are slow on `sdr list` (full SDR iteration).",
			},
			"allow_unauthenticated": schema.BoolAttribute{
				Optional: true,
				Description: "Opt-in confirmation that cipher_suite = 0 " +
					"(no RMCP+ authentication or integrity) is intentional. " +
					"Setting cipher_suite = 0 without this flag is a Configure-" +
					"time error. Never set this for production.",
			},
			"max_concurrent_calls_per_host": schema.Int64Attribute{
				Optional: true,
				Description: "Maximum concurrent ipmitool subprocesses per " +
					"host. Defaults to 3 (safe for iDRAC6 session table). " +
					"Raise for modern BMCs: 8 for iDRAC7+, 16+ for " +
					"SuperMicro X10+ / AsRock Rack.",
			},
			"health_check": schema.BoolAttribute{
				Optional: true,
				Description: "When true, run `mc info` at Configure time " +
					"using the provider-block defaults to fail fast on " +
					"unreachable hosts or bad credentials. " +
					"**Per-resource connection overrides are NOT probed** — " +
					"those still fail at apply time. Defaults to false " +
					"(zero network calls at Configure).",
			},
		},
	}
}

// Configure detects ipmitool in PATH and stashes a ClientFactory for
// resources / data sources to consume.
func (p *ipmiProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	binaryPath, err := exec.LookPath("ipmitool")
	if err != nil {
		resp.Diagnostics.AddError(
			"ipmitool not found in PATH",
			"This provider wraps the ipmitool CLI. Install it:\n"+
				"  macOS:   brew install ipmitool\n"+
				"  Debian:  apt install ipmitool\n"+
				"  RHEL:    dnf install ipmitool\n"+
				"  Alpine:  apk add ipmitool\n"+
				"  Windows: install WSL2 then apt install ipmitool inside WSL2",
		)
		return
	}

	var data providerConfigModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	iface := "lanplus"
	if !data.Interface.IsNull() && !data.Interface.IsUnknown() {
		iface = data.Interface.ValueString()
		switch iface {
		case "lanplus", "lan", "open":
		default:
			resp.Diagnostics.AddAttributeError(
				path.Root("interface"),
				"invalid interface",
				fmt.Sprintf("got %q; want one of lanplus, lan, open", iface),
			)
			return
		}
	}

	// cipher_suite is Required (schema-enforced) but verify the value
	// satisfies the cipher=0 opt-in invariant.
	cipher := int(data.CipherSuite.ValueInt64())
	if cipher == 0 && !data.AllowUnauthenticated.ValueBool() {
		resp.Diagnostics.AddAttributeError(
			path.Root("cipher_suite"),
			"cipher_suite = 0 requires explicit opt-in",
			"Cipher suite 0 disables RMCP+ authentication and integrity. "+
				"Set allow_unauthenticated = true on the provider block to "+
				"confirm this is intentional. Never do this in production.",
		)
		return
	}

	factory := &ipmi.ClientFactory{
		IpmitoolPath:         binaryPath,
		MaxConcurrentPerHost: intOr(data.MaxConcurrentCallsPerHost, 3),
		Defaults: ipmi.ConnectionParams{
			Host:        data.Host.ValueString(),
			Username:    data.Username.ValueString(),
			Password:    data.Password.ValueString(),
			Port:        ipmi.IntPtr(intOr(data.Port, 623)),
			Interface:   iface,
			CipherSuite: ipmi.IntPtr(cipher),
			TimeoutSecs: intOr(data.TimeoutSeconds, 60),
		},
	}

	// Optional health probe against the provider-block defaults. Skipped
	// silently if host is empty (caller intends per-resource overrides for
	// everything) or if health_check is unset.
	if data.HealthCheck.ValueBool() && factory.Defaults.Host != "" {
		probe := factory.New(ipmi.ConnectionParams{})
		if _, err := probe.GetBMCInfo(ctx); err != nil {
			resp.Diagnostics.AddError(
				"ipmi: health check failed",
				"Configure-time `mc info` probe failed against "+
					factory.Defaults.Host+": "+err.Error()+". Verify host, "+
					"port, username, password, cipher_suite, and network "+
					"reachability. Set health_check = false to skip this probe.",
			)
			return
		}
	}

	resp.DataSourceData = factory
	resp.ResourceData = factory
}

// Resources / DataSources are empty until the first Tier 1 resource lands.
func (p *ipmiProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPowerResource,
		NewBootDeviceResource,
		NewUserResource,
		NewChannelAccessResource,
		NewLanResource,
		NewWatchdogResource,
		NewChassisIdentifyResource,
		NewSOLResource,
	}
}

func (p *ipmiProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewChassisStatusDataSource,
		NewBMCInfoDataSource,
		NewFRUDataSource,
		NewSELDataSource,
		NewSensorsDataSource,
	}
}

func intOr(v types.Int64, def int) int {
	if v.IsNull() || v.IsUnknown() {
		return def
	}
	return int(v.ValueInt64())
}

// optionalIntPtr converts a framework Int64 attribute into *int.
// Returns nil when the attribute is Null or Unknown — distinguishes
// "user didn't set this" from "user explicitly set to zero".
func optionalIntPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := int(v.ValueInt64())
	return &out
}
