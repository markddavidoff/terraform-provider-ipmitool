package ipmi

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ipmitoolClient implements BMCClient by subprocess-spawning ipmitool
// per call. Each invocation is self-contained: open session → run cmd →
// exit. No long-lived sessions, no keepalives — sidesteps the POC 1
// session-lifecycle problem.
type ipmitoolClient struct {
	binary string
	params ConnectionParams
}

func newIpmitoolClient(binary string, params ConnectionParams) *ipmitoolClient {
	return &ipmitoolClient{binary: binary, params: params}
}

// run executes ipmitool with the configured connection args and the
// given command tail. Returns stdout. Stderr is folded into the error
// when the exit code is non-zero.
//
// Retries up to 3 times on transient BMC errors:
//   - "insufficient resources for session" (iDRAC6 session table full when
//     many parallel TF data sources hit one BMC)
//   - "Unable to establish IPMI v2 / RMCP+ session" (transient handshake
//     race during BMC busy periods)
func (c *ipmitoolClient) run(ctx context.Context, args ...string) (string, error) {
	timeout := time.Duration(c.params.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	common := []string{
		"-I", c.params.Interface,
		"-C", strconv.Itoa(c.params.CipherSuite),
		"-H", c.params.Host,
		"-p", strconv.Itoa(c.params.Port),
		"-U", c.params.Username,
		"-P", c.params.Password,
	}
	full := append(common, args...)

	const maxAttempts = 3
	backoff := 500 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx2, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(ctx2, c.binary, full...)
		out, err := cmd.Output()
		cancel()
		if err == nil {
			return string(out), nil
		}

		ee, ok := err.(*exec.ExitError)
		if !ok {
			return string(out), fmt.Errorf("ipmitool %v: %w", args, err)
		}
		stderr := strings.TrimSpace(string(ee.Stderr))
		lastErr = fmt.Errorf("ipmitool %v: exit %d: %s", args, ee.ExitCode(), stderr)

		if !isTransientBMCError(stderr) || attempt == maxAttempts {
			return string(out), lastErr
		}

		select {
		case <-ctx.Done():
			return string(out), lastErr
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return "", lastErr
}

// isTransientBMCError matches stderr text that indicates a retryable
// BMC-side condition (session table contention, transient handshake).
func isTransientBMCError(stderr string) bool {
	switch {
	case strings.Contains(stderr, "insufficient resources for session"):
		return true
	case strings.Contains(stderr, "Unable to establish IPMI v2 / RMCP+ session"):
		return true
	}
	return false
}

func (c *ipmitoolClient) GetChassisStatus(ctx context.Context) (*ChassisStatus, error) {
	out, err := c.run(ctx, "chassis", "status")
	if err != nil {
		return nil, err
	}
	var st ChassisStatus
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		k, v := splitKV(s.Text())
		switch k {
		case "System Power":
			st.PowerOn = v == "on"
		case "Power Overload":
			st.PowerOverload = v == "true"
		case "Main Power Fault":
			st.PowerFault = v == "true"
		case "Chassis Intrusion":
			st.ChassisIntrusion = v == "active"
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("parsing chassis status: %w", err)
	}
	return &st, nil
}

// splitKV parses an ipmitool "Key : Value" line. Returns empty strings
// if no colon is found.
func splitKV(line string) (key, value string) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (c *ipmitoolClient) GetBMCInfo(ctx context.Context) (*BMCInfo, error) {
	out, err := c.run(ctx, "mc", "info")
	if err != nil {
		return nil, err
	}
	var info BMCInfo
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		k, v := splitKV(s.Text())
		switch k {
		case "Device ID":
			info.DeviceID, _ = strconv.Atoi(v)
		case "Device Revision":
			info.DeviceRevision, _ = strconv.Atoi(v)
		case "Firmware Revision":
			info.FirmwareVersion = v
		case "IPMI Version":
			info.IPMIVersion = v
		case "Manufacturer ID":
			info.ManufacturerID, _ = strconv.Atoi(v)
		case "Manufacturer Name":
			info.ManufacturerName = v
		case "Product ID":
			// "256 (0x0100)" — keep the decimal part.
			info.ProductID, _ = strconv.Atoi(strings.SplitN(v, " ", 2)[0])
		case "Product Name":
			info.ProductName = v
		case "Device Available":
			info.DeviceAvailable = v == "yes"
		}
	}
	return &info, nil
}

func (c *ipmitoolClient) GetFRU(ctx context.Context, deviceID int) (*FRU, error) {
	out, err := c.run(ctx, "fru", "print", strconv.Itoa(deviceID))
	if err != nil {
		return nil, err
	}
	fru := &FRU{DeviceID: deviceID}
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		k, v := splitKV(s.Text())
		switch k {
		case "FRU Device Description":
			fru.DeviceDescription = v
		case "Chassis Type":
			fru.ChassisType = v
		case "Chassis Serial":
			fru.ChassisSerial = v
		case "Chassis Part Number":
			fru.ChassisPartNumber = v
		case "Board Mfg":
			fru.BoardMfg = v
		case "Board Product":
			fru.BoardProduct = v
		case "Board Serial":
			fru.BoardSerial = v
		case "Board Part Number":
			fru.BoardPartNumber = v
		case "Product Manufacturer":
			fru.ProductMfg = v
		case "Product Name":
			fru.ProductName = v
		case "Product Serial":
			fru.ProductSerial = v
		case "Product Part Number":
			fru.ProductPartNumber = v
		}
	}
	return fru, nil
}

