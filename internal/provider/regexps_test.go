package provider

import "regexp"

// regexpSelfDisable matches the lockout-guard diagnostic emitted when a
// plan would disable the connection user.
var regexpSelfDisable = regexp.MustCompile(`self-disable would lock`)
