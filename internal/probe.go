package internal

import (
	"context"
	"net/http"
	"net/url"

	"github.com/izenadns/izena-connector-contract/connector"
)

// Probe verifies access to the configured account's zone inventory.
func (c *Client) Probe(ctx context.Context) (connector.Profile, error) {
	var zones []zoneWire
	query := url.Values{"account.id": {c.accountID}, "per_page": {"5"}}
	if _, err := c.request(ctx, http.MethodGet, []string{"zones"}, query, nil, &zones); err != nil {
		return connector.Profile{}, err
	}
	for _, zone := range zones {
		if zone.Account.ID != c.accountID {
			return connector.Profile{}, connector.ErrMalformed
		}
	}
	return connector.Profile{DaemonType: providerName, Version: "v4"}, nil
}
