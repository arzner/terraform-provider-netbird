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

// specProjected lists object properties the provider models as one value,
// as "Schema.property": "member": the value holds that member of the object.
// The object's other members must be read-only or listed in specUnmodelled.
var specProjected = map[string]string{
	"OSVersionCheck.android": "min_version",
	"OSVersionCheck.darwin":  "min_version",
	"OSVersionCheck.ios":     "min_version",
	"OSVersionCheck.linux":   "min_kernel_version",
	"OSVersionCheck.windows": "min_kernel_version",

	// resources is a list of IDs; the provider sends each one's type.
	"GroupRequest.resources": "id",
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
	"GroupRequest.resources.type": "not configurable: the provider looks up each resource's type from its ID",
}
