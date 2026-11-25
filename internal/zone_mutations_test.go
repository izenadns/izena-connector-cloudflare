package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestCreateZoneUsesConnectionAccount(t *testing.T) {
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodPost, "zones")
		var body struct {
			Name    string            `json:"name"`
			Type    string            `json:"type"`
			Account map[string]string `json:"account"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil || body.Name != "example.test" ||
			body.Type != "full" || body.Account["id"] != fixtureAccountID {
			t.Fatalf("create body = %+v, %v", body, err)
		}
		return zoneResponse(t, testZoneFixture("full")), nil
	}}
	input := connector.ZoneCreateInput{Name: "example.test.", Kind: zoneKindFull}
	zone, err := client.CreateZone(t.Context(), input)
	if err != nil || zone.ID != fixtureZoneID || zone.Name != input.Name || zone.Capabilities == nil {
		t.Fatalf("CreateZone() = %+v, %v", zone, err)
	}
	input.Settings = map[string][]string{"account": {"33333333333333333333333333333333"}}
	if _, err := client.CreateZone(t.Context(), input); !errors.Is(err, connector.ErrValidation) {
		t.Fatalf("CreateZone() with an account override = %v, want validation error", err)
	}
}

func TestUpdateZoneReportsAttemptAndPreviousKind(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		kind      string
		attempted bool
		wantErr   error
	}{
		{name: "enterprise conversion failed", plan: "enterprise", kind: zoneKindPartial, attempted: true, wantErr: connector.ErrUnavailable},
		{name: "non-enterprise conversion", plan: "free", kind: zoneKindPartial, wantErr: connector.ErrUnsupported},
		{name: "same type", plan: "free", kind: zoneKindFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempted := false
			client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
				if req.Method == http.MethodGet {
					assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID)
					wire := testZoneFixture("full")
					wire.Plan = map[string]string{"legacy_id": tt.plan}
					return zoneResponse(t, wire), nil
				}
				assertZoneRequest(t, req, http.MethodPatch, "zones", fixtureZoneID)
				if attempted || string(req.Body) != `{"type":"partial"}` {
					t.Fatalf("patch body = %s after attempted=%v", req.Body, attempted)
				}
				attempted = true
				return abi.HTTPResponse{Status: http.StatusBadGateway}, nil
			}}
			result, err := client.UpdateZone(t.Context(), fixtureZoneID, connector.ZoneUpdateInput{Kind: tt.kind})
			if !errors.Is(err, tt.wantErr) || result.Attempted != tt.attempted || attempted != tt.attempted ||
				result.Previous == nil || result.Previous.Kind != zoneKindFull {
				t.Fatalf("UpdateZone() = %+v, %v, upstream mutation=%v", result, err, attempted)
			}
		})
	}
}

func TestDeleteZoneChecksOwnershipBeforeDeleting(t *testing.T) {
	var methods []string
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		methods = append(methods, req.Method)
		switch req.Method {
		case http.MethodGet:
			assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID)
			return zoneResponse(t, testZoneFixture("full")), nil
		case http.MethodDelete:
			assertZoneRequest(t, req, http.MethodDelete, "zones", fixtureZoneID)
			return zoneResponse(t, map[string]string{"id": fixtureZoneID}), nil
		default:
			t.Fatalf("unexpected HTTP method %s", req.Method)
			return abi.HTTPResponse{}, nil
		}
	}}
	if err := client.DeleteZone(t.Context(), fixtureZoneID); err != nil || len(methods) != 2 ||
		methods[0] != http.MethodGet || methods[1] != http.MethodDelete {
		t.Fatalf("DeleteZone() = %v with HTTP calls %v", err, methods)
	}
}
