package provider

import "testing"

func TestParseHostPortID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		in        string
		wantHost  string
		wantPort  int
		wantError bool
	}{
		{"v4 default port", "192.0.2.10:623", "192.0.2.10", 623, false},
		{"v4 alt port", "192.0.2.10:6230", "192.0.2.10", 6230, false},
		{"hostname", "bmc-01.lab:623", "bmc-01.lab", 623, false},
		{"missing port", "192.0.2.10", "", 0, true},
		{"extra path segment", "192.0.2.10:623/ch1", "", 0, true},
		{"empty", "", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port, err := parseHostPortID(tc.in)
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v wantError=%v", err, tc.wantError)
			}
			if !tc.wantError && (host != tc.wantHost || port != tc.wantPort) {
				t.Errorf("got %s:%d want %s:%d", host, port, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func TestParseHostPortChannelID(t *testing.T) {
	t.Parallel()
	host, port, ch, err := parseHostPortChannelID("192.0.2.10:623/ch1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "192.0.2.10" || port != 623 || ch != 1 {
		t.Errorf("got %s:%d ch%d", host, port, ch)
	}
	if _, _, _, err := parseHostPortChannelID("192.0.2.10:623"); err == nil {
		t.Errorf("expected error on missing /chN")
	}
}

func TestParseHostPortChannelUserID(t *testing.T) {
	t.Parallel()
	host, port, ch, uid, err := parseHostPortChannelUserID("192.0.2.10:623/ch1/user2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "192.0.2.10" || port != 623 || ch != 1 || uid != 2 {
		t.Errorf("got %s:%d ch%d user%d", host, port, ch, uid)
	}
	if _, _, _, _, err := parseHostPortChannelUserID("192.0.2.10:623/ch1"); err == nil {
		t.Errorf("expected error on missing /userN")
	}
}
