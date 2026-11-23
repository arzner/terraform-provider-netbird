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

func Test_canonicalAddress(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"fd00:1234:5678::7", true},
		{"FD00:1234:5678::7", false},
		{"fd00:1234:5678:0:0::7", false},
		{"fd00::1%eth0", false},
		{"100.64.0.7", false},
		{"::ffff:100.64.0.7", false},
		{"fd00::/64", false},
	} {
		var resp validator.StringResponse
		canonicalAddress{ipv6: true}.ValidateString(context.Background(),
			validator.StringRequest{ConfigValue: types.StringValue(tc.value)}, &resp)
		if ok := !resp.Diagnostics.HasError(); ok != tc.ok {
			t.Errorf("%q: expected valid %v, found %v %v", tc.value, tc.ok, ok, resp.Diagnostics)
		}
	}
}

func Test_peerIPv6Request(t *testing.T) {
	cur := "fd00::7"
	if got := peerIPv6Request(types.StringNull(), &cur); got != nil {
		t.Errorf("null plan: expected nil, found %v", *got)
	}
	if got := peerIPv6Request(types.StringUnknown(), &cur); got != nil {
		t.Errorf("unknown plan: expected nil, found %v", *got)
	}
	if got := peerIPv6Request(types.StringValue("fd00::7"), &cur); got != nil {
		t.Errorf("same address: expected nil, found %v", *got)
	}
	if got := peerIPv6Request(types.StringValue("fd00::8"), &cur); got == nil || *got != "fd00::8" {
		t.Errorf("new address: expected fd00::8, found %v", got)
	}
	if got := peerIPv6Request(types.StringValue("fd00::8"), nil); got == nil || *got != "fd00::8" {
		t.Errorf("no current address: expected fd00::8, found %v", got)
	}
}
