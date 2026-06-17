package provider

import (
	"fmt"
	"regexp"
	"strconv"
)

// ID format conventions used across resources for ImportState:
//
//   ipmi_power, ipmi_boot_device, ipmi_chassis_identify, ipmi_watchdog,
//   ipmi_sol:
//     "<host>:<port>"
//     Example: "192.0.2.10:623"
//
//   ipmi_lan, ipmi_channel_access:
//     "<host>:<port>/ch<channel>"
//     Example: "192.0.2.10:623/ch1"
//
//   ipmi_user:
//     "<host>:<port>/ch<channel>/user<id>"
//     Example: "192.0.2.10:623/ch1/user2"
//
// The provider's Read implementation populates whatever else can be
// recovered from the BMC. Connection credentials (username, password,
// cipher_suite) cannot be imported — they must be set in HCL or come
// from the provider block.

var (
	hostPortOnlyRE = regexp.MustCompile(`^([^:/]+):(\d+)$`)
	hostPortChRE   = regexp.MustCompile(`^([^:/]+):(\d+)/ch(\d+)$`)
	hostPortChUsRE = regexp.MustCompile(`^([^:/]+):(\d+)/ch(\d+)/user(\d+)$`)
)

// parseHostPortID parses "<host>:<port>".
func parseHostPortID(id string) (host string, port int, err error) {
	m := hostPortOnlyRE.FindStringSubmatch(id)
	if m == nil {
		return "", 0, fmt.Errorf(
			"invalid import ID %q: expected format <host>:<port> (e.g. 192.0.2.10:623)",
			id,
		)
	}
	p, _ := strconv.Atoi(m[2])
	return m[1], p, nil
}

// parseHostPortChannelID parses "<host>:<port>/ch<channel>".
func parseHostPortChannelID(id string) (host string, port int, channel int, err error) {
	m := hostPortChRE.FindStringSubmatch(id)
	if m == nil {
		return "", 0, 0, fmt.Errorf(
			"invalid import ID %q: expected format <host>:<port>/ch<channel> "+
				"(e.g. 192.0.2.10:623/ch1)",
			id,
		)
	}
	p, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return m[1], p, c, nil
}

// parseHostPortChannelUserID parses "<host>:<port>/ch<channel>/user<id>".
func parseHostPortChannelUserID(id string) (host string, port int, channel int, userID int, err error) {
	m := hostPortChUsRE.FindStringSubmatch(id)
	if m == nil {
		return "", 0, 0, 0, fmt.Errorf(
			"invalid import ID %q: expected format <host>:<port>/ch<channel>/user<id> "+
				"(e.g. 192.0.2.10:623/ch1/user2)",
			id,
		)
	}
	p, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	u, _ := strconv.Atoi(m[4])
	return m[1], p, c, u, nil
}
