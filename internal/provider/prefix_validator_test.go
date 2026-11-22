// Copyright (c) HashiCorp, Inc.

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Test_canonicalPrefix(t *testing.T) {
	for _, tc := range []struct {
		value string
		ipv6  bool
		ok    bool
	}{
		{"fd00:1234:5678::/64", true, true},
		{"FD00:1234:5678::/64", true, false},
		{"fd00:1234:5678:0::/64", true, false},
		{"fd00:1234:5678::1/64", true, false},
		{"100.64.0.0/10", true, false},
		{"::ffff:100.64.0.0/106", true, false},
		{"not-a-prefix", true, false},
		{"100.64.0.0/10", false, true},
		{"100.64.0.1/10", false, false},
		{"fd00::/64", false, false},
	} {
		var resp validator.StringResponse
		canonicalPrefix{ipv6: tc.ipv6}.ValidateString(context.Background(),
			validator.StringRequest{ConfigValue: types.StringValue(tc.value)}, &resp)
		if ok := !resp.Diagnostics.HasError(); ok != tc.ok {
			t.Errorf("%q (ipv6 %v): expected valid %v, found %v %v", tc.value, tc.ipv6, tc.ok, ok, resp.Diagnostics)
		}
	}
	var resp validator.StringResponse
	canonicalPrefix{ipv6: true}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringNull()}, &resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("null must be accepted")
	}
}