// GetSensors parses `ipmitool sdr list` — three pipe-separated fields:
// name | "<reading> <unit>" or "<hex>" | status. We split reading and
// unit by the first space if the reading looks like "<number> <unit>".
func (c *ipmitoolClient) GetSensors(ctx context.Context) ([]Sensor, error) {
	out, err := c.run(ctx, "sdr", "list")
	if err != nil {
		return nil, err
	}
	var sensors []Sensor
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		raw := strings.TrimSpace(parts[1])
		status := strings.TrimSpace(parts[2])

		reading, unit := raw, ""
		if i := strings.Index(raw, " "); i > 0 {
			reading = raw[:i]
			unit = strings.TrimSpace(raw[i+1:])
		}
		sensors = append(sensors, Sensor{
			Name:    name,
			Reading: reading,
			Unit:    unit,
			Status:  status,
		})
	}
	return sensors, nil
}

// SetBootDevice runs `ipmitool chassis bootdev <device> [options=<csv>]`.
// On Dell 11G BMCs, BIOS auto-clears one-shot (persistent=false) flags
// after consuming them on the next boot — this is per-IPMI 2.0 spec.
// See plans/poc-1-bootdev-result.md.
func (c *ipmitoolClient) SetBootDevice(ctx context.Context, device BootDevice, persistent, efi bool) error {
	switch device {
	case BootDeviceNone, BootDevicePXE, BootDeviceDisk, BootDeviceCDROM, BootDeviceBIOS, BootDeviceFloppy:
	default:
		return fmt.Errorf("unsupported boot device %q", device)
	}
	args := []string{"chassis", "bootdev", string(device)}
	var opts []string
	if persistent {
		opts = append(opts, "persistent")
	}
	if efi {
		opts = append(opts, "efiboot")
	}
	if len(opts) > 0 {
		args = append(args, "options="+strings.Join(opts, ","))
	}
	_, err := c.run(ctx, args...)
	return err
}

// GetBootFlags reads Boot Options selector 5 via raw and decodes the
// byte format from the IPMI 2.0 spec §28.13.
//
// Byte layout (after the leading param-version byte):
//   byte 1: bit7=Valid, bit6=Persistent, bit5=EFI
//   byte 2: bits 5..2 = device selector
func (c *ipmitoolClient) GetBootFlags(ctx context.Context) (*BootFlags, error) {
	out, err := c.run(ctx, "raw", "0x00", "0x09", "0x05", "0x00", "0x00")
	if err != nil {
		return nil, err
	}
	bytes, err := parseHexBytes(out)
	if err != nil {
		return nil, err
	}
	return decodeBootFlags(bytes), nil
}

func decodeBootFlags(bytes []byte) *BootFlags {
	bf := &BootFlags{Device: BootDeviceNone}
	if len(bytes) < 3 {
		return bf
	}
	bf.Valid = bytes[1]&0x80 != 0
	bf.Persistent = bytes[1]&0x40 != 0
	bf.EFI = bytes[1]&0x20 != 0
	switch (bytes[2] >> 2) & 0x0F {
	case 0:
		bf.Device = BootDeviceNone
	case 1:
		bf.Device = BootDevicePXE
	case 2:
		bf.Device = BootDeviceDisk
	case 5:
		bf.Device = BootDeviceCDROM
	case 6:
		bf.Device = BootDeviceBIOS
	case 0x0f:
		bf.Device = BootDeviceFloppy
	}
	return bf
}

// parseHexBytes converts ipmitool `raw` output (space-separated hex bytes,
// possibly wrapped across lines) into a []byte.
func parseHexBytes(out string) ([]byte, error) {
	var bs []byte
	for _, tok := range strings.Fields(out) {
		v, err := strconv.ParseUint(tok, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("bad hex token %q in %q", tok, out)
		}
		bs = append(bs, byte(v))
	}
	return bs, nil
}

