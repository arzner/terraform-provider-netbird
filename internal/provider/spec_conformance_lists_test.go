// Copyright (c) HashiCorp, Inc.

package provider

// specRenamed maps a spec property to the attribute that models it under a
// different name, as "Schema.property": "attribute".
var specRenamed = map[string]string{
	"Checks.nb_version_check":               "netbird_version_check",
	"CreateScimIntegrationRequest.provider": "provider_name",
	"CreateSetupKeyRequest.expires_in":      "expiry_seconds",
	"PersonalAccessTokenRequest.expires_in": "expiration_days",
	"PolicyUpdate.rules":                    "rule",
}

// specUnmodelled lists the spec request properties a resource deliberately
// does not model, as "Schema.property": "reason". Every entry needs a reason a
// reviewer can check; a property that later gets modelled or leaves the spec
// fails Test_SpecConformance until it is removed here.
var specUnmodelled = map[string]string{
	"PeerRequest.ipv6":             "not modelled yet (#24); the server keeps the peer's address when it is omitted",
	"ServiceRequest.private":       "not modelled yet (#25)",
	"ServiceRequest.access_groups": "not modelled yet (#25); only used with private",
}
