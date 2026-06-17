// Package ipmi abstracts BMC access behind a BMCClient interface so
// resources can be unit-tested with a mock and the real implementation
// can swap (today: ipmitool subprocess; future: pure-Go lib, RMCP+).
//
// Design rationale (recorded in plans/poc-4-wrap-result.md): ipmitool
// is the de-facto reference IPMI implementation with 20+ years of
// vendor-quirk handling, and the production-proven choice in
// bmc-toolbox/bmclib for the same use case.
package ipmi

import (
	"context"
	"sync"
)

// ConnectionParams are the per-resource (or provider-default) settings
// needed to talk to a single BMC. Resources merge their per-resource
// overrides onto the provider defaults via Merge.
//
// Port and CipherSuite are pointers so a per-resource override of zero
// is distinguishable from "not set" — required for cipher_suite = 0
// (RMCP+ no-auth) to actually override a non-zero provider default.
type ConnectionParams struct {
	Host        string
	Username    string
	Password    string
	Port        *int
	Interface   string // "lanplus" / "lan" / "open"
	CipherSuite *int
	TimeoutSecs int
}

// IntPtr returns a pointer to v. Helper for constructing ConnectionParams
// with literal int values (mostly used by tests and by provider Configure).
func IntPtr(v int) *int { return &v }

// Merge applies override on top of base. Empty-string and nil-pointer
// values in override are treated as "not set" — base wins for them.
func (base ConnectionParams) Merge(override ConnectionParams) ConnectionParams {
	out := base
	if override.Host != "" {
		out.Host = override.Host
	}
	if override.Username != "" {
		out.Username = override.Username
	}
	if override.Password != "" {
		out.Password = override.Password
	}
	if override.Port != nil {
		out.Port = override.Port
	}
	if override.Interface != "" {
		out.Interface = override.Interface
	}
	if override.CipherSuite != nil {
		out.CipherSuite = override.CipherSuite
	}
	if override.TimeoutSecs != 0 {
		out.TimeoutSecs = override.TimeoutSecs
	}
	return out
}

// ChassisStatus is the parsed view of `ipmitool chassis status`.
// We only expose fields we expect Tier 1 data sources to consume —
// more get added as we wire up the relevant resources.
type ChassisStatus struct {
	PowerOn          bool
	PowerOverload    bool
	PowerFault       bool
	ChassisIntrusion bool
}

// BMCInfo is the parsed view of `ipmitool mc info` — BMC firmware
// identification + capabilities.
type BMCInfo struct {
	DeviceID         int
	DeviceRevision   int
	FirmwareVersion  string
	IPMIVersion      string
	ManufacturerID   int
	ManufacturerName string
	ProductID        int
	ProductName      string
	DeviceAvailable  bool
}

// FRU is the parsed view of `ipmitool fru print 0` — built-in FRU
// inventory data (board / chassis / product manufacturer + serial).
type FRU struct {
	DeviceID          int
	DeviceDescription string
	ChassisType       string
	ChassisSerial     string
	ChassisPartNumber string
	BoardMfg          string
	BoardProduct      string
	BoardSerial       string
	BoardPartNumber   string
	ProductMfg        string
	ProductName       string
	ProductSerial     string
	ProductPartNumber string
}

// SELEntry is one record from `ipmitool sel elist`.
type SELEntry struct {
	RecordID    string
	Timestamp   string
	Sensor      string
	EventType   string
	Direction   string
	Description string
}

// Sensor is one row from `ipmitool sdr list`.
type Sensor struct {
	Name    string
	Reading string // numeric value as string ("30") or raw hex ("0x01")
	Unit    string // "degrees C", "RPM", "" if no unit
	Status  string // "ok", "ns" (not specified), etc.
}

// PowerState is the declarative state ipmi_power supports.
// Only steady states ("on", "off") are first-class; "cycle"/"reset" are
// imperative actions, deferred to v0.2's ipmi_power_action resource.
type PowerState string

const (
	PowerOn  PowerState = "on"
	PowerOff PowerState = "off"
)

// BootDevice is the declarative boot-device override.
type BootDevice string

const (
	BootDeviceNone   BootDevice = "none"
	BootDevicePXE    BootDevice = "pxe"
	BootDeviceDisk   BootDevice = "disk"
	BootDeviceCDROM  BootDevice = "cdrom"
	BootDeviceBIOS   BootDevice = "bios"
	BootDeviceFloppy BootDevice = "floppy"
)

// BootFlags is the parsed view of IPMI Boot Options selector 5
// (Boot Flags). See POC 1 result doc for byte format.
type BootFlags struct {
	Valid      bool       // bit 7 of byte 1 — flags are active
	Persistent bool       // bit 6 of byte 1 — survives across reboots
	EFI        bool       // bit 5 of byte 1 — UEFI rather than legacy BIOS
	Device     BootDevice // bits 5:2 of byte 2 — selected device
}