// SetPowerState runs `ipmitool chassis power on|off`. ipmitool reports
// idempotent success when the chassis is already in the requested state,
// so this is safe to call unconditionally from Create/Update.
func (c *ipmitoolClient) SetPowerState(ctx context.Context, state PowerState) error {
	var verb string
	switch state {
	case PowerOn:
		verb = "on"
	case PowerOff:
		verb = "off"
	default:
		return fmt.Errorf("unsupported power state %q", state)
	}
	_, err := c.run(ctx, "chassis", "power", verb)
	return err
}

// ──────────── Watchdog ────────────

// GetWatchdog reads via raw cmd 0x06 0x25.
//
// Response bytes (per IPMI 2.0 §27.7):
//   byte 0: Timer Use — bit 6 = stopped (when 1), bit 7 = log
//   byte 1: Timer Actions — bits 2:0 timeout action
//   byte 2: Pre-Timeout Interval seconds
//   byte 3: Timer Use Expiration flags
//   byte 4: Initial Countdown LSB (100ms units)
//   byte 5: Initial Countdown MSB
//   byte 6: Present Countdown LSB
//   byte 7: Present Countdown MSB
func (c *ipmitoolClient) GetWatchdog(ctx context.Context) (*WatchdogConfig, error) {
	out, err := c.run(ctx, "raw", "0x06", "0x25")
	if err != nil {
		return nil, err
	}
	bytes, err := parseHexBytes(out)
	if err != nil {
		return nil, err
	}
	return decodeWatchdog(bytes), nil
}

func decodeWatchdog(bytes []byte) *WatchdogConfig {
	cfg := &WatchdogConfig{Action: WatchdogActionNone}
	if len(bytes) < 8 {
		return cfg
	}
	cfg.Stopped = bytes[0]&0x40 != 0
	cfg.LogEvent = bytes[0]&0x80 == 0 // bit 7 = "Don't Log" — inverted
	switch bytes[1] & 0x07 {
	case 0:
		cfg.Action = WatchdogActionNone
	case 1:
		cfg.Action = WatchdogActionHardReset
	case 2:
		cfg.Action = WatchdogActionPowerDown
	case 3:
		cfg.Action = WatchdogActionPowerCycle
	}
	countdown100ms := int(bytes[4]) | int(bytes[5])<<8
	cfg.TimeoutSeconds = countdown100ms / 10
	present100ms := int(bytes[6]) | int(bytes[7])<<8
	cfg.Running = present100ms > 0 && !cfg.Stopped
	return cfg
}

// SetWatchdog writes via raw cmd 0x06 0x24.
func (c *ipmitoolClient) SetWatchdog(ctx context.Context, cfg WatchdogConfig) error {
	// Timer use byte: bits 2:0 = use type (4 = SMS/OS), bit 6 = don't stop
	// (we use bit 6 set = stopped per spec), bit 7 = don't log.
	use := byte(0x04) // SMS/OS
	if cfg.Stopped {
		use |= 0x40
	}
	if !cfg.LogEvent {
		use |= 0x80
	}
	actions := byte(0)
	switch cfg.Action {
	case WatchdogActionHardReset:
		actions = 1
	case WatchdogActionPowerDown:
		actions = 2
	case WatchdogActionPowerCycle:
		actions = 3
	}
	if cfg.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must be >= 0")
	}
	if cfg.TimeoutSeconds > 6553 {
		return fmt.Errorf("timeout_seconds exceeds IPMI max (~6553s)")
	}
	cd := cfg.TimeoutSeconds * 10 // to 100ms units
	cdLo := byte(cd & 0xFF)
	cdHi := byte((cd >> 8) & 0xFF)

	_, err := c.run(ctx, "raw", "0x06", "0x24",
		fmt.Sprintf("0x%02x", use),
		fmt.Sprintf("0x%02x", actions),
		"0x00", // pre-timeout interval (none for v0.1)
		"0x00", // timer-use expiration flags clear (none)
		fmt.Sprintf("0x%02x", cdLo),
		fmt.Sprintf("0x%02x", cdHi),
	)
	return err
}

// ResetWatchdog runs cmd 0x06 0x22 — starts/restarts the timer with the
// previously configured countdown.
func (c *ipmitoolClient) ResetWatchdog(ctx context.Context) error {
	_, err := c.run(ctx, "raw", "0x06", "0x22")
	return err
}

// ──────────── SOL config ────────────
//
// Set/Get SOL Configuration Parameters are NetFn 0x0c (Transport),
// commands 0x21 / 0x22. Selectors of interest:
//   1: SOL Enable (1 byte, bit 0)
//   2: SOL Auth (1 byte, bit 7=encryption, bit 6=auth, bits 3:0=priv)
//   5: Non-Volatile Bit Rate (1 byte, bits 3:0)
//   6: Volatile Bit Rate (1 byte, bits 3:0)

