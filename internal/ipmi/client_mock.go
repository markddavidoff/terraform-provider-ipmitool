package ipmi

import (
	"context"
	"sync"
)

// MockBMCClient is a programmable test double. Tests set the response
// fields, then assert against the per-method call counters afterwards.
//
// Safe for concurrent use — all methods take the lock.
type MockBMCClient struct {
	mu sync.Mutex

	// Programmable responses.
	ChassisStatus      ChassisStatus
	ChassisError       error
	BMCInfo            BMCInfo
	BMCInfoError       error
	FRU                FRU
	FRUError           error
	SEL                []SELEntry
	SELError           error
	Sensors            []Sensor
	SensorsError       error
	SetPowerStateError error
	BootFlags          BootFlags
	BootFlagsError     error
	SetBootDeviceError error
	Users              []User
	UsersError         error
	UserOpError        error
	ChannelAccess      ChannelAccess
	ChannelAccessError error
	ChannelAccessOpErr error
	LanConfig          LanConfig
	LanConfigError     error
	ApplyLanConfigErr  error
	Watchdog           WatchdogConfig
	WatchdogError      error
	WatchdogOpErr      error
	IdentifyError      error
	SOLConfig          SOLConfig
	SOLConfigError     error
	ApplySOLErr        error

	// Call counters.
	GetChassisStatusCalls int
	GetBMCInfoCalls       int
	GetFRUCalls           int
	GetSELCalls           int
	GetSensorsCalls       int
	SetPowerStateCalls    int
	GetBootFlagsCalls     int
	SetBootDeviceCalls    int
	GetUsersCalls         int
	SetUserNameCalls      int
	SetUserPasswordCalls  int
	SetUserPrivilegeCalls int
	EnableUserCalls       int
	DisableUserCalls      int
	GetChannelAccessCalls int
	SetChannelAccessCalls int
	GetLanConfigCalls     int
	ApplyLanConfigCalls   int
	GetWatchdogCalls      int
	SetWatchdogCalls      int
	ResetWatchdogCalls    int
	ChassisIdentifyCalls  int
	GetSOLCalls           int
	ApplySOLCalls         int

	// Args recorded for the most recent call.
	LastFRUDeviceID    int
	LastSELMax         int
	LastPowerState     PowerState
	LastBootDevice     BootDevice
	LastBootPersistent bool
	LastBootEFI        bool
	LastUserChannel    uint8
	LastUserID         uint8
	LastUserName       string
	LastUserPassword   string
	LastUserPrivilege  UserPrivilege
	LastChannelAccess  ChannelAccess
	LastChannelPersist ChannelPersistence
	LastLanUpdate      LanConfigUpdate
	LastWatchdog       WatchdogConfig
	LastIdentifyDur    int
	LastIdentifyInd    bool
	LastSOLUpdate      SOLConfigUpdate
}

func (m *MockBMCClient) GetChassisStatus(_ context.Context) (*ChassisStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetChassisStatusCalls++
	if m.ChassisError != nil {
		return nil, m.ChassisError
	}
	st := m.ChassisStatus
	return &st, nil
}

func (m *MockBMCClient) GetBMCInfo(_ context.Context) (*BMCInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetBMCInfoCalls++
	if m.BMCInfoError != nil {
		return nil, m.BMCInfoError
	}
	info := m.BMCInfo
	return &info, nil
}

func (m *MockBMCClient) GetFRU(_ context.Context, deviceID int) (*FRU, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetFRUCalls++
	m.LastFRUDeviceID = deviceID
	if m.FRUError != nil {
		return nil, m.FRUError
	}
	f := m.FRU
	return &f, nil
}

func (m *MockBMCClient) GetSEL(_ context.Context, maxEntries int) ([]SELEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetSELCalls++
	m.LastSELMax = maxEntries
	if m.SELError != nil {
		return nil, m.SELError
	}
	return append([]SELEntry(nil), m.SEL...), nil
}

func (m *MockBMCClient) GetSensors(_ context.Context) ([]Sensor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetSensorsCalls++
	if m.SensorsError != nil {
		return nil, m.SensorsError
	}
	return append([]Sensor(nil), m.Sensors...), nil
}

func (m *MockBMCClient) SetPowerState(_ context.Context, state PowerState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetPowerStateCalls++
	m.LastPowerState = state
	if m.SetPowerStateError != nil {
		return m.SetPowerStateError
	}
	// Reflect the change in ChassisStatus so a subsequent GetChassisStatus
	// call in the same test sees the updated state.
	m.ChassisStatus.PowerOn = state == PowerOn
	return nil
}

