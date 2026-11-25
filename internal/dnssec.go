package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/izenadns/izena-connector-contract/connector"
)

type dnssecWire struct {
	Status     string          `json:"status"`
	ModifiedOn json.RawMessage `json:"modified_on,omitempty"`
	Algorithm  string          `json:"algorithm"`
	Flags      int             `json:"flags"`
	KeyTag     *int32          `json:"key_tag"`
	DS         string          `json:"ds"`
	PublicKey  string          `json:"public_key"`
}

type dnssecUpdateWire struct {
	Status string `json:"status"`
}

func (c *Client) SetDNSSEC(ctx context.Context, zoneID string, enabled bool) error {
	zone, err := c.readZone(ctx, zoneID)
	if err != nil {
		return err
	}
	if dnssecCapabilities(zone.Type).Signing != connector.SigningSupported {
		return connector.ErrUnsupported
	}
	status := "disabled"
	if enabled {
		status = "active"
	}
	_, err = c.request(ctx, http.MethodPatch, []string{"zones", zoneID, "dnssec"}, nil, dnssecUpdateWire{Status: status}, nil)
	return err
}

func (c *Client) GetDNSSEC(ctx context.Context, zoneID string) (connector.DNSSEC, error) {
	zone, err := c.readZone(ctx, zoneID)
	if err != nil {
		return connector.DNSSEC{}, err
	}
	if !dnssecCapabilities(zone.Type).Inspect {
		return connector.DNSSEC{}, connector.ErrUnsupported
	}
	var wire dnssecWire
	if _, err := c.request(ctx, http.MethodGet, []string{"zones", zoneID, "dnssec"}, nil, nil, &wire); err != nil {
		return connector.DNSSEC{}, err
	}
	return decodeDNSSEC(wire)
}

func decodeDNSSEC(wire dnssecWire) (connector.DNSSEC, error) {
	var result connector.DNSSEC
	active := true
	switch wire.Status {
	case "active", "pending":
		result.Enabled, result.Active = true, &active
	case "pending-disabled":
		result.Active = &active
	case "disabled":
		// Cloudflare still signs until DNSSEC is deleted. An explicit null
		// identifies deleted or never-configured DNSSEC.
		if len(wire.ModifiedOn) > 0 {
			var modified *time.Time
			if err := json.Unmarshal(wire.ModifiedOn, &modified); err != nil {
				return connector.DNSSEC{}, &connector.Error{Kind: connector.ErrMalformed, Code: "invalid_dnssec_timestamp"}
			}
			active = modified != nil
			result.Active = &active
		}
	case "error":
		return connector.DNSSEC{}, &connector.Error{Kind: connector.ErrUnavailable, Code: "dnssec_status_error"}
	default:
		return connector.DNSSEC{}, &connector.Error{Kind: connector.ErrMalformed, Code: "invalid_dnssec_status"}
	}
	if result.Active != nil && !*result.Active || wire.KeyTag == nil || *wire.KeyTag < 0 || *wire.KeyTag > 65535 {
		return result, nil
	}

	key := connector.DNSSECKey{
		ID: int64(*wire.KeyTag), KeyType: "ksk", KeyTag: *wire.KeyTag,
		Active: result.Active != nil && *result.Active, Published: wire.PublicKey != "",
	}
	if algorithm, err := strconv.ParseUint(wire.Algorithm, 10, 8); err == nil {
		key.Algorithm = &wire.Algorithm
		if wire.PublicKey != "" && wire.Flags > 0 && wire.Flags <= 65535 {
			key.DNSKEY = fmt.Sprintf("%d 3 %d %s", wire.Flags, algorithm, wire.PublicKey)
		}
	}
	if ds := strings.TrimSpace(wire.DS); ds != "" {
		key.DS = []string{ds}
	}
	if key.DNSKEY != "" || len(key.DS) != 0 {
		result.Keys = []connector.DNSSECKey{key}
	}
	return result, nil
}

var _ connector.DNSSECOperations = (*Client)(nil)
