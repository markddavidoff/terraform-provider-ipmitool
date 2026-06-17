package ipmi

import (
	"context"
	"strings"
	"testing"
)

// TestRun_PasswordNotInArgv verifies the BMC password is never passed
// as a command-line argument to the ipmitool subprocess. Closes C-1
// (argv leak via ps / /proc/<pid>/cmdline). The password must reach
// ipmitool via the IPMI_PASSWORD environment variable instead (-E flag).
//
// We use `echo` as the stand-in binary because it succeeds (exit 0) and
// prints its argv to stdout — which run() returns to us. If the password
// shows up in that output, run() built the wrong argv.
func TestRun_PasswordNotInArgv(t *testing.T) {
	t.Parallel()

	const sentinel = "PASSWORD-SENTINEL-9f2e7c"

	c := &ipmitoolClient{
		binary: "echo",
		params: ConnectionParams{
			Host:        "192.0.2.99",
			Username:    "u",
			Password:    sentinel,
			Port:        IntPtr(623),
			Interface:   "lanplus",
			CipherSuite: IntPtr(17),
			TimeoutSecs: 5,
		},
	}

	out, err := c.run(context.Background(), "chassis", "status")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if strings.Contains(out, sentinel) {
		t.Fatalf("password sentinel %q appeared in argv (run output): %s", sentinel, out)
	}
}

// TestRun_UsesEnvVarFlag verifies that the ipmitool argv includes the
// `-E` flag (read password from IPMI_PASSWORD env). Combined with
// TestRun_PasswordNotInArgv, this confirms the password handoff is via
// env, not argv.
func TestRun_UsesEnvVarFlag(t *testing.T) {
	t.Parallel()

	c := &ipmitoolClient{
		binary: "echo",
		params: ConnectionParams{
			Host:        "192.0.2.99",
			Username:    "u",
			Password:    "irrelevant",
			Port:        IntPtr(623),
			Interface:   "lanplus",
			CipherSuite: IntPtr(17),
			TimeoutSecs: 5,
		},
	}

	out, err := c.run(context.Background(), "chassis", "status")
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(out, " -E ") && !strings.HasSuffix(strings.TrimSpace(out), " -E") {
		t.Fatalf("ipmitool argv missing -E flag: %s", out)
	}
	// Also: argv must NOT contain "-P " (the old flag we're replacing).
	if strings.Contains(out, " -P ") {
		t.Fatalf("ipmitool argv still contains -P flag: %s", out)
	}
}
