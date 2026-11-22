// Copyright (c) HashiCorp, Inc.

package provider

import (
	"errors"
	"io"
	"net/http"
)

// maxResponseBody bounds every management API response the provider reads.
const maxResponseBody = 64 << 20

var errResponseTooLarge = errors.New("management API response exceeds the size limit")

// boundedClient is the HTTP client handed to the NetBird client library. The
// library reads error responses to the end with no size limit and never closes
// them; the bounded body makes such a read fail past the limit and releases
// the connection at the end of the body, at the limit, or on Close.
type boundedClient struct {
	client *http.Client
	limit  int64
}

func newBoundedClient() boundedClient {
	return boundedClient{client: http.DefaultClient, limit: maxResponseBody}
}

func (c boundedClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &boundedBody{body: resp.Body, left: c.limit}
	return resp, nil
}

type boundedBody struct {
	body   io.ReadCloser
	left   int64
	closed bool
	err    error
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	// Read one byte past the limit to tell a body of exactly the limit from a
	// longer one.
	if int64(len(p)) > b.left+1 {
		p = p[:b.left+1]
	}
	n, err := b.body.Read(p)
	if int64(n) > b.left {
		b.err = errResponseTooLarge
		_ = b.Close()
		return int(b.left), b.err
	}
	b.left -= int64(n)
	if err != nil {
		b.err = err
		_ = b.Close()
	}
	return n, err
}

func (b *boundedBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	return b.body.Close()
}