const (
	solParamSetInProgress = 0
	solParamEnable        = 1
	solParamAuth          = 2
	solParamBitrateNV     = 5
	solParamBitrateVol    = 6
)

func (c *ipmitoolClient) GetSOL(ctx context.Context, channel uint8) (*SOLConfig, error) {
	cfg := &SOLConfig{Supported: make(map[uint8]bool)}

	read := func(sel uint8) ([]byte, bool) {
		out, err := c.run(ctx, "raw", "0x0c", "0x22",
			fmt.Sprintf("0x%02x", channel),
			fmt.Sprintf("0x%02x", sel),
			"0x00", "0x00",
		)
		if err != nil {
			return nil, false
		}
		bytes, perr := parseHexBytes(out)
		if perr != nil {
			return nil, false
		}
		cfg.Supported[sel] = true
		if len(bytes) > 1 {
			return bytes[1:], true
		}
		return nil, true
	}

	if d, ok := read(solParamEnable); ok && len(d) >= 1 {
		cfg.Enabled = d[0]&0x01 != 0
	}
	if d, ok := read(solParamAuth); ok && len(d) >= 1 {
		cfg.ForceEncryption = d[0]&0x80 != 0
		cfg.ForceAuthentication = d[0]&0x40 != 0
		cfg.PrivilegeLimit = decodePrivLevel(d[0] & 0x0F)
	}
	if d, ok := read(solParamBitrateNV); ok && len(d) >= 1 {
		cfg.BitrateNonVolatile = decodeSOLBitrate(d[0])
	}
	if d, ok := read(solParamBitrateVol); ok && len(d) >= 1 {
		cfg.BitrateVolatile = decodeSOLBitrate(d[0])
	}
	return cfg, nil
}

// ApplySOL writes only non-nil fields. Bitrate is written to both
// volatile and non-volatile selectors (most users want them in sync).
func (c *ipmitoolClient) ApplySOL(ctx context.Context, channel uint8, u SOLConfigUpdate) error {
	write := func(sel uint8, data []byte) error {
		args := []string{"raw", "0x0c", "0x21",
			fmt.Sprintf("0x%02x", channel),
			fmt.Sprintf("0x%02x", sel),
		}
		for _, b := range data {
			args = append(args, fmt.Sprintf("0x%02x", b))
		}
		_, err := c.run(ctx, args...)
		return err
	}
	if u.Enabled != nil {
		var b byte
		if *u.Enabled {
			b = 0x01
		}
		if err := write(solParamEnable, []byte{b}); err != nil {
			return fmt.Errorf("set SOL enable: %w", err)
		}
	}
	if u.PrivilegeLimit != nil || u.ForceAuthentication != nil || u.ForceEncryption != nil {
		// Need to combine into one byte. If only some fields are set, the
		// rest default to enabled/required (the safer defaults).
		var b byte
		priv := UserPrivUser
		if u.PrivilegeLimit != nil {
			priv = *u.PrivilegeLimit
		}
		b |= encodeChannelPriv(priv) & 0x0F
		forceAuth := true
		if u.ForceAuthentication != nil {
			forceAuth = *u.ForceAuthentication
		}
		if forceAuth {
			b |= 0x40
		}
		forceEnc := true
		if u.ForceEncryption != nil {
			forceEnc = *u.ForceEncryption
		}
		if forceEnc {
			b |= 0x80
		}
		if err := write(solParamAuth, []byte{b}); err != nil {
			return fmt.Errorf("set SOL auth: %w", err)
		}
	}
	if u.Bitrate != nil {
		b, ok := encodeSOLBitrate(*u.Bitrate)
		if !ok {
			return fmt.Errorf("invalid SOL bitrate %q", *u.Bitrate)
		}
		if err := write(solParamBitrateNV, []byte{b}); err != nil {
			return fmt.Errorf("set SOL bitrate (nv): %w", err)
		}
		if err := write(solParamBitrateVol, []byte{b}); err != nil {
			return fmt.Errorf("set SOL bitrate (vol): %w", err)
		}
	}
	return nil
}

func encodeSOLBitrate(b SOLBitrate) (byte, bool) {
	switch b {
	case SOLBitrate9600:
		return 6, true
	case SOLBitrate19200:
		return 7, true
	case SOLBitrate38400:
		return 8, true
	case SOLBitrate57600:
		return 9, true
	case SOLBitrate115200:
		return 10, true
	}
	return 0, false
}