// UserPrivilege maps to ipmitool's `user priv` privilege levels.
type UserPrivilege string

const (
	UserPrivCallback      UserPrivilege = "callback"      // 1
	UserPrivUser          UserPrivilege = "user"          // 2
	UserPrivOperator      UserPrivilege = "operator"      // 3
	UserPrivAdministrator UserPrivilege = "administrator" // 4
	UserPrivOEM           UserPrivilege = "oem"           // 5
	UserPrivNoAccess      UserPrivilege = "no_access"     // 15
)

// User is one row of `ipmitool user list <channel>`.
type User struct {
	ID        int
	Name      string
	Enabled   bool          // derived from "Empty Password" being false + presence
	Privilege UserPrivilege // mapped from the channel's priv-limit column
}

// ChannelAccessMode controls when this channel is reachable.
type ChannelAccessMode string

const (
	ChannelAccessDisabled ChannelAccessMode = "disabled"
	ChannelAccessPreBoot  ChannelAccessMode = "pre_boot"
	ChannelAccessAlways   ChannelAccessMode = "always"
	ChannelAccessShared   ChannelAccessMode = "shared"
)

// ChannelAccess is the parsed view of IPMI Get/Set Channel Access (0x41/0x40).
// See plans/poc-3-lan-result.md context (this lives on the same channel as
// the LAN config we read in POC 3).
type ChannelAccess struct {
	AccessMode     ChannelAccessMode
	UserLevelAuth  bool // false = required (auth disabled bit)
	PerMessageAuth bool
	PEFAlerting    bool
	PrivilegeLimit UserPrivilege
}

// ChannelPersistence is which view of channel access to read or write.
// Most BMCs distinguish "volatile" (active settings) from "non_volatile"
// (boot-time defaults). Writing "both" covers the typical case.
type ChannelPersistence string

const (
	PersistVolatile    ChannelPersistence = "volatile"
	PersistNonVolatile ChannelPersistence = "non_volatile"
	PersistBoth        ChannelPersistence = "both"
)

// LanIPSource maps to IPMI LAN Config Param 4 (IP Address Source).
type LanIPSource string

const (
	IPSourceUnspecified LanIPSource = "unspecified"
	IPSourceStatic      LanIPSource = "static"
	IPSourceDHCP        LanIPSource = "dhcp"
	IPSourceBIOS        LanIPSource = "bios"
	IPSourceOther       LanIPSource = "other"
)

// LanConfig is the parsed view of the IPv4 LAN parameters we care about.
// `Supported` records which parameter selectors the BMC implements —
// per POC 3, this varies across firmware families (R210 II supports 20 of
// 24 standard selectors).
type LanConfig struct {
	IPSource        LanIPSource
	IPAddress       string // "a.b.c.d"
	SubnetMask      string
	DefaultGateway  string
	BackupGateway   string
	MAC             string // "aa:bb:cc:dd:ee:ff"
	VLANID          int    // 0 = disabled
	VLANEnabled     bool
	VLANPriority    int
	PrimaryRMCPPort int
	Supported       map[uint8]bool
}

// SOLBitrate is one of the IPMI-spec-encoded SOL baud rates.
type SOLBitrate string

const (
	SOLBitrate9600   SOLBitrate = "9600"
	SOLBitrate19200  SOLBitrate = "19200"
	SOLBitrate38400  SOLBitrate = "38400"
	SOLBitrate57600  SOLBitrate = "57600"
	SOLBitrate115200 SOLBitrate = "115200"
)

// SOLConfig is the parsed view of the SOL configuration parameters we care about.
// `Supported` records which parameter selectors the BMC implements (LAN-style).
type SOLConfig struct {
	Enabled               bool
	BitrateNonVolatile    SOLBitrate
	BitrateVolatile       SOLBitrate
	PrivilegeLimit        UserPrivilege
	ForceAuthentication   bool
	ForceEncryption       bool
	Supported             map[uint8]bool
}

// SOLConfigUpdate is the partial-write view passed to ApplySOL.
// nil = leave alone.
type SOLConfigUpdate struct {
	Enabled             *bool
	Bitrate             *SOLBitrate // applied to both volatile and non-volatile
	PrivilegeLimit      *UserPrivilege
	ForceAuthentication *bool
	ForceEncryption     *bool
}

// WatchdogAction is what the BMC does when the watchdog expires.
type WatchdogAction string

const (
	WatchdogActionNone       WatchdogAction = "none"
	WatchdogActionHardReset  WatchdogAction = "hard_reset"
	WatchdogActionPowerDown  WatchdogAction = "power_down"
	WatchdogActionPowerCycle WatchdogAction = "power_cycle"
)

