package internal

import (
	"context"
	"strings"

	"github.com/izenadns/izena-connector-contract/connector"
)

const proxiedKey = "proxied"

func (c *Client) Capabilities(ctx context.Context) (connector.ServerCapabilities, error) {
	return connector.ServerCapabilities{
		Provider:    providerID,
		ZoneKinds:   zoneKindOptions(),
		RecordModel: connector.RecordModelIndividual,
		RecordTypes: creatableRecordTypes(),
		Pagination:  connector.PaginationCapabilities{Zones: true, Records: true},
		DNSSEC:      dnssecCapabilities(zoneKindFull),
	}, nil
}

func zoneCapabilities(zone zoneWire) *connector.ZoneCapabilities {
	capabilities := &connector.ZoneCapabilities{
		UpdateZoneKinds: zoneUpdateKinds(zone),
		RecordTypes:     creatableRecordTypes(),
		AutomaticTTL:    true,
		DNSSEC:          dnssecCapabilities(zone.Type),
	}
	if zoneSupportsRecordWrites(zone.Type) {
		capabilities.WriteRecords = true
		capabilities.RecordComments = true
		capabilities.RecordOptions = []connector.SettingField{{
			Key: proxiedKey, Label: "Proxied", Kind: connector.SettingBoolean,
			Description: "Configure Cloudflare proxying for eligible A, AAAA, and CNAME records. Proxying one A or AAAA record affects all A and AAAA records at that name.",
		}}
	}
	return capabilities
}

func zoneSupportsRecordWrites(kind string) bool {
	return kind == zoneKindFull || kind == zoneKindPartial
}

func dnssecCapabilities(kind string) connector.DNSSECCapabilities {
	switch kind {
	case zoneKindFull:
		return connector.DNSSECCapabilities{Inspect: true, KeyMetadata: true, Signing: connector.SigningSupported}
	case zoneKindSecondary:
		return connector.DNSSECCapabilities{Inspect: true, KeyMetadata: true, Signing: connector.SigningUnknown}
	default:
		return connector.DNSSECCapabilities{Signing: connector.SigningUnsupported}
	}
}

func zoneUpdateKinds(wire zoneWire) []string {
	// Cloudflare restricts zone type edits to Enterprise and individually
	// enabled zones. The API does not report individual enablement.
	if !strings.EqualFold(wire.Plan.LegacyID, "enterprise") {
		return nil
	}
	switch wire.Type {
	case zoneKindFull:
		return []string{zoneKindPartial}
	case zoneKindPartial:
		return []string{zoneKindFull}
	default:
		return nil
	}
}

func zoneKindOptions() []connector.ZoneKindOption {
	return []connector.ZoneKindOption{
		{ID: zoneKindFull, Label: "Full"},
		{ID: zoneKindPartial, Label: "Partial"},
	}
}

func creatableRecordTypes() []string {
	return []string{
		"A", "AAAA", "CAA", "CERT", "CNAME", "DNSKEY", "DS", "HTTPS", "LOC", "MX",
		"NAPTR", "NS", "OPENPGPKEY", "PTR", "SMIMEA", "SRV", "SSHFP", "SVCB", "TLSA", "TXT", "URI",
	}
}

func proxyRecordType(recordType string) bool {
	switch recordType {
	case "A", "AAAA", "CNAME":
		return true
	default:
		return false
	}
}

var _ connector.PaginatedReadOperations = (*Client)(nil)
