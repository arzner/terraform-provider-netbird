// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

// The API returns groups and users in no defined order; the plural data
// sources list them by ID so the same account reads the same way every time.
func Test_pluralDataSources_sortByID(t *testing.T) {
	ctx := context.Background()
	groups := []api.Group{{Id: "c", Name: "c"}, {Id: "a", Name: "a"}, {Id: "b", Name: "b"}}
	reversed := slices.Clone(groups)
	slices.Reverse(reversed)
	for _, in := range [][]api.Group{groups, reversed} {
		var data GroupsModel
		if d := groupsAPIToTerraform(ctx, in, &data); d.HasError() {
			t.Fatal(d)
		}
		var ids []string
		data.Ids.ElementsAs(ctx, &ids, false)
		if !slices.Equal(ids, []string{"a", "b", "c"}) || data.Groups[0].Id.ValueString() != "a" {
			t.Errorf("groups: expected a, b, c, found %v", ids)
		}
	}
	if groups[0].Id != "c" {
		t.Error("the caller's slice must not be reordered")
	}
	users := []api.User{{Id: "z", Email: "z"}, {Id: "x", Email: "x"}, {Id: "y", Email: "y"}}
	var data UsersModel
	if d := usersAPIToTerraform(ctx, users, &data); d.HasError() {
		t.Fatal(d)
	}
	var ids []string
	data.Ids.ElementsAs(ctx, &ids, false)
	if !slices.Equal(ids, []string{"x", "y", "z"}) || data.Users[0].Id.ValueString() != "x" {
		t.Errorf("users: expected x, y, z, found %v", ids)
	}
}
