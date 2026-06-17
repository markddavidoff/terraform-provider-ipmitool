package ipmi

import (
	"context"
	"errors"
	"testing"
)

func TestConnectionParams_Merge(t *testing.T) {
	t.Parallel()

	base := ConnectionParams{
		Host:        "192.0.2.10",
		Username:    "root",
		Password:    "oldpass",
		Port:        IntPtr(623),
		Interface:   "lanplus",
		CipherSuite: IntPtr(3),
		TimeoutSecs: 15,
	}

	t.Run("override host and password only", func(t *testing.T) {
		got := base.Merge(ConnectionParams{Host: "192.0.2.11", Password: "newpass"})
		if got.Host != "192.0.2.11" {
			t.Errorf("Host: want 192.0.2.11, got %q", got.Host)
		}
		if got.Password != "newpass" {
			t.Errorf("Password: want newpass, got %q", got.Password)
		}
		if got.Username != "root" {
			t.Errorf("Username should be preserved from base, got %q", got.Username)
		}
		if got.Port == nil || *got.Port != 623 || got.CipherSuite == nil || *got.CipherSuite != 3 {
			t.Errorf("numeric fields should be preserved: port=%v cipher=%v", got.Port, got.CipherSuite)
		}
	})

	t.Run("nil-pointer override returns base", func(t *testing.T) {
		got := base.Merge(ConnectionParams{})
		if got.Host != base.Host || got.Username != base.Username ||
			got.Password != base.Password || got.Interface != base.Interface ||
			got.TimeoutSecs != base.TimeoutSecs ||
			got.Port != base.Port || got.CipherSuite != base.CipherSuite {
			t.Errorf("nil override should not change base, got %+v", got)
		}
	})

	t.Run("explicit zero cipher overrides non-zero base", func(t *testing.T) {
		got := base.Merge(ConnectionParams{CipherSuite: IntPtr(0)})
		if got.CipherSuite == nil || *got.CipherSuite != 0 {
			t.Errorf("explicit CipherSuite=0 should override base, got %v", got.CipherSuite)
		}
	})
}

func TestMockBMCClient_GetChassisStatus(t *testing.T) {
	t.Parallel()

	t.Run("returns programmed status and records call", func(t *testing.T) {
		m := &MockBMCClient{ChassisStatus: ChassisStatus{PowerOn: true}}
		got, err := m.GetChassisStatus(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.PowerOn {
			t.Errorf("want PowerOn=true, got %v", got.PowerOn)
		}
		if m.GetChassisStatusCalls != 1 {
			t.Errorf("want 1 call, got %d", m.GetChassisStatusCalls)
		}
	})

	t.Run("returns programmed error", func(t *testing.T) {
		want := errors.New("boom")
		m := &MockBMCClient{ChassisError: want}
		_, err := m.GetChassisStatus(context.Background())
		if !errors.Is(err, want) {
			t.Errorf("want error %v, got %v", want, err)
		}
		if m.GetChassisStatusCalls != 1 {
			t.Errorf("call should be recorded even on error, got %d", m.GetChassisStatusCalls)
		}
	})
}

func TestDecodeBootFlags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []byte
		want BootFlags
	}{
		{
			name: "no override",
			raw:  []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			want: BootFlags{Device: BootDeviceNone},
		},
		{
			name: "valid + persistent + EFI + cdrom",
			// byte[1] = 0xE0 (valid + persistent + EFI)
			// byte[2] = 5<<2 = 0x14 (cdrom)
			raw:  []byte{0x01, 0xE0, 0x14},
			want: BootFlags{Valid: true, Persistent: true, EFI: true, Device: BootDeviceCDROM},
		},
		{
			name: "valid one-shot PXE",
			// byte[1] = 0x80 (valid only), byte[2] = 1<<2 = 0x04 (PXE)
			raw:  []byte{0x01, 0x80, 0x04},
			want: BootFlags{Valid: true, Device: BootDevicePXE},
		},
		{
			name: "too short returns zero with device=none",
			raw:  []byte{0x01},
			want: BootFlags{Device: BootDeviceNone},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeBootFlags(tc.raw)
			if *got != tc.want {
				t.Errorf("decodeBootFlags(% x) = %+v; want %+v", tc.raw, *got, tc.want)
			}
		})
	}
}

func TestParseHexBytes(t *testing.T) {
	t.Parallel()
	got, err := parseHexBytes("01 80 04 00 00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []byte{0x01, 0x80, 0x04, 0x00, 0x00}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d: got %02x, want %02x", i, got[i], want[i])
		}
	}

	if _, err := parseHexBytes("not-hex"); err == nil {
		t.Errorf("expected error on non-hex input")
	}
}

func TestSplitKV(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line        string
		wantK, wantV string
	}{
		{"System Power         : on", "System Power", "on"},
		{"  Power Overload     : false", "Power Overload", "false"},
		{"no colon here", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			k, v := splitKV(tc.line)
			if k != tc.wantK || v != tc.wantV {
				t.Errorf("splitKV(%q) = (%q, %q); want (%q, %q)",
					tc.line, k, v, tc.wantK, tc.wantV)
			}
		})
	}
}