func (m *MockBMCClient) SetBootDevice(_ context.Context, device BootDevice, persistent, efi bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetBootDeviceCalls++
	m.LastBootDevice = device
	m.LastBootPersistent = persistent
	m.LastBootEFI = efi
	if m.SetBootDeviceError != nil {
		return m.SetBootDeviceError
	}
	m.BootFlags = BootFlags{Valid: device != BootDeviceNone, Persistent: persistent, EFI: efi, Device: device}
	return nil
}

func (m *MockBMCClient) GetBootFlags(_ context.Context) (*BootFlags, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetBootFlagsCalls++
	if m.BootFlagsError != nil {
		return nil, m.BootFlagsError
	}
	bf := m.BootFlags
	return &bf, nil
}

func (m *MockBMCClient) GetUsers(_ context.Context, channel uint8) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetUsersCalls++
	m.LastUserChannel = channel
	if m.UsersError != nil {
		return nil, m.UsersError
	}
	return append([]User(nil), m.Users...), nil
}

func (m *MockBMCClient) SetUserName(_ context.Context, userID uint8, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetUserNameCalls++
	m.LastUserID = userID
	m.LastUserName = name
	return m.UserOpError
}

func (m *MockBMCClient) SetUserPassword(_ context.Context, userID uint8, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetUserPasswordCalls++
	m.LastUserID = userID
	m.LastUserPassword = password
	return m.UserOpError
}

func (m *MockBMCClient) SetUserPrivilege(_ context.Context, userID, channel uint8, priv UserPrivilege) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetUserPrivilegeCalls++
	m.LastUserID = userID
	m.LastUserChannel = channel
	m.LastUserPrivilege = priv
	return m.UserOpError
}

func (m *MockBMCClient) EnableUser(_ context.Context, userID uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EnableUserCalls++
	m.LastUserID = userID
	return m.UserOpError
}

func (m *MockBMCClient) DisableUser(_ context.Context, userID uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DisableUserCalls++
	m.LastUserID = userID
	return m.UserOpError
}

func (m *MockBMCClient) GetChannelAccess(_ context.Context, channel uint8, persist ChannelPersistence) (*ChannelAccess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetChannelAccessCalls++
	m.LastUserChannel = channel
	m.LastChannelPersist = persist
	if m.ChannelAccessError != nil {
		return nil, m.ChannelAccessError
	}
	ca := m.ChannelAccess
	return &ca, nil
}

func (m *MockBMCClient) SetChannelAccess(_ context.Context, channel uint8, access ChannelAccess, persist ChannelPersistence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetChannelAccessCalls++
	m.LastUserChannel = channel
	m.LastChannelAccess = access
	m.LastChannelPersist = persist
	if m.ChannelAccessOpErr != nil {
		return m.ChannelAccessOpErr
	}
	m.ChannelAccess = access
	return nil
}

func (m *MockBMCClient) GetLanConfig(_ context.Context, channel uint8) (*LanConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetLanConfigCalls++
	m.LastUserChannel = channel
	if m.LanConfigError != nil {
		return nil, m.LanConfigError
	}
	cfg := m.LanConfig
	return &cfg, nil
}

func (m *MockBMCClient) ApplyLanConfig(_ context.Context, channel uint8, update LanConfigUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApplyLanConfigCalls++
	m.LastUserChannel = channel
	m.LastLanUpdate = update
	return m.ApplyLanConfigErr
}

func (m *MockBMCClient) GetWatchdog(_ context.Context) (*WatchdogConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetWatchdogCalls++
	if m.WatchdogError != nil {
		return nil, m.WatchdogError
	}
	cfg := m.Watchdog
	return &cfg, nil
}

func (m *MockBMCClient) SetWatchdog(_ context.Context, cfg WatchdogConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetWatchdogCalls++
	m.LastWatchdog = cfg
	if m.WatchdogOpErr != nil {
		return m.WatchdogOpErr
	}
	m.Watchdog = cfg
	return nil
}

func (m *MockBMCClient) ResetWatchdog(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResetWatchdogCalls++
	return m.WatchdogOpErr
}

func (m *MockBMCClient) ChassisIdentify(_ context.Context, duration int, indefinite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ChassisIdentifyCalls++
	m.LastIdentifyDur = duration
	m.LastIdentifyInd = indefinite
	return m.IdentifyError
}

func (m *MockBMCClient) GetSOL(_ context.Context, channel uint8) (*SOLConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetSOLCalls++
	m.LastUserChannel = channel
	if m.SOLConfigError != nil {
		return nil, m.SOLConfigError
	}
	cfg := m.SOLConfig
	return &cfg, nil
}

func (m *MockBMCClient) ApplySOL(_ context.Context, channel uint8, update SOLConfigUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApplySOLCalls++
	m.LastUserChannel = channel
	m.LastSOLUpdate = update
	return m.ApplySOLErr
}
