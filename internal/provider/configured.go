// Copyright (c) HashiCorp, Inc.

package provider

import "github.com/hashicorp/terraform-plugin-framework/types"

// configuredOnly returns the planned value when the configuration sets the
// attribute and null otherwise: whether to send comes from the configuration,
// what to send from the plan (which honours ignore_changes).
func configuredOnly(planned, configured types.String) types.String {
	if configured.IsNull() {
		return types.StringNull()
	}
	return planned
}
