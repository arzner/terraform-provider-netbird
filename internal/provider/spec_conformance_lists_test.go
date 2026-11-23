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
	// Account settings the resource does not manage yet (#23). The raw update
	// sends the account's current value back unchanged, so they are kept.
	"AccountSettings.agent_network_only":               "not modelled yet (#23); kept by the raw update",
	"AccountSettings.auto_update_always":               "not modelled yet (#23); kept by the raw update",
	"AccountSettings.dashboard_features":               "not modelled yet (#23); kept by the raw update",
	"AccountSettings.metrics_push_enabled":             "not modelled yet (#23); kept by the raw update",
	"AccountSettings.peer_hostname_collision_rejected": "not modelled yet (#23); kept by the raw update",

	"ServiceRequest.private":       "not modelled yet (#25)",
	"ServiceRequest.access_groups": "not modelled yet (#25); only used with private",
	"Checks.certificate_check":     "not modelled yet (#26)",

	"CreateScimIntegrationRequest.connector_id": "not modelled yet (#27); embedded-IdP SCIM only",
	"UpdateScimIntegrationRequest.connector_id": "not modelled yet (#27); embedded-IdP SCIM only",
}
