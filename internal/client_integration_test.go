//go:build integration

package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestCloudflareFixtureZonesAndDNSSEC(t *testing.T) {
	dispatch := newCloudflareFixture(t)

	if got := dispatch(abi.Probe, nil); got != (connector.Profile{DaemonType: providerName, Version: "v4"}) {
		t.Fatalf("Probe() = %+v", got)
	}
	zones := dispatch(abi.ListZonesPage, connector.PageInput{Limit: 5}).(connector.Page[connector.ZoneMetadata])
	if len(zones.Items) != 1 || zones.Items[0].ID != fixtureZoneID {
		t.Fatalf("ListZonesPage() = %+v", zones)
	}
	gotZone := dispatch(abi.GetZone, nil).(connector.Zone)
	if gotZone.Name != "example.test." || gotZone.Capabilities == nil || !gotZone.Capabilities.WriteRecords || gotZone.Capabilities.WriteRRsets {
		t.Fatalf("GetZone() = %+v", gotZone)
	}
	dnssec := dispatch(abi.GetDNSSEC, nil).(connector.DNSSEC)
	if dnssec.Enabled || dnssec.Active == nil || *dnssec.Active {
		t.Fatalf("GetDNSSEC() = %+v", dnssec)
	}
}

func TestCloudflareFixtureRecordLifecycle(t *testing.T) {
	dispatch := newCloudflareFixture(t)

	proxied := true
	input := connector.IndividualRecordInput{
		Name: "www.example.test.", Type: "A", Content: "192.0.2.1", AutomaticTTL: true,
		Options: map[string]connector.SettingValue{proxiedKey: {Boolean: &proxied}},
	}
	record := dispatch(abi.CreateRecord, input).(connector.IndividualRecord)
	if record.ID != fixtureRecordID || !record.AutomaticTTL || record.Options[proxiedKey].Boolean == nil || !*record.Options[proxiedKey].Boolean {
		t.Fatalf("CreateRecord() = %+v", record)
	}
	records := dispatch(abi.ListRecordsPage, connector.PageInput{Limit: 5}).(connector.Page[connector.IndividualRecord])
	if len(records.Items) != 1 || records.Items[0].ID != fixtureRecordID {
		t.Fatalf("ListRecordsPage() = %+v", records)
	}
	proxied = false
	input.Content = "192.0.2.2"
	input.AutomaticTTL, input.TTL = false, 300
	dispatch(abi.UpdateRecord, input)
	record = dispatch(abi.GetRecord, nil).(connector.IndividualRecord)
	if record.Content != input.Content || record.TTL != 300 || record.AutomaticTTL ||
		record.Options[proxiedKey].Boolean == nil || *record.Options[proxiedKey].Boolean {
		t.Fatalf("GetRecord() after update = %+v", record)
	}
	dispatch(abi.DeleteRecord, nil)
	if remaining := dispatch(abi.ListRecords, nil).([]connector.IndividualRecord); len(remaining) != 0 {
		t.Fatalf("ListRecords() after delete = %+v", remaining)
	}
}

func newCloudflareFixture(t *testing.T) func(abi.Operation, any) any {
	t.Helper()
	const zonePath = "/client/v4/zones/" + fixtureZoneID
	const recordPath = zonePath + "/dns_records"
	const zone = `{"id":"` + fixtureZoneID + `","name":"example.test","type":"full","account":{"id":"` + fixtureAccountID + `"}}`
	var mu sync.Mutex
	var stored json.RawMessage

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "missing credentials or accept header", http.StatusUnauthorized)
			return
		}

		mu.Lock()
		defer mu.Unlock()
		var result json.RawMessage
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/client/v4/zones" && r.URL.Query().Get("account.id") == fixtureAccountID:
			result = json.RawMessage("[" + zone + "]")
		case r.Method == http.MethodGet && r.URL.Path == zonePath:
			result = json.RawMessage(zone)
		case r.Method == http.MethodGet && r.URL.Path == zonePath+"/dnssec":
			result = json.RawMessage(`{"status":"disabled","modified_on":null}`)
		case r.Method == http.MethodPost && r.URL.Path == recordPath,
			r.Method == http.MethodPatch && r.URL.Path == recordPath+"/"+fixtureRecordID:
			if r.Header.Get("Content-Type") != "application/json" {
				http.Error(w, "missing content type", http.StatusBadRequest)
				return
			}
			var record map[string]any
			if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
				http.Error(w, "invalid record", http.StatusBadRequest)
				return
			}
			record["id"] = fixtureRecordID
			record["proxiable"] = true
			raw, err := json.Marshal(record)
			if err != nil {
				http.Error(w, "invalid fixture", http.StatusInternalServerError)
				return
			}
			stored, result = raw, raw
		case r.Method == http.MethodGet && r.URL.Path == recordPath:
			query := r.URL.Query()
			if !query.Has("cursor") || query.Get("cursor") != "" || query.Has("page") || query.Has("prev_cursor") {
				http.Error(w, "expected initial cursor query", http.StatusBadRequest)
				return
			}
			result = json.RawMessage("[]")
			if stored != nil {
				result = json.RawMessage("[" + string(stored) + "]")
			}
		case r.Method == http.MethodGet && r.URL.Path == recordPath+"/"+fixtureRecordID && stored != nil:
			result = stored
		case r.Method == http.MethodDelete && r.URL.Path == recordPath+"/"+fixtureRecordID && stored != nil:
			stored = nil
			result = json.RawMessage(`{"id":"` + fixtureRecordID + `"}`)
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Success bool            `json:"success"`
			Result  json.RawMessage `json:"result"`
		}{true, result})
	}))
	t.Cleanup(upstream.Close)

	// Stand in for the network binding Izena supplies to the connector.
	host := func(ctx context.Context, call abi.HTTPRequest) (abi.HTTPResponse, error) {
		request, err := http.NewRequestWithContext(ctx, call.Method,
			upstream.URL+"/client/v4/"+strings.Join(call.Parts, "/"), bytes.NewReader(call.Body))
		if err != nil {
			return abi.HTTPResponse{}, err
		}
		request.URL.RawQuery = call.Query.Encode()
		request.Header = call.Headers.Clone()
		request.Header.Set("Authorization", "Bearer fixture-token")
		response, err := upstream.Client().Do(request)
		if err != nil {
			return abi.HTTPResponse{}, err
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(response.Body, int64(call.Limit)))
		return abi.HTTPResponse{Status: response.StatusCode, Body: body}, err
	}
	return func(operation abi.Operation, input any) any {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		value, err := Dispatch(t.Context(), abi.Request{
			Operation: operation, Settings: fixtureSettings(), ZoneID: fixtureZoneID, RecordID: fixtureRecordID, Input: body,
		}, host)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		return value
	}
}
