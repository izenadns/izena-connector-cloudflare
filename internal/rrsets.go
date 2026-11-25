package internal

import (
	"context"

	"github.com/izenadns/izena-connector-contract/connector"
)

// This connector operates on individual records. RRset operations have no
// upstream HTTP routes and report the unsupported capability.
func (c *Client) ListRRsetsPage(ctx context.Context, zoneID string, input connector.PageInput) (connector.Page[connector.RRset], error) {
	return connector.Page[connector.RRset]{}, connector.ErrUnsupported
}

func (c *Client) GetRRset(ctx context.Context, zoneID, name, rrType string) (connector.RRset, error) {
	return connector.RRset{}, connector.ErrUnsupported
}

func (c *Client) ReplaceRRset(ctx context.Context, zoneID string, input connector.RRsetReplaceInput) error {
	return connector.ErrUnsupported
}

func (c *Client) DeleteRRset(ctx context.Context, zoneID, name, rrType string) error {
	return connector.ErrUnsupported
}
