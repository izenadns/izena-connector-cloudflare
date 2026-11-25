package internal

import (
	"context"
	"net/http"
	"strings"

	"github.com/izenadns/izena-connector-contract/connector"
)

type zoneCreateWire struct {
	Account zoneAccountWire `json:"account"`
	Name    string          `json:"name"`
	Type    string          `json:"type"`
}

type zoneUpdateWire struct {
	Type string `json:"type"`
}

func (c *Client) CreateZone(ctx context.Context, input connector.ZoneCreateInput) (connector.Zone, error) {
	if input.Kind != zoneKindFull && input.Kind != zoneKindPartial {
		return connector.Zone{}, &connector.Error{Kind: connector.ErrUnsupported, Code: "unsupported_zone_kind"}
	}
	name := strings.TrimSuffix(input.Name, ".")
	if !validZoneName(name) {
		return connector.Zone{}, &connector.Error{Kind: connector.ErrValidation, Code: "invalid_zone_name"}
	}
	if len(input.Settings) != 0 {
		return connector.Zone{}, &connector.Error{Kind: connector.ErrValidation, Code: "invalid_zone_settings"}
	}

	body := zoneCreateWire{Account: zoneAccountWire{ID: c.accountID}, Name: name, Type: input.Kind}
	var wire zoneWire
	if _, err := c.request(ctx, http.MethodPost, []string{"zones"}, nil, body, &wire); err != nil {
		return connector.Zone{}, err
	}
	if err := c.validateZone(wire, ""); err != nil {
		return connector.Zone{}, err
	}
	zone := decodeZone(wire)
	zone.Capabilities = zoneCapabilities(wire)
	return zone, nil
}

func (c *Client) UpdateZone(ctx context.Context, id string, input connector.ZoneUpdateInput) (connector.ZoneUpdateResult, error) {
	wire, err := c.readZone(ctx, id)
	if err != nil {
		return connector.ZoneUpdateResult{}, err
	}
	metadata := zoneMetadata(wire)
	result := connector.ZoneUpdateResult{Previous: &metadata}
	if wire.Type == input.Kind {
		return result, nil
	}
	kinds := zoneUpdateKinds(wire)
	if len(kinds) == 0 || kinds[0] != input.Kind {
		return result, &connector.Error{Kind: connector.ErrUnsupported, Code: "unsupported_zone_conversion"}
	}
	result.Attempted = true
	_, err = c.request(ctx, http.MethodPatch, []string{"zones", id}, nil, zoneUpdateWire{Type: input.Kind}, nil)
	return result, err
}

func (c *Client) DeleteZone(ctx context.Context, id string) error {
	if _, err := c.readZone(ctx, id); err != nil {
		return err
	}
	_, err := c.request(ctx, http.MethodDelete, []string{"zones", id}, nil, nil, nil)
	return err
}

var _ connector.ZoneMutator = (*Client)(nil)
