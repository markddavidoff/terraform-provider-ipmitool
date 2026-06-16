package ipmi

import (
	"reflect"
	"testing"
)

func TestParseIPv4(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []byte
		err  bool
	}{
		{"192.0.2.10", []byte{0xc0, 0x00, 0x02, 0x0a}, false},
		{"0.0.0.0", []byte{0, 0, 0, 0}, false},
		{"255.255.255.0", []byte{0xff, 0xff, 0xff, 0}, false},
		{"256.0.0.0", nil, true},
		{"-1.0.0.0", nil, true},
		{"not.an.ip.address", nil, true},
		{"", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseIPv4(tc.in)
			if tc.err {
				if err == nil {
					t.Errorf("expected error for %q, got %v", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got % x, want % x", got, tc.want)
			}
		})
	}
}

func TestFormatIPv4(t *testing.T) {
	t.Parallel()
	if got := formatIPv4([]byte{0xc0, 0x00, 0x02, 0x0a}); got != "192.0.2.10" {
		t.Errorf("got %q, want 192.0.2.10", got)
	}
}

func TestFormatMAC(t *testing.T) {
	t.Parallel()
	if got := formatMAC([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("got %q, want aa:bb:cc:dd:ee:ff", got)
	}
}

func TestIPSourceRoundTrip(t *testing.T) {
	t.Parallel()
	for _, src := range []LanIPSource{IPSourceUnspecified, IPSourceStatic, IPSourceDHCP, IPSourceBIOS, IPSourceOther} {
		t.Run(string(src), func(t *testing.T) {
			b := encodeIPSource(src)
			got := decodeIPSource(b)
			if got != src {
				t.Errorf("round-trip: %s → %d → %s", src, b, got)
			}
		})
	}
}