func decodeSOLBitrate(b byte) SOLBitrate {
	switch b & 0x0F {
	case 6:
		return SOLBitrate9600
	case 7:
		return SOLBitrate19200
	case 8:
		return SOLBitrate38400
	case 9:
		return SOLBitrate57600
	case 10:
		return SOLBitrate115200
	}
	return ""
}

func decodePrivLevel(b byte) UserPrivilege {
	switch b {
	case 1:
		return UserPrivCallback
	case 2:
		return UserPrivUser
	case 3:
		return UserPrivOperator
	case 4:
		return UserPrivAdministrator
	case 5:
		return UserPrivOEM
	}
	return ""
}

// ──────────── Chassis identify ────────────

// ChassisIdentify runs cmd 0x00 0x04. duration=0 turns off; indefinite
// overrides the duration.
func (c *ipmitoolClient) ChassisIdentify(ctx context.Context, duration int, indefinite bool) error {
	if duration < 0 || duration > 255 {
		return fmt.Errorf("duration must be 0..255 seconds")
	}
	args := []string{"raw", "0x00", "0x04", fmt.Sprintf("0x%02x", duration)}
	if indefinite {
		args = append(args, "0x01")
	}
	_, err := c.run(ctx, args...)
	return err
}

// ──────────── LAN config ────────────

// Standard IPv4 LAN parameter selectors (IPMI 2.0 §28.13 table 28-13).
// Selectors above 26 and IPv6 selectors (50+) are intentionally omitted
// — POC 3 showed they're either unsupported or out of v0.1 scope.
const (
	lanParamSetInProgress    = 0
	lanParamAuthTypeSupport  = 1
	lanParamAuthTypeEnables  = 2
	lanParamIP               = 3
	lanParamIPSource         = 4
	lanParamMAC              = 5
	lanParamSubnetMask       = 6
	lanParamPrimaryRMCPPort  = 8
	lanParamDefaultGatewayIP = 12
	lanParamBackupGatewayIP  = 14
	lanParamVLANID           = 20
	lanParamVLANPriority     = 21
)

// GetLanConfig iterates the selectors we care about with per-selector
// error tolerance: completion code 0x80 ("parameter not supported") just
// leaves the field at zero value (POC 3 finding).
func (c *ipmitoolClient) GetLanConfig(ctx context.Context, channel uint8) (*LanConfig, error) {
	cfg := &LanConfig{Supported: make(map[uint8]bool)}

	read := func(sel uint8) ([]byte, bool) {
		out, err := c.run(ctx, "raw", "0x0c", "0x02",
			fmt.Sprintf("0x%02x", channel),
			fmt.Sprintf("0x%02x", sel),
			"0x00", "0x00",
		)
		if err != nil {
			return nil, false
		}
		bytes, perr := parseHexBytes(out)
		if perr != nil {
			return nil, false
		}
		cfg.Supported[sel] = true
		// ipmitool raw output starts with the parameter revision byte;
		// our callers want the data portion only.
		if len(bytes) > 1 {
			return bytes[1:], true
		}
		return nil, true
	}

	if d, ok := read(lanParamIPSource); ok && len(d) >= 1 {
		cfg.IPSource = decodeIPSource(d[0])
	}
	if d, ok := read(lanParamIP); ok && len(d) >= 4 {
		cfg.IPAddress = formatIPv4(d[:4])
	}
	if d, ok := read(lanParamSubnetMask); ok && len(d) >= 4 {
		cfg.SubnetMask = formatIPv4(d[:4])
	}
	if d, ok := read(lanParamDefaultGatewayIP); ok && len(d) >= 4 {
		cfg.DefaultGateway = formatIPv4(d[:4])
	}
	if d, ok := read(lanParamBackupGatewayIP); ok && len(d) >= 4 {
		cfg.BackupGateway = formatIPv4(d[:4])
	}
	if d, ok := read(lanParamMAC); ok && len(d) >= 6 {
		cfg.MAC = formatMAC(d[:6])
	}
	if d, ok := read(lanParamPrimaryRMCPPort); ok && len(d) >= 2 {
		cfg.PrimaryRMCPPort = int(d[0]) | int(d[1])<<8
	}
	if d, ok := read(lanParamVLANID); ok && len(d) >= 2 {
		// byte 0 = LSB of VLAN ID; byte 1: bits 3:0 = high nibble of ID,
		// bit 7 = enable.
		cfg.VLANID = int(d[0]) | int(d[1]&0x0F)<<8
		cfg.VLANEnabled = d[1]&0x80 != 0
	}
	if d, ok := read(lanParamVLANPriority); ok && len(d) >= 1 {
		cfg.VLANPriority = int(d[0] & 0x07)
	}
	return cfg, nil
}

