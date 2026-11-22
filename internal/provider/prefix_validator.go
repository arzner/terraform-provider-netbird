// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// canonicalPrefix accepts a CIDR prefix of one address family written the way
// the management server returns it: host bits zero, lower-case, shortest form.
// The server stores the masked prefix and returns its canonical spelling, so
// any other spelling of the same prefix would come back different from the
// plan and fail the apply.
type canonicalPrefix struct{ ipv6 bool }

func (v canonicalPrefix) family() string {
	if v.ipv6 {
		return "IPv6"
	}
	return "IPv4"
}

func (v canonicalPrefix) Description(context.Context) string {
	return fmt.Sprintf("must be an %s prefix in canonical CIDR form", v.family())
}

func (v canonicalPrefix) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v canonicalPrefix) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid network range", fmt.Sprintf("%q is not a CIDR prefix: %v", value, err))
		return
	}
	if prefix.Addr().Is6() != v.ipv6 || prefix.Addr().Is4In6() {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid network range", fmt.Sprintf("%q is not an %s prefix", value, v.family()))
		return
	}
	if canonical := prefix.Masked().String(); canonical != value {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid network range",
			fmt.Sprintf("%q is not in canonical form; write %q, as the management server returns it", value, canonical))
	}
}