// WatchdogConfig is the parsed view of IPMI Get/Set Watchdog Timer
// (cmd 0x22 / 0x24).
type WatchdogConfig struct {
	Stopped        bool           // true if timer is stopped
	LogEvent       bool           // log to SEL on expiry
	Action         WatchdogAction // what happens at timeout
	TimeoutSeconds int            // total countdown in seconds (lib converts 100ms units)
	Running        bool           // (read-only) true if timer is currently counting
}

// LanConfigUpdate is the partial-write view passed to ApplyLanConfig.
// Pointer fields are write-only: nil means "leave this parameter alone".
// This matches Terraform Optional+Computed semantics — null in the plan
// → don't touch.
type LanConfigUpdate struct {
	IPSource        *LanIPSource
	IPAddress       *string
	SubnetMask      *string
	DefaultGateway  *string
	BackupGateway   *string
	VLANID          *int  // negative or zero = disable VLAN
	VLANPriority    *int
	PrimaryRMCPPort *int
}

// BMCClient is the abstraction every resource and data source calls.
// New methods are added incrementally as resources land in Tier 1.
type BMCClient interface {
	GetChassisStatus(ctx context.Context) (*ChassisStatus, error)
	GetBMCInfo(ctx context.Context) (*BMCInfo, error)
	GetFRU(ctx context.Context, deviceID int) (*FRU, error)
	GetSEL(ctx context.Context, maxEntries int) ([]SELEntry, error)
	GetSensors(ctx context.Context) ([]Sensor, error)
	SetPowerState(ctx context.Context, state PowerState) error
	SetBootDevice(ctx context.Context, device BootDevice, persistent, efi bool) error
	GetBootFlags(ctx context.Context) (*BootFlags, error)
	GetUsers(ctx context.Context, channel uint8) ([]User, error)
	SetUserName(ctx context.Context, userID uint8, name string) error
	SetUserPassword(ctx context.Context, userID uint8, password string) error
	SetUserPrivilege(ctx context.Context, userID, channel uint8, priv UserPrivilege) error
	EnableUser(ctx context.Context, userID uint8) error
	DisableUser(ctx context.Context, userID uint8) error
	GetChannelAccess(ctx context.Context, channel uint8, persist ChannelPersistence) (*ChannelAccess, error)
	SetChannelAccess(ctx context.Context, channel uint8, access ChannelAccess, persist ChannelPersistence) error
	GetLanConfig(ctx context.Context, channel uint8) (*LanConfig, error)
	ApplyLanConfig(ctx context.Context, channel uint8, update LanConfigUpdate) error
	GetWatchdog(ctx context.Context) (*WatchdogConfig, error)
	SetWatchdog(ctx context.Context, cfg WatchdogConfig) error
	ResetWatchdog(ctx context.Context) error
	ChassisIdentify(ctx context.Context, durationSeconds int, indefinite bool) error
	GetSOL(ctx context.Context, channel uint8) (*SOLConfig, error)
	ApplySOL(ctx context.Context, channel uint8, update SOLConfigUpdate) error
}

// ClientFactory is what provider Configure stashes on the request.
// Resources call New(override) at the start of each CRUD method to
// get a BMCClient configured with the merged params.
//
// The MockFn field is the unit-test injection point: tests construct a
// ClientFactory{MockFn: func(p) BMCClient { return &MockBMCClient{...} }}
// and pass it to the resource directly.
//
// MaxConcurrentPerHost caps how many ipmitool subprocesses can be in
// flight against a single BMC at once. Defaults to 3 — sized for the
// iDRAC6 session table (the empirical small-fleet failure mode Devon
// flagged). Raise to 8 for iDRAC7+, 16+ for SuperMicro X10+ /
// AsRock Rack. Zero or negative falls back to 3.
type ClientFactory struct {
	IpmitoolPath         string
	Defaults             ConnectionParams
	MaxConcurrentPerHost int
	MockFn               func(ConnectionParams) BMCClient

	semaphores sync.Map // host string -> chan struct{} (capacity = MaxConcurrentPerHost)
}

// acquire blocks until a slot is available for the given host, then
// returns the semaphore channel. Callers must call release(sem).
// The semaphore is keyed by the merged host string, so per-resource
// connection-override hosts share a slot pool with the provider-block
// host if (and only if) they are byte-identical strings.
func (f *ClientFactory) acquire(host string) chan struct{} {
	cap := f.MaxConcurrentPerHost
	if cap <= 0 {
		cap = 3
	}
	sem, _ := f.semaphores.LoadOrStore(host, make(chan struct{}, cap))
	c := sem.(chan struct{})
	c <- struct{}{}
	return c
}

func (f *ClientFactory) release(sem chan struct{}) {
	<-sem
}

func (f *ClientFactory) New(override ConnectionParams) BMCClient {
	params := f.Defaults.Merge(override)
	if f.MockFn != nil {
		return f.MockFn(params)
	}
	return newIpmitoolClient(f.IpmitoolPath, params, f)
}
