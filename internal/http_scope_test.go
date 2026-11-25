package internal

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
)

func TestHTTPPolicyBindsRequestsToOperationAndZone(t *testing.T) {
	tests := []struct {
		name   string
		op     abi.Operation
		method string
		parts  []string
		query  url.Values
		want   bool
	}{
		{"account inventory", abi.Probe, http.MethodGet, []string{"zones"}, url.Values{"account.id": {fixtureAccountID}}, true},
		{"unfiltered inventory", abi.Probe, http.MethodGet, []string{"zones"}, nil, false},
		{"zone account check", abi.CreateRecord, http.MethodGet, []string{"zones", fixtureZoneID}, nil, true},
		{"create record", abi.CreateRecord, http.MethodPost, []string{"zones", fixtureZoneID, "dns_records"}, nil, true},
		{"update record", abi.UpdateRecord, http.MethodPatch, []string{"zones", fixtureZoneID, "dns_records", fixtureRecordID}, nil, true},
		{"wrong zone", abi.UpdateRecord, http.MethodPatch, []string{"zones", fixtureAccountID, "dns_records", fixtureRecordID}, nil, false},
		{"wrong record", abi.UpdateRecord, http.MethodPatch, []string{"zones", fixtureZoneID, "dns_records", fixtureAccountID}, nil, false},
		{"write during read", abi.GetRecord, http.MethodPatch, []string{"zones", fixtureZoneID, "dns_records", fixtureRecordID}, nil, false},
		{"DNSSEC write", abi.SetDNSSEC, http.MethodPatch, []string{"zones", fixtureZoneID, "dnssec"}, nil, true},
		{"zone write during DNSSEC", abi.SetDNSSEC, http.MethodPatch, []string{"zones", fixtureZoneID}, nil, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := abi.Request{Operation: test.op, ZoneID: fixtureZoneID, RecordID: fixtureRecordID, Settings: fixtureSettings()}
			request := abi.HTTPRequest{Method: test.method, Parts: test.parts, Query: test.query}
			if allowedRequest(call, request) != test.want {
				t.Fatalf("allowedRequest() = %t, want %t", !test.want, test.want)
			}
		})
	}
}