// ApplyLanConfig writes only the non-nil fields. Each parameter is sent
// as one Set LAN Config Param raw command. We don't issue the IPMI
// "Set In Progress" dance — most BMCs accept direct writes and the
// homelab use case doesn't need atomic multi-write.
func (c *ipmitoolClient) ApplyLanConfig(ctx context.Context, channel uint8, u LanConfigUpdate) error {
	write := func(sel uint8, data []byte) error {
		args := []string{"raw", "0x0c", "0x01",
			fmt.Sprintf("0x%02x", channel),
			fmt.Sprintf("0x%02x", sel),
		}
		for _, b := range data {
			args = append(args, fmt.Sprintf("0x%02x", b))
		}
		_, err := c.run(ctx, args...)
		return err
	}

	if u.IPSource != nil {
		if err := write(lanParamIPSource, []byte{encodeIPSource(*u.IPSource)}); err != nil {
			return fmt.Errorf("set IPSource: %w", err)
		}
	}
	if u.IPAddress != nil {
		bs, err := parseIPv4(*u.IPAddress)
		if err != nil {
			return fmt.Errorf("ip_address: %w", err)
		}
		if err := write(lanParamIP, bs); err != nil {
			return fmt.Errorf("set IP: %w", err)
		}
	}
	if u.SubnetMask != nil {
		bs, err := parseIPv4(*u.SubnetMask)
		if err != nil {
			return fmt.Errorf("subnet_mask: %w", err)
		}
		if err := write(lanParamSubnetMask, bs); err != nil {
			return fmt.Errorf("set subnet: %w", err)
		}
	}
	if u.DefaultGateway != nil {
		bs, err := parseIPv4(*u.DefaultGateway)
		if err != nil {
			return fmt.Errorf("default_gateway: %w", err)
		}
		if err := write(lanParamDefaultGatewayIP, bs); err != nil {
			return fmt.Errorf("set gateway: %w", err)
		}
	}
	if u.BackupGateway != nil {
		bs, err := parseIPv4(*u.BackupGateway)
		if err != nil {
			return fmt.Errorf("backup_gateway: %w", err)
		}
		if err := write(lanParamBackupGatewayIP, bs); err != nil {
			return fmt.Errorf("set backup gw: %w", err)
		}
	}
	if u.PrimaryRMCPPort != nil {
		p := *u.PrimaryRMCPPort
		if err := write(lanParamPrimaryRMCPPort, []byte{byte(p & 0xFF), byte((p >> 8) & 0xFF)}); err != nil {
			return fmt.Errorf("set RMCP port: %w", err)
		}
	}
	if u.VLANID != nil {
		id := *u.VLANID
		if id < 0 || id > 4095 {
			return fmt.Errorf("vlan_id out of range: %d", id)
		}
		lo := byte(id & 0xFF)
		hi := byte((id >> 8) & 0x0F)
		if id > 0 {
			hi |= 0x80 // enable bit
		}
		if err := write(lanParamVLANID, []byte{lo, hi}); err != nil {
			return fmt.Errorf("set VLAN ID: %w", err)
		}
	}
	if u.VLANPriority != nil {
		p := *u.VLANPriority
		if p < 0 || p > 7 {
			return fmt.Errorf("vlan_priority out of range: %d", p)
		}
		if err := write(lanParamVLANPriority, []byte{byte(p & 0x07)}); err != nil {
			return fmt.Errorf("set VLAN priority: %w", err)
		}
	}
	return nil
}

// ──────────── LAN config helpers ────────────

func encodeIPSource(s LanIPSource) byte {
	switch s {
	case IPSourceUnspecified:
		return 0
	case IPSourceStatic:
		return 1
	case IPSourceDHCP:
		return 2
	case IPSourceBIOS:
		return 3
	case IPSourceOther:
		return 4
	}
	return 0
}

func decodeIPSource(b byte) LanIPSource {
	switch b & 0x0F {
	case 1:
		return IPSourceStatic
	case 2:
		return IPSourceDHCP
	case 3:
		return IPSourceBIOS
	case 4:
		return IPSourceOther
	}
	return IPSourceUnspecified
}

