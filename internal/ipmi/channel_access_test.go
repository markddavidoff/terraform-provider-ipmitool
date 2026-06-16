package ipmi

import "testing"

func TestEncodeDecodeChannelAccess(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   ChannelAccess
	}{
		{"always + admin + all auth enabled",
			ChannelAccess{
				AccessMode: ChannelAccessAlways, UserLevelAuth: true,
				PerMessageAuth: true, PEFAlerting: true,
				PrivilegeLimit: UserPrivAdministrator,
			}},
		{"disabled + operator + per-msg auth off",
			ChannelAccess{
				AccessMode: ChannelAccessDisabled, UserLevelAuth: true,
				PerMessageAuth: false, PEFAlerting: true,
				PrivilegeLimit: UserPrivOperator,
			}},
		{"pre_boot + user + everything disabled",
			ChannelAccess{
				AccessMode: ChannelAccessPreBoot, UserLevelAuth: false,
				PerMessageAuth: false, PEFAlerting: false,
				PrivilegeLimit: UserPrivUser,
			}},
		{"shared + callback",
			ChannelAccess{
				AccessMode: ChannelAccessShared, UserLevelAuth: true,
				PerMessageAuth: true, PEFAlerting: true,
				PrivilegeLimit: UserPrivCallback,
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := encodeChannelAccessSettings(tc.in)
			priv := encodeChannelPriv(tc.in.PrivilegeLimit)
			got := decodeChannelAccess([]byte{settings, priv})
			if *got != tc.in {
				t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", *got, tc.in)
			}
		})
	}
}
