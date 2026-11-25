package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestZoneReadsUseOpaqueIDs(t *testing.T) {
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID)
		return zoneResponse(t, testZoneFixture("full")), nil
	}}
	zone, err := client.GetZone(t.Context(), fixtureZoneID)
	if err != nil || zone.ID != fixtureZoneID || zone.Name != "example.test." ||
		zone.Kind != zoneKindFull || zone.Capabilities == nil || len(zone.RRsets) != 0 {
		t.Fatalf("GetZone() = %+v, %v", zone, err)
	}
	metadata, err := client.GetZoneMetadata(t.Context(), fixtureZoneID)
	if err != nil || metadata.Name != zone.Name || metadata.ID != zone.ID || metadata.Kind != zone.Kind {
		t.Fatalf("GetZoneMetadata() = %+v, %v", metadata, err)
	}
	if _, err := client.GetZone(t.Context(), "example.test."); !errors.Is(err, connector.ErrValidation) {
		t.Fatalf("GetZone() by name = %v, want validation failure", err)
	}
}

func TestZoneReadsRejectOtherAccounts(t *testing.T) {
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodGet, "zones", fixtureZoneID)
		wire := testZoneFixture("full")
		wire.Account["id"] = "33333333333333333333333333333333"
		return zoneResponse(t, wire), nil
	}}
	if _, err := client.GetZone(t.Context(), fixtureZoneID); !errors.Is(err, connector.ErrNotFound) {
		t.Fatalf("GetZone() = %v, want not found", err)
	}
	if err := client.DeleteZone(t.Context(), fixtureZoneID); !errors.Is(err, connector.ErrNotFound) {
		t.Fatalf("DeleteZone() = %v, want not found before sending a mutation", err)
	}
}

func TestListZonesPageRespectsSmallLimits(t *testing.T) {
	var upstreamPages []string
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodGet, "zones")
		if req.Query.Get("account.id") != fixtureAccountID || req.Query.Get("per_page") != "5" ||
			req.Query.Get("type") != "full,partial,secondary,internal" {
			t.Fatalf("list query = %v", req.Query)
		}
		upstreamPages = append(upstreamPages, req.Query.Get("page"))
		items := make([]zoneFixture, 0, 5)
		for _, name := range []string{"a", "b", "c", "d", "e"} {
			zone := testZoneFixture("full")
			zone.Name = name + ".test"
			items = append(items, zone)
		}
		if req.Query.Get("page") == "2" {
			items = []zoneFixture{testZoneFixture("internal")}
		}
		return zonePageResponse(t, items, 2), nil
	}}
	input := connector.PageInput{Limit: 2}
	var names []string
	for range 4 {
		page, err := client.ListZonesPage(t.Context(), input)
		if err != nil || len(page.Items) > input.Limit {
			t.Fatalf("ListZonesPage() = %+v, %v", page, err)
		}
		for _, item := range page.Items {
			names = append(names, item.Name)
		}
		input.Cursor = page.NextCursor
	}
	if input.Cursor != "" || !reflect.DeepEqual(names, []string{"a.test.", "b.test.", "c.test.", "d.test.", "e.test.", "example.test."}) ||
		!reflect.DeepEqual(upstreamPages, []string{"1", "1", "1", "2"}) {
		t.Fatalf("collected %v from %v with final cursor %q", names, upstreamPages, input.Cursor)
	}
}

func TestListZonesCollectsPagesForAccount(t *testing.T) {
	var pages []string
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodGet, "zones")
		pages = append(pages, req.Query.Get("page"))
		if req.Query.Get("per_page") != "50" || req.Query.Get("account.id") != fixtureAccountID {
			t.Fatalf("list query = %v", req.Query)
		}
		if req.Query.Get("page") == "1" {
			return zonePageResponse(t, []zoneFixture{testZoneFixture("full")}, 2), nil
		}
		return zoneResponse(t, []zoneFixture{testZoneFixture("secondary")}), nil
	}}
	zones, err := client.ListZones(t.Context())
	if err != nil || len(zones) != 2 || zones[1].Kind != zoneKindSecondary || !reflect.DeepEqual(pages, []string{"1", "2"}) {
		t.Fatalf("ListZones() = %+v, %v after pages %v", zones, err, pages)
	}
}

func TestListZonesPageStopsAtEmptyFilteredResults(t *testing.T) {
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodGet, "zones")
		body := `{"success":true,"result":[],"result_info":{"total_pages":20,"total_count":1000}}`
		return abi.HTTPResponse{Status: http.StatusOK, Body: []byte(body)}, nil
	}}
	page, err := client.ListZonesPage(t.Context(), connector.PageInput{Limit: 2, Cursor: "1:5/4"})
	if err != nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("ListZonesPage() = %+v, %v", page, err)
	}
}

func TestListZonesPageIgnoresUnfilteredCount(t *testing.T) {
	const body = `{"success":true,"result":[{"id":"` + fixtureZoneID + `","name":"example.test","type":"full","account":{"id":"` + fixtureAccountID + `"}}],"result_info":{"total_count":1000}}`
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, req abi.HTTPRequest) (abi.HTTPResponse, error) {
		assertZoneRequest(t, req, http.MethodGet, "zones")
		return abi.HTTPResponse{Status: http.StatusOK, Body: []byte(body)}, nil
	}}
	page, err := client.ListZonesPage(t.Context(), connector.PageInput{Limit: 5})
	if err != nil || len(page.Items) != 1 || page.NextCursor != "" {
		t.Fatalf("ListZonesPage() = %+v, %v", page, err)
	}
}

// The HTTP fixtures describe upstream JSON independently of the client decoder.
type zoneFixture struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Account map[string]string `json:"account"`
	Plan    map[string]string `json:"plan,omitempty"`
}

func testZoneFixture(kind string) zoneFixture {
	return zoneFixture{
		ID: fixtureZoneID, Name: "example.test", Type: kind,
		Account: map[string]string{"id": fixtureAccountID},
	}
}

func zonePageResponse(t *testing.T, items []zoneFixture, totalPages int) abi.HTTPResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"success": true, "result": items,
		"result_info": map[string]int{"total_pages": totalPages},
	})
	if err != nil {
		t.Fatal(err)
	}
	return abi.HTTPResponse{Status: http.StatusOK, Body: body}
}

func zoneResponse(t *testing.T, result any) abi.HTTPResponse {
	t.Helper()
	body, err := json.Marshal(struct {
		Success bool `json:"success"`
		Result  any  `json:"result"`
	}{Success: true, Result: result})
	if err != nil {
		t.Fatal(err)
	}
	return abi.HTTPResponse{Status: http.StatusOK, Body: body}
}

func assertZoneRequest(t *testing.T, req abi.HTTPRequest, method string, parts ...string) {
	t.Helper()
	if req.Method != method || !reflect.DeepEqual(req.Parts, parts) {
		t.Fatalf("HTTP request = %s %v, want %s %v", req.Method, req.Parts, method, parts)
	}
}
