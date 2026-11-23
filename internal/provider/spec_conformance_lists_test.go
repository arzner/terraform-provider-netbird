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
	"PolicyRuleUpdate.sourceResource":       "source_resource",
	"PolicyRuleUpdate.destinationResource":  "destination_resource",

	// The OS version check flattens each {min_version} or {min_kernel_version}
	// object into one string attribute.
	"OSVersionCheck.android": "android_min_version",
	"OSVersionCheck.darwin":  "darwin_min_version",
	"OSVersionCheck.ios":     "ios_min_version",
	"OSVersionCheck.linux":   "linux_min_kernel_version",
	"OSVersionCheck.windows": "windows_min_kernel_version",
}

// specNestedFlattened lists nested array properties whose item members the
// provider models directly on the parent block, as "Schema.property".
var specNestedFlattened = map[string]bool{
	// process_check is a list of processes, one block per process.
	"ProcessCheck.processes": true,
}

// specUnmodelled lists the spec request properties a resource deliberately
// does not model, as "Schema.property": "reason". Every entry needs a reason a
// reviewer can check; a property that later gets modelled or leaves the spec
// fails Test_SpecConformance until it is removed here.
var specUnmodelled = map[string]string{
	"AccessRestrictions.crowdsec_mode":     "not modelled yet (#45); server default off",
	"ServiceTarget.access_action":          "not modelled yet (#45); server default inherit",
	"ServiceTargetOptions.direct_upstream": "not modelled yet (#45); server default false",

	// Account settings the resource does not manage yet (#23). The raw update
	// sends the account's current value back unchanged, so they are kept.
	"AccountSettings.agent_network_only":               "not modelled yet (#23); kept by the raw update",
	"AccountSettings.auto_update_always":               "not modelled yet (#23); kept by the raw update",
	"AccountSettings.dashboard_features":               "not modelled yet (#23); kept by the raw update",
	"AccountSettings.metrics_push_enabled":             "not modelled yet (#23); kept by the raw update",
	"AccountSettings.peer_hostname_collision_rejected": "not modelled yet (#23); kept by the raw update",

	"PeerRequest.ipv6":             "not modelled yet (#24); the server keeps the peer's address when it is omitted",
	"ServiceRequest.private":       "not modelled yet (#25)",
	"ServiceRequest.access_groups": "not modelled yet (#25); only used with private",
	"Checks.certificate_check":     "not modelled yet (#26)",

	"CreateScimIntegrationRequest.connector_id": "not modelled yet (#27); embedded-IdP SCIM only",
	"UpdateScimIntegrationRequest.connector_id": "not modelled yet (#27); embedded-IdP SCIM only",
}
