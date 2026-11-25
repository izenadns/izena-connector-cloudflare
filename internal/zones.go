package internal

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/izenadns/izena-connector-contract/connector"
)

const (
	zoneKindFull      = "full"
	zoneKindPartial   = "partial"
	zoneKindSecondary = "secondary"
	zoneKindInternal  = "internal"

	minZonePageSize = 5
	maxZonePageSize = 50
)

type zoneWire struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Account zoneAccountWire `json:"account"`
	Plan    struct {
		LegacyID string `json:"legacy_id"`
	} `json:"plan"`
}

type zoneAccountWire struct {
	ID string `json:"id"`
}

func (c *Client) ListZones(ctx context.Context) ([]connector.Zone, error) {
	zones := make([]connector.Zone, 0)
	input := connector.PageInput{Limit: maxZonePageSize}
	for {
		page, err := c.ListZonesPage(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			zones = append(zones, connector.Zone{ID: item.ID, Name: item.Name, Kind: item.Kind})
		}
		if page.NextCursor == "" {
			return zones, nil
		}
		input.Cursor = page.NextCursor
	}
}

func (c *Client) ListZonesPage(ctx context.Context, input connector.PageInput) (connector.Page[connector.ZoneMetadata], error) {
	var result connector.Page[connector.ZoneMetadata]
	limit := min(input.Limit, maxZonePageSize)
	pageInput := input
	offset := 0
	if limit > 0 && limit < minZonePageSize {
		pageInput.Limit = minZonePageSize
		if input.Cursor != "" {
			cursor, position, ok := strings.Cut(input.Cursor, "/")
			var err error
			offset, err = strconv.Atoi(position)
			if !ok || err != nil || offset < 0 || offset >= minZonePageSize {
				return result, invalidPage()
			}
			pageInput.Cursor = cursor
		}
	}
	query, err := zonePageQuery(pageInput)
	if err != nil {
		return result, err
	}
	query.Set("account.id", c.accountID)
	query.Set("order", "name")
	query.Set("type", "full,partial,secondary,internal")

	var wire []zoneWire
	info, err := c.request(ctx, http.MethodGet, []string{"zones"}, query, nil, &wire)
	if err != nil {
		return result, err
	}
	if wire == nil || len(wire) > max(limit, minZonePageSize) {
		return result, &connector.Error{Kind: connector.ErrMalformed, Code: "malformed_zone_list"}
	}
	upper := min(offset+limit, len(wire))
	offset = min(offset, len(wire))
	result.Items = make([]connector.ZoneMetadata, 0, upper-offset)
	for _, item := range wire[offset:upper] {
		if err := c.validateZone(item, ""); err != nil {
			return connector.Page[connector.ZoneMetadata]{}, err
		}
		result.Items = append(result.Items, zoneMetadata(item))
	}
	switch {
	case upper < len(wire):
		result.NextCursor = query.Get("page") + ":" + query.Get("per_page") + "/" + strconv.Itoa(upper)
	case limit < minZonePageSize:
		if cursor := nextZonePage(info, query, len(wire)); cursor != "" {
			result.NextCursor = cursor + "/0"
		}
	default:
		result.NextCursor = nextZonePage(info, query, len(wire))
	}
	return result, nil
}

func (c *Client) GetZone(ctx context.Context, id string) (connector.Zone, error) {
	wire, err := c.readZone(ctx, id)
	if err != nil {
		return connector.Zone{}, err
	}
	zone := decodeZone(wire)
	zone.Capabilities = zoneCapabilities(wire)
	return zone, nil
}

func (c *Client) GetZoneMetadata(ctx context.Context, id string) (connector.ZoneMetadata, error) {
	wire, err := c.readZone(ctx, id)
	if err != nil {
		return connector.ZoneMetadata{}, err
	}
	return zoneMetadata(wire), nil
}

func (c *Client) readZone(ctx context.Context, id string) (zoneWire, error) {
	if err := validateZoneID(id); err != nil {
		return zoneWire{}, err
	}
	var wire zoneWire
	if _, err := c.request(ctx, http.MethodGet, []string{"zones", id}, nil, nil, &wire); err != nil {
		return zoneWire{}, err
	}
	if err := c.validateZone(wire, id); err != nil {
		return zoneWire{}, err
	}
	return wire, nil
}

func (c *Client) validateZone(wire zoneWire, expectedID string) error {
	if !validID(wire.ID) || expectedID != "" && wire.ID != expectedID ||
		!validID(wire.Account.ID) || !validZoneName(strings.TrimSuffix(wire.Name, ".")) {
		return &connector.Error{Kind: connector.ErrMalformed, Code: "malformed_zone"}
	}
	if wire.Account.ID != c.accountID {
		return &connector.Error{Kind: connector.ErrNotFound, Code: "zone_outside_account"}
	}
	switch wire.Type {
	case zoneKindFull, zoneKindPartial, zoneKindSecondary, zoneKindInternal:
		return nil
	default:
		return &connector.Error{Kind: connector.ErrUnsupported, Code: "unsupported_zone_kind"}
	}
}

func zoneMetadata(wire zoneWire) connector.ZoneMetadata {
	return connector.ZoneMetadata{
		ID: wire.ID, Name: strings.TrimSuffix(wire.Name, ".") + ".", Kind: wire.Type,
	}
}

func decodeZone(wire zoneWire) connector.Zone {
	metadata := zoneMetadata(wire)
	return connector.Zone{ID: metadata.ID, Name: metadata.Name, Kind: metadata.Kind}
}

func zonePageQuery(input connector.PageInput) (url.Values, error) {
	if input.Limit <= 0 {
		return nil, invalidPage()
	}
	size := strconv.Itoa(min(input.Limit, maxZonePageSize))
	page := 1
	if input.Cursor != "" {
		number, cursorSize, ok := strings.Cut(input.Cursor, ":")
		var err error
		page, err = strconv.Atoi(number)
		if !ok || err != nil || page < 1 || cursorSize != size {
			return nil, invalidPage()
		}
	}
	return url.Values{"page": {strconv.Itoa(page)}, "per_page": {size}}, nil
}

func nextZonePage(info pageInfo, query url.Values, count int) string {
	page, _ := strconv.Atoi(query.Get("page"))
	size, _ := strconv.Atoi(query.Get("per_page"))
	hasMore := count >= size
	if info.TotalPages > 0 {
		hasMore = page < info.TotalPages
	}
	if count == 0 || !hasMore || page == math.MaxInt {
		return ""
	}
	return strconv.Itoa(page+1) + ":" + strconv.Itoa(size)
}

func validateZoneID(value string) error {
	if !validID(value) {
		return &connector.Error{Kind: connector.ErrValidation, Code: "invalid_zone_id"}
	}
	return nil
}

func validZoneName(value string) bool {
	if len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-", r) {
				return false
			}
		}
	}
	return true
}

var _ connector.ZoneReader = (*Client)(nil)
