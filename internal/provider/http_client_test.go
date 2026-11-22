// Copyright (c) HashiCorp, Inc.

package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
)

type trackingBody struct {
	io.Reader
	closed bool
}

func (t *trackingBody) Close() error { t.closed = true; return nil }

func Test_boundedBody(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		limit   int64
		wantErr error
	}{
		"under the limit":    {"abc", 10, nil},
		"exactly the limit":  {"abcd", 4, nil},
		"over the limit":     {"abcde", 4, errResponseTooLarge},
		"far over the limit": {strings.Repeat("x", 1<<20), 16, errResponseTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			src := &trackingBody{Reader: strings.NewReader(tc.body)}
			b := &boundedBody{body: src, left: tc.limit}
			got, err := io.ReadAll(b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, found %v", tc.wantErr, err)
			}
			if int64(len(got)) > tc.limit {
				t.Errorf("read %d bytes past a limit of %d", len(got), tc.limit)
			}
			if tc.wantErr == nil && string(got) != tc.body {
				t.Errorf("expected %q, found %q", tc.body, got)
			}
			if !src.closed {
				t.Error("the body must be released without an explicit Close")
			}
		})
	}
}

// The client library reads an error response with io.ReadAll and no limit;
// through the bounded client an oversized error body fails instead.
func Test_boundedClient_errorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message":"`+strings.Repeat("x", 4096)+`"}`)
	}))
	defer srv.Close()
	c := netbird.NewWithOptions(netbird.WithManagementURL(srv.URL), netbird.WithPAT("t"),
		netbird.WithHttpClient(boundedClient{client: srv.Client(), limit: 1024}))
	_, err := c.Accounts.List(t.Context())
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("expected the size limit error, found %v", err)
	}
}
