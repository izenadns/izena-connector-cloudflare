package internal

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestGetDNSSECMapsConfigurationAndPublicKey(t *testing.T) {
	tests := []struct {
		status     string
		modifiedOn string
		enabled    bool
		active     bool
	}{
		{status: "active", enabled: true, active: true},
		{status: "pending", enabled: true, active: true},
		{status: "pending-disabled", active: true},
		{status: "disabled", modifiedOn: "2026-01-01T00:00:00Z", active: true},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
				if len(req.Parts) == 2 {
					assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID)
					return zoneResponse(t, testZoneFixture("full")), nil
				}
				assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID, "dnssec")
				result := map[string]any{
					"status": tt.status, "algorithm": "13", "flags": 257, "key_tag": 2371,
					"ds": "2371 13 2 ABCD", "public_key": "YWJj",
				}
				if tt.modifiedOn != "" {
					result["modified_on"] = tt.modifiedOn
				}
				return zoneResponse(t, result), nil
			}}
			result, err := client.GetDNSSEC(t.Context(), fixtureZoneID)
			if err != nil || result.Enabled != tt.enabled || result.Active == nil || *result.Active != tt.active ||
				len(result.Keys) != 1 || !result.Keys[0].Active || result.Keys[0].KeyTag != 2371 || result.Keys[0].DNSKEY != "257 3 13 YWJj" ||
				len(result.Keys[0].DS) != 1 || result.Keys[0].DS[0] != "2371 13 2 ABCD" {
				t.Fatalf("GetDNSSEC() = %+v, %v", result, err)
			}
		})
	}
}

func TestDecodeDisabledDNSSECWithDeletedOrUnknownSigning(t *testing.T) {
	deleted, err := decodeDNSSEC(dnssecWire{Status: "disabled", ModifiedOn: []byte("null")})
	if err != nil || deleted.Enabled || deleted.Active == nil || *deleted.Active || len(deleted.Keys) != 0 {
		t.Fatalf("deleted DNSSEC = %+v, %v", deleted, err)
	}
	unknown, err := decodeDNSSEC(dnssecWire{Status: "disabled"})
	if err != nil || unknown.Enabled || unknown.Active != nil {
		t.Fatalf("unknown signing status = %+v, %v", unknown, err)
	}
}

func TestSetDNSSECOnlyChangesEnabledStatus(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		enabled bool
		body    string
		wantErr error
	}{
		{name: "enable full zone", kind: zoneKindFull, enabled: true, body: `{"status":"active"}`},
		{name: "disable full zone", kind: zoneKindFull, body: `{"status":"disabled"}`},
		{name: "secondary signing is conditional", kind: zoneKindSecondary, enabled: true, wantErr: connector.ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patches int
			client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
				if req.Method == http.MethodGet {
					assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID)
					return zoneResponse(t, testZoneFixture(tt.kind)), nil
				}
				assertZoneRequest(t, req, http.MethodPatch, "zones", fixtureZoneID, "dnssec")
				patches++
				if string(req.Body) != tt.body {
					t.Fatalf("PATCH body = %s, want %s", req.Body, tt.body)
				}
				return zoneResponse(t, nil), nil
			}}
			if err := client.SetDNSSEC(t.Context(), fixtureZoneID, tt.enabled); !errors.Is(err, tt.wantErr) ||
				tt.body == "" && patches != 0 || tt.body != "" && patches != 1 {
				t.Fatalf("SetDNSSEC() = %v with %d patches", err, patches)
			}
		})
	}
}
