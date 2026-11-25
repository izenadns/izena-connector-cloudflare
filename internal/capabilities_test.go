package internal

import (
	"slices"
	"testing"

	"github.com/izenadns/izena-connector-contract/connector"
)

func TestCapabilitiesAdvertiseIndividualRecords(t *testing.T) {
	got, err := (&Client{}).Capabilities(t.Context())
	if err != nil || got.Provider != providerID || got.RecordModel != connector.RecordModelIndividual ||
		!slices.Contains(got.RecordTypes, "A") || len(got.ZoneKinds) != 2 ||
		!got.Pagination.Zones || !got.Pagination.Records || got.Pagination.RRsets ||
		len(got.Actions) != 0 || got.DNSSEC.Signing != connector.SigningSupported {
		t.Fatalf("Capabilities() = %+v, %v", got, err)
	}
}

func TestZoneCapabilitiesExposeCloudflareRecordOptions(t *testing.T) {
	capabilities := zoneCapabilities(zoneWire{Type: zoneKindFull})
	if !capabilities.WriteRecords || capabilities.WriteRRsets || !capabilities.AutomaticTTL ||
		len(capabilities.ManagedRecordTypes) != 0 || len(capabilities.RecordOptions) != 1 || capabilities.RecordOptions[0].Key != proxiedKey ||
		capabilities.RecordOptions[0].Kind != connector.SettingBoolean {
		t.Fatalf("zoneCapabilities() = %+v", capabilities)
	}
	for _, kind := range []string{zoneKindSecondary, zoneKindInternal} {
		capabilities := zoneCapabilities(zoneWire{Type: kind})
		if capabilities.WriteRecords || len(capabilities.ManagedRecordTypes) != 0 {
			t.Fatalf("%s zoneCapabilities() = %+v", kind, capabilities)
		}
	}
}
