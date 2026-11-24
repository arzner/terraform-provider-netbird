package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"math"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

func Test_dnsRecordAPIToTerraform(t *testing.T) {
	cases := []struct {
		resource *api.DNSRecord
		zoneId   string
		expected DNSRecordModel
	}{
		{
			resource: &api.DNSRecord{
				Id:      "rec123",
				Name:    "www.netbird.io",
				Type:    api.DNSRecordTypeA,
				Content: "192.168.1.1",
				Ttl:     300,
			},
			zoneId: "zone123",
			expected: DNSRecordModel{
				Id:      types.StringValue("rec123"),
				ZoneId:  types.StringValue("zone123"),
				Name:    types.StringValue("www.netbird.io"),
				Type:    types.StringValue("A"),
				Content: types.StringValue("192.168.1.1"),
				Ttl:     types.Int64Value(300),
			},
		},
		{
			resource: &api.DNSRecord{
				Id:      "rec456",
				Name:    "api.netbird.io",
				Type:    api.DNSRecordTypeAAAA,
				Content: "2001:db8::1",
				Ttl:     600,
			},
			zoneId: "zone456",
			expected: DNSRecordModel{
				Id:      types.StringValue("rec456"),
				ZoneId:  types.StringValue("zone456"),
				Name:    types.StringValue("api.netbird.io"),
				Type:    types.StringValue("AAAA"),
				Content: types.StringValue("2001:db8::1"),
				Ttl:     types.Int64Value(600),
			},
		},
		{
			resource: &api.DNSRecord{
				Id:      "rec789",
				Name:    "mail.netbird.io",
				Type:    api.DNSRecordTypeCNAME,
				Content: "mail.example.com",
				Ttl:     3600,
			},
			zoneId: "zone789",
			expected: DNSRecordModel{
				Id:      types.StringValue("rec789"),
				ZoneId:  types.StringValue("zone789"),
				Name:    types.StringValue("mail.netbird.io"),
				Type:    types.StringValue("CNAME"),
				Content: types.StringValue("mail.example.com"),
				Ttl:     types.Int64Value(3600),
			},
		},
	}

	for _, c := range cases {
		var out DNSRecordModel
		dnsRecordAPIToTerraform(c.resource, c.zoneId, &out)

		if !reflect.DeepEqual(out, c.expected) {
			t.Fatalf("Expected:\n%#v\nFound:\n%#v", c.expected, out)
		}
	}
}

// The TTL takes what the server accepts: 0 or more.
func Test_dnsRecordTTLValidator(t *testing.T) {
	ctx := context.Background()
	var sch resource.SchemaResponse
	(&DNSRecord{}).Schema(ctx, resource.SchemaRequest{}, &sch)
	attr, ok := sch.Schema.Attributes["ttl"].(schema.Int64Attribute)
	if !ok {
		t.Fatal("ttl is not an Int64Attribute")
	}
	for ttl, wantError := range map[int64]bool{-1: true, 0: false, 30: false, 300: false, 172800: false, int64(math.MaxInt): false} {
		var resp validator.Int64Response
		for _, v := range attr.Validators {
			v.ValidateInt64(ctx, validator.Int64Request{Path: path.Root("ttl"), ConfigValue: types.Int64Value(ttl)}, &resp)
		}
		if resp.Diagnostics.HasError() != wantError {
			t.Errorf("ttl %d: expected error %v, found %v", ttl, wantError, resp.Diagnostics)
		}
	}
}
