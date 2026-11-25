package internal

import (
	"errors"
	"testing"

	"github.com/izenadns/izena-connector-contract/connector"
)

func TestIndividualRecordConnectorRejectsRRsetOperations(t *testing.T) {
	client := Client{}
	input := connector.RRsetReplaceInput{Name: "www.example.test.", Type: "A", TTL: 300}
	if _, err := client.GetRRset(t.Context(), fixtureZoneID, input.Name, input.Type); !errors.Is(err, connector.ErrUnsupported) {
		t.Fatalf("GetRRset() = %v, want unsupported", err)
	}
	if err := client.ReplaceRRset(t.Context(), fixtureZoneID, input); !errors.Is(err, connector.ErrUnsupported) {
		t.Fatalf("ReplaceRRset() = %v, want unsupported", err)
	}
	if err := client.DeleteRRset(t.Context(), fixtureZoneID, input.Name, input.Type); !errors.Is(err, connector.ErrUnsupported) {
		t.Fatalf("DeleteRRset() = %v, want unsupported", err)
	}
}