func formatIPv4(b []byte) string {
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

func formatMAC(b []byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

func parseIPv4(s string) ([]byte, error) {
	var a, b, c, d int
	if _, err := fmt.Sscanf(s, "%d.%d.%d.%d", &a, &b, &c, &d); err != nil {
		return nil, fmt.Errorf("invalid IPv4 %q: %w", s, err)
	}
	for _, octet := range []int{a, b, c, d} {
		if octet < 0 || octet > 255 {
			return nil, fmt.Errorf("octet out of range in %q", s)
		}
	}
	return []byte{byte(a), byte(b), byte(c), byte(d)}, nil
}

// ──────────── Channel access ────────────

// GetChannelAccess reads via raw cmd 0x06 0x41.
//
// Request byte: bits 3:0 = channel, bits 7:6 = memory selector
//   0x40 (01xxxxxx) = non-volatile, 0x80 (10xxxxxx) = volatile
//
// Response bytes:
//   byte 0: settings — bit5 PEF disabled, bit4 per-msg auth disabled,
//           bit3 user-level auth disabled, bits 2:0 access mode
//   byte 1: privilege limit — bits 3:0
func (c *ipmitoolClient) GetChannelAccess(ctx context.Context, channel uint8, persist ChannelPersistence) (*ChannelAccess, error) {
	memSel := byte(0x40)
	if persist == PersistVolatile {
		memSel = 0x80
	}
	chByte := (channel & 0x0F) | memSel

	out, err := c.run(ctx, "raw", "0x06", "0x41",
		fmt.Sprintf("0x%02x", chByte))
	if err != nil {
		return nil, err
	}
	bytes, err := parseHexBytes(out)
	if err != nil {
		return nil, err
	}
	return decodeChannelAccess(bytes), nil
}

func decodeChannelAccess(bytes []byte) *ChannelAccess {
	ca := &ChannelAccess{AccessMode: ChannelAccessDisabled, PrivilegeLimit: UserPrivAdministrator}
	if len(bytes) < 2 {
		return ca
	}
	switch bytes[0] & 0x07 {
	case 0:
		ca.AccessMode = ChannelAccessDisabled
	case 1:
		ca.AccessMode = ChannelAccessPreBoot
	case 2:
		ca.AccessMode = ChannelAccessAlways
	case 3:
		ca.AccessMode = ChannelAccessShared
	}
	// The "disabled" bits are inverted — set=disabled, clear=enabled.
	ca.PEFAlerting = bytes[0]&0x20 == 0
	ca.PerMessageAuth = bytes[0]&0x10 == 0
	ca.UserLevelAuth = bytes[0]&0x08 == 0
	switch bytes[1] & 0x0F {
	case 1:
		ca.PrivilegeLimit = UserPrivCallback
	case 2:
		ca.PrivilegeLimit = UserPrivUser
	case 3:
		ca.PrivilegeLimit = UserPrivOperator
	case 4:
		ca.PrivilegeLimit = UserPrivAdministrator
	case 5:
		ca.PrivilegeLimit = UserPrivOEM
	}
	return ca
}

// SetChannelAccess writes via raw cmd 0x06 0x40. PersistBoth issues two
// writes (volatile then non-volatile).
func (c *ipmitoolClient) SetChannelAccess(ctx context.Context, channel uint8, access ChannelAccess, persist ChannelPersistence) error {
	settings := encodeChannelAccessSettings(access)
	priv := encodeChannelPriv(access.PrivilegeLimit)

	writeWith := func(memSel byte) error {
		chByte := (channel & 0x0F) | memSel
		_, err := c.run(ctx, "raw", "0x06", "0x40",
			fmt.Sprintf("0x%02x", chByte),
			fmt.Sprintf("0x%02x", settings|0x40), // bit 6 = "set in progress" for access mode
			fmt.Sprintf("0x%02x", priv|0x40),     // bit 6 = "set in progress" for priv limit
		)
		return err
	}

	switch persist {
	case PersistVolatile:
		return writeWith(0x80)
	case PersistNonVolatile:
		return writeWith(0x40)
	default: // PersistBoth
		if err := writeWith(0x80); err != nil {
			return err
		}
		return writeWith(0x40)
	}
}

func encodeChannelAccessSettings(a ChannelAccess) byte {
	var b byte
	switch a.AccessMode {
	case ChannelAccessDisabled:
		b |= 0
	case ChannelAccessPreBoot:
		b |= 1
	case ChannelAccessAlways:
		b |= 2
	case ChannelAccessShared:
		b |= 3
	}
	if !a.UserLevelAuth {
		b |= 0x08
	}
	if !a.PerMessageAuth {
		b |= 0x10
	}
	if !a.PEFAlerting {
		b |= 0x20
	}
	return b
}

func encodeChannelPriv(p UserPrivilege) byte {
	switch p {
	case UserPrivCallback:
		return 1
	case UserPrivUser:
		return 2
	case UserPrivOperator:
		return 3
	case UserPrivAdministrator:
		return 4
	case UserPrivOEM:
		return 5
	}
	return 4 // sane default
}

// ──────────── User management ────────────

func (c *ipmitoolClient) SetUserName(ctx context.Context, userID uint8, name string) error {
	_, err := c.run(ctx, "user", "set", "name", strconv.Itoa(int(userID)), name)
	return err
}

func (c *ipmitoolClient) SetUserPassword(ctx context.Context, userID uint8, password string) error {
	_, err := c.run(ctx, "user", "set", "password", strconv.Itoa(int(userID)), password)
	return err
}

func (c *ipmitoolClient) SetUserPrivilege(ctx context.Context, userID, channel uint8, priv UserPrivilege) error {
	level, ok := privilegeToLevel(priv)
	if !ok {
		return fmt.Errorf("unsupported privilege %q", priv)
	}
	_, err := c.run(ctx, "user", "priv", strconv.Itoa(int(userID)), strconv.Itoa(level), strconv.Itoa(int(channel)))
	return err
}

func (c *ipmitoolClient) EnableUser(ctx context.Context, userID uint8) error {
	_, err := c.run(ctx, "user", "enable", strconv.Itoa(int(userID)))
	return err
}

func (c *ipmitoolClient) DisableUser(ctx context.Context, userID uint8) error {
	_, err := c.run(ctx, "user", "disable", strconv.Itoa(int(userID)))
	return err
}

func privilegeToLevel(p UserPrivilege) (int, bool) {
	switch p {
	case UserPrivCallback:
		return 1, true
	case UserPrivUser:
		return 2, true
	case UserPrivOperator:
		return 3, true
	case UserPrivAdministrator:
		return 4, true
	case UserPrivOEM:
		return 5, true
	case UserPrivNoAccess:
		return 15, true
	}
	return 0, false
}

func levelToPrivilege(level string) UserPrivilege {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "CALLBACK":
		return UserPrivCallback
	case "USER":
		return UserPrivUser
	case "OPERATOR":
		return UserPrivOperator
	case "ADMINISTRATOR":
		return UserPrivAdministrator
	case "OEM":
		return UserPrivOEM
	case "NO ACCESS", "NOACCESS", "NO_ACCESS":
		return UserPrivNoAccess
	}
	return ""
}

// GetUsers parses `ipmitool user list <channel>`. ipmitool prints a
// fixed-column table; we split on >=2-space runs since column widths
// differ between BMC firmwares.
//
// Example row from R210 II:
//   2   root             true    false      true       ADMINISTRATOR
func (c *ipmitoolClient) GetUsers(ctx context.Context, channel uint8) ([]User, error) {
	out, err := c.run(ctx, "user", "list", strconv.Itoa(int(channel)))
	if err != nil {
		return nil, err
	}
	var users []User
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimRight(s.Text(), " \t")
		if line == "" {
			continue
		}
		// Skip header row.
		if strings.HasPrefix(strings.TrimSpace(line), "ID") {
			continue
		}
		fields := splitColumns(line)
		if len(fields) < 2 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		u := User{ID: id, Name: fields[1]}
		if len(fields) >= 6 {
			u.Privilege = levelToPrivilege(fields[5])
			// IPMI Msg column ("true"/"false") signals whether the slot
			// is enabled for IPMI messaging — closest proxy ipmitool gives.
			u.Enabled = strings.EqualFold(strings.TrimSpace(fields[4]), "true")
		}
		users = append(users, u)
	}
	return users, nil
}

