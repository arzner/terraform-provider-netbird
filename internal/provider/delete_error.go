// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net/http"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
)

// deleteError returns the error a Delete reports, or nil when the object is
// gone. NetBird answers 404 also for a revoked token and for another object
// the delete touches (v0.83.2, v0.82.1), so a 404 counts as deleted only when
// the object's own lookup answers 404 too and the token still works.
func deleteError(ctx context.Context, client *netbird.Client, err error, lookup func(context.Context) error) error {
	if err == nil || !netbird.IsNotFound(err) {
		return err
	}
	if lerr := lookup(ctx); lerr == nil || !netbird.IsNotFound(lerr) {
		return err
	}
	if _, aerr := client.Accounts.List(ctx); aerr != nil {
		return err
	}
	return nil
}

// userLookup is a lookup for deleteError: users have no single-user GET, so
// the list is searched.
func userLookup(ctx context.Context, client *netbird.Client, id string) error {
	users, err := client.Users.List(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Id == id {
			return nil
		}
	}
	return &netbird.APIError{StatusCode: http.StatusNotFound, Message: "user not found"}
}