// splitColumns splits an ipmitool table row on runs of 2+ spaces.
func splitColumns(line string) []string {
	var out []string
	inRun := false
	start := 0
	for i := 0; i <= len(line); i++ {
		atSpace := i == len(line) || line[i] == ' '
		if atSpace {
			if !inRun {
				out = append(out, line[start:i])
				inRun = true
			} else if i+1 < len(line) && line[i+1] != ' ' {
				start = i + 1
				inRun = false
			}
		}
	}
	return out
}

// GetSEL uses `sel elist last <N>` for a bounded, parseable output.
// Format: "<id> | <date> | <time> | <sensor> | <event> | <direction>"
func (c *ipmitoolClient) GetSEL(ctx context.Context, maxEntries int) ([]SELEntry, error) {
	if maxEntries <= 0 {
		maxEntries = 100
	}
	out, err := c.run(ctx, "sel", "elist", "last", strconv.Itoa(maxEntries))
	if err != nil {
		return nil, err
	}
	var entries []SELEntry
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "SEL has no entries") {
			continue
		}
		parts := strings.Split(line, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		e := SELEntry{}
		switch {
		case len(parts) >= 6:
			e.RecordID = parts[0]
			e.Timestamp = parts[1] + " " + parts[2]
			e.Sensor = parts[3]
			e.EventType = parts[4]
			e.Direction = parts[5]
			if len(parts) >= 7 {
				e.Description = parts[6]
			}
		case len(parts) >= 4:
			// shorter format on some ipmitool versions
			e.RecordID = parts[0]
			e.Timestamp = parts[1]
			e.Sensor = parts[2]
			e.Description = parts[3]
		default:
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}
