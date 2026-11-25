package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

const (
	recordTestCursor    = "cf+cursor/=&"
	recordTestAccount   = "00112233445566778899aabbccddeeff"
	recordTestZone      = "112233445566778899aabbccddeeff00"
	recordTestID        = "2233445566778899aabbccddeeff0011"
	recordTestOtherID   = "33445566778899aabbccddeeff001122"
	recordTestZonePath  = "zones/" + recordTestZone
	recordTestListPath  = recordTestZonePath + "/dns_records"
	recordTestItemPath  = recordTestListPath + "/" + recordTestID
	recordTestZoneWire  = `{"id":"` + recordTestZone + `","name":"example.test","type":"full","account":{"id":"` + recordTestAccount + `"}}`
	recordTestAddress   = `{"id":"` + recordTestID + `","name":"www.example.test","type":"A","ttl":1,"content":"192.0.2.1","proxiable":true,"proxied":true,"comment":"managed","comment_modified_on":"2024-01-01T00:00:00Z"}`
	recordTestOtherAddr = `{"id":"` + recordTestOtherID + `","name":"www.example.test","type":"A","ttl":300,"content":"192.0.2.2","proxiable":true,"proxied":false}`
)

func TestListRecordsPreservesIdentitiesAcrossPages(t *testing.T) {
	requests := 0
	client := recordTestClient(t, func(request abi.HTTPRequest) abi.HTTPResponse {
		requests++
		if !request.Query.Has("cursor") || request.Query.Has("page") || request.Query.Get("per_page") != "100" {
			t.Fatalf("unexpected record query %s", request.Query.Encode())
		}
		switch request.Query.Get("cursor") {
		case "":
			return recordTestResponse(`[`+recordTestAddress+`]`, `,"result_info":{"cursor":"`+recordTestCursor+`"}`)
		case recordTestCursor:
			return recordTestResponse(`[`+recordTestOtherAddr+`]`, "")
		default:
			t.Fatalf("unexpected cursor %q", request.Query.Get("cursor"))
			return abi.HTTPResponse{}
		}
	})
	records, err := client.ListRecords(t.Context(), recordTestZone)
	if err != nil || len(records) != 2 || requests != 2 {
		t.Fatalf("ListRecords() = %#v, %v after %d page requests", records, err, requests)
	}
	first, second := records[0], records[1]
	if first.ID != recordTestID || second.ID != recordTestOtherID || first.Name != "www.example.test." || first.Name != second.Name || first.Type != second.Type {
		t.Fatalf("record identities = %#v", records)
	}
	if !first.AutomaticTTL || first.TTL != 0 || first.Options[proxiedKey].Boolean == nil || !*first.Options[proxiedKey].Boolean ||
		second.AutomaticTTL || second.TTL != 300 || second.Options[proxiedKey].Boolean == nil || *second.Options[proxiedKey].Boolean {
		t.Fatalf("record settings = %#v", records)
	}
	if len(first.Comments) != 1 || first.Comments[0].Content != "managed" || !first.Comments[0].ModifiedAt.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("comments = %#v", first.Comments)
	}
}

func TestListRecordsPageContinuesAfterDeletion(t *testing.T) {
	records := []string{recordTestAddress, recordTestOtherAddr}
	requestCount := 0
	client := recordTestClient(t, func(request abi.HTTPRequest) abi.HTTPResponse {
		requestCount++
		if request.Method != http.MethodGet || strings.Join(request.Parts, "/") != recordTestListPath ||
			request.Query.Get("per_page") != "1" || !request.Query.Has("cursor") || request.Query.Has("page") {
			t.Fatalf("request = %#v", request)
		}
		switch request.Query.Get("cursor") {
		case "":
			return recordTestResponse(`[`+records[0]+`]`, `,"result_info":{"cursor":"`+recordTestCursor+`"}`)
		case recordTestCursor:
			if len(records) != 1 {
				t.Fatalf("expected one surviving record, got %d", len(records))
			}
			return recordTestResponse(`[`+records[0]+`]`, "")
		default:
			t.Fatalf("unexpected cursor %q", request.Query.Get("cursor"))
			return abi.HTTPResponse{}
		}
	})
	first, err := client.ListRecordsPage(t.Context(), recordTestZone, connector.PageInput{Limit: 1})
	if err != nil || first.NextCursor != recordTestCursor || len(first.Items) != 1 || first.Items[0].ID != recordTestID {
		t.Fatalf("first page = %#v, %v", first, err)
	}

	// Deleting the first record moves the survivor to the first offset.
	records = records[1:]
	last, err := client.ListRecordsPage(t.Context(), recordTestZone, connector.PageInput{Limit: 1, Cursor: first.NextCursor})
	if err != nil || last.NextCursor != "" || len(last.Items) != 1 || last.Items[0].ID != recordTestOtherID || requestCount != 2 {
		t.Fatalf("last page = %#v, %v after %d requests", last, err, requestCount)
	}
}

func TestIndividualRecordMutationsUseCloudflareRecordIDs(t *testing.T) {
	proxied, dnsOnly := true, false
	address := connector.IndividualRecordInput{
		Name: "www.example.test.", Type: "A", Content: "192.0.2.1", TTL: 120,
		Options: map[string]connector.SettingValue{proxiedKey: {Boolean: &proxied}},
	}
	client := recordTestClient(t, func(request abi.HTTPRequest) abi.HTTPResponse {
		wantPath := recordTestItemPath
		if request.Method == http.MethodPost {
			wantPath = recordTestListPath
		}
		if strings.Join(request.Parts, "/") != wantPath {
			t.Fatalf("request = %#v", request)
		}
		switch request.Method {
		case http.MethodGet:
			return recordTestResponse(recordTestAddress, "")
		case http.MethodDelete:
			return recordTestResponse(`{"id":"`+recordTestID+`"}`, "")
		case http.MethodPost, http.MethodPatch:
			var body recordMutationWire
			if err := json.Unmarshal(request.Body, &body); err != nil {
				t.Fatalf("decode record mutation: %v", err)
			}
			wantProxy, wantTTL := request.Method == http.MethodPost, int64(120)
			if wantProxy {
				wantTTL = 1
			}
			if body.Name != "www.example.test" || body.Type != "A" || body.Content != address.Content ||
				body.Proxied == nil || *body.Proxied != wantProxy || body.TTL != wantTTL {
				t.Fatalf("mutation = %#v", body)
			}
			if wantProxy {
				return recordTestResponse(recordTestAddress, "")
			}
			return recordTestResponse(strings.ReplaceAll(strings.ReplaceAll(recordTestAddress, `"proxied":true`, `"proxied":false`), `"ttl":1`, `"ttl":120`), "")
		default:
			t.Fatalf("unexpected method %s", request.Method)
			return abi.HTTPResponse{}
		}
	})

	created, err := client.CreateRecord(t.Context(), recordTestZone, address)
	if err != nil || created.ID != recordTestID || !created.AutomaticTTL {
		t.Fatalf("CreateRecord() = %#v, %v", created, err)
	}
	record, err := client.GetRecord(t.Context(), recordTestZone, recordTestID)
	if err != nil || record.Content != address.Content {
		t.Fatalf("GetRecord() = %#v, %v", record, err)
	}
	address.Options[proxiedKey] = connector.SettingValue{Boolean: &dnsOnly}
	updated, err := client.UpdateRecord(t.Context(), recordTestZone, recordTestID, address)
	if err != nil || updated.AutomaticTTL || updated.TTL != 120 {
		t.Fatalf("UpdateRecord() = %#v, %v", updated, err)
	}
	if err := client.DeleteRecord(t.Context(), recordTestZone, recordTestID); err != nil {
		t.Fatalf("DeleteRecord() error = %v", err)
	}
}

func TestRecordMutationsRejectReadOnlyZones(t *testing.T) {
	client := Client{
		accountID: recordTestAccount,
		exchange: func(_ context.Context, request abi.HTTPRequest) (abi.HTTPResponse, error) {
			if request.Method != http.MethodGet || strings.Join(request.Parts, "/") != recordTestZonePath {
				t.Fatalf("unexpected request for read-only zone: %#v", request)
			}
			return recordTestResponse(strings.ReplaceAll(recordTestZoneWire, `"full"`, `"secondary"`), ""), nil
		},
	}
	input := connector.IndividualRecordInput{Name: "www.example.test.", Type: "A", Content: "192.0.2.1", TTL: 120}
	_, createErr := client.CreateRecord(t.Context(), recordTestZone, input)
	_, updateErr := client.UpdateRecord(t.Context(), recordTestZone, recordTestID, input)
	deleteErr := client.DeleteRecord(t.Context(), recordTestZone, recordTestID)
	for _, err := range []error{createErr, updateErr, deleteErr} {
		if !errors.Is(err, connector.ErrUnsupported) {
			t.Fatalf("read-only zone returned %v, want unsupported", err)
		}
	}
}

func TestRecordProxyInputValidation(t *testing.T) {
	enabled, label := true, "true"
	for _, test := range []struct {
		name, recordType string
		options          map[string]connector.SettingValue
		want             error
	}{
		{name: "unsupported type", recordType: "TXT", options: map[string]connector.SettingValue{proxiedKey: {Boolean: &enabled}}, want: connector.ErrUnsupported},
		{name: "unsupported key", recordType: "A", options: map[string]connector.SettingValue{"foo": {Boolean: &enabled}}, want: connector.ErrUnsupported},
		{name: "wrong type", recordType: "A", options: map[string]connector.SettingValue{proxiedKey: {String: &label}}, want: connector.ErrValidation},
		{name: "two values", recordType: "A", options: map[string]connector.SettingValue{proxiedKey: {Boolean: &enabled, String: &label}}, want: connector.ErrValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := encodeRecord(connector.IndividualRecordInput{
				Name: "www.example.test.", Type: test.recordType, Content: "192.0.2.1", TTL: 300, Options: test.options,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("encodeRecord() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEncodeStructuredRecordContent(t *testing.T) {
	for _, test := range []struct {
		recordType string
		content    string
		want       string
	}{
		{recordType: "CAA", content: `0 issue "ca.example.test; validationmethods=dns-01"`, want: `{"data":{"flags":0,"tag":"issue","value":"ca.example.test; validationmethods=dns-01"}}`},
		{recordType: "CERT", content: "1 42 8 dGVzdA==", want: `{"data":{"algorithm":8,"certificate":"dGVzdA==","key_tag":42,"type":1}}`},
		{recordType: "DNSKEY", content: "257 3 13 dGVzdA==", want: `{"data":{"algorithm":13,"flags":257,"protocol":3,"public_key":"dGVzdA=="}}`},
		{recordType: "DS", content: "42 13 2 0123", want: `{"data":{"algorithm":13,"digest":"0123","digest_type":2,"key_tag":42}}`},
		{recordType: "HTTPS", content: `1 . alpn="h2,h3"`, want: `{"data":{"priority":1,"target":".","value":"alpn=\"h2,h3\""}}`},
		{recordType: "LOC", content: "37 46 30.12 N 122 25 9.9 W 80m 1m 10000m 10m", want: `{"data":{"altitude":80,"lat_degrees":37,"lat_direction":"N","lat_minutes":46,"lat_seconds":30.12,"long_degrees":122,"long_direction":"W","long_minutes":25,"long_seconds":9.9,"precision_horz":10000,"precision_vert":10,"size":1}}`},
		{recordType: "LOC", content: "37 n 122 25 W 80m", want: `{"data":{"altitude":80,"lat_degrees":37,"lat_direction":"N","lat_minutes":0,"lat_seconds":0,"long_degrees":122,"long_direction":"W","long_minutes":25,"long_seconds":0}}`},
		{recordType: "MX", content: "0 mail.example.test.", want: `{"content":"mail.example.test.","priority":0}`},
		{recordType: "NAPTR", content: `100 10 "U" "E2U+sip" "!^.*$!sip:alice@example.test!" .`, want: `{"data":{"flags":"U","order":100,"preference":10,"regex":"!^.*$!sip:alice@example.test!","replacement":".","service":"E2U+sip"}}`},
		{recordType: "SMIMEA", content: "3 1 1 0123", want: `{"data":{"certificate":"0123","matching_type":1,"selector":1,"usage":3}}`},
		{recordType: "SRV", content: "5 10 443 srv.example.test.", want: `{"data":{"port":443,"priority":5,"target":"srv.example.test.","weight":10}}`},
		{recordType: "SSHFP", content: "4 2 deadbeef", want: `{"data":{"algorithm":4,"fingerprint":"deadbeef","type":2}}`},
		{recordType: "SVCB", content: "0 svc.example.test.", want: `{"data":{"priority":0,"target":"svc.example.test."}}`},
		{recordType: "TLSA", content: "3 1 1 0123", want: `{"data":{"certificate":"0123","matching_type":1,"selector":1,"usage":3}}`},
		{recordType: "URI", content: `5 10 "https://example.test/x"`, want: `{"data":{"target":"https://example.test/x","weight":10},"priority":5}`},
	} {
		t.Run(test.recordType, func(t *testing.T) {
			body := recordMutationWire{Type: test.recordType, Content: test.content}
			if err := encodeRecordContent(&body); err != nil {
				t.Fatalf("encodeRecordContent() error = %v", err)
			}
			wire, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal record: %v", err)
			}
			var got, want map[string]any
			if err := json.Unmarshal(wire, &got); err != nil {
				t.Fatalf("decode wire content: %v", err)
			}
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatalf("decode expected fixture: %v", err)
			}
			delete(got, "name")
			delete(got, "type")
			delete(got, "ttl")
			delete(got, "comment")
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("mutation content = %#v, want %#v", got, want)
			}
		})
	}
}

func TestEncodeRecordContentRejectsInvalidRData(t *testing.T) {
	for _, test := range []struct {
		recordType string
		content    string
	}{
		{recordType: "MX", content: "mail.example.test."},
		{recordType: "SRV", content: "5 10 65536 srv.example.test."},
		{recordType: "CAA", content: `0 issue "unclosed`},
		{recordType: "CAA", content: `0 issue "closed" trailing`},
		{recordType: "LOC", content: "37 46 NaN N 122 25 9.9 W 80m"},
	} {
		t.Run(test.recordType+"/"+test.content, func(t *testing.T) {
			body := recordMutationWire{Type: test.recordType, Content: test.content}
			if err := encodeRecordContent(&body); !errors.Is(err, connector.ErrValidation) {
				t.Fatalf("encodeRecordContent() = %v, want validation error", err)
			}
		})
	}
}

func TestDecodeMailExchangePriority(t *testing.T) {
	priority := uint16(0)
	record, err := decodeRecord(recordWire{
		ID: recordTestID, Type: "MX", Name: "example.test", TTL: 300,
		Priority: &priority, Content: "mail.example.test.",
	}, recordTestID)
	if err != nil || record.Content != "0 mail.example.test." {
		t.Fatalf("decodeRecord() = %#v, %v", record, err)
	}
}

func TestCloudflareRecordContentRoundTrips(t *testing.T) {
	for _, test := range []struct {
		recordType string
		wire       string
		content    string
	}{
		{"SRV", `{"content":"5 5223 server.example.test","priority":10,"data":{"priority":10,"weight":5,"port":5223,"target":"server.example.test"}}`, "10 5 5223 server.example.test"},
		{"SRV", `{"content":"5 5223 server.example.test","priority":10}`, "10 5 5223 server.example.test"},
		{"CAA", `{"data":{"flags":0,"tag":"issue","value":"ca.example.test"}}`, `0 issue "ca.example.test"`},
		{"URI", `{"priority":0,"data":{"weight":5,"target":"https://example.test/a"}}`, `0 5 "https://example.test/a"`},
		{"TXT", `{"content":"\"value with spaces\" \"second string\""}`, `"value with spaces" "second string"`},
	} {
		t.Run(test.recordType+test.wire, func(t *testing.T) {
			wire := recordWire{ID: recordTestID, Name: "example.test", TTL: 300, Type: test.recordType}
			if err := json.Unmarshal([]byte(test.wire), &wire); err != nil {
				t.Fatal(err)
			}
			record, err := decodeRecord(wire, recordTestID)
			if err != nil || record.Content != test.content {
				t.Fatalf("decodeRecord() = %q, %v, want %q", record.Content, err, test.content)
			}
			if _, err := encodeRecord(record.IndividualRecordInput); err != nil {
				t.Fatalf("encodeRecord() could not edit the decoded record: %v", err)
			}
		})
	}
}

func TestEncodePlainTXTContent(t *testing.T) {
	body := recordMutationWire{Type: "TXT", Content: `value="example"`}
	if err := encodeRecordContent(&body); err != nil || body.Content != `"value=\"example\""` {
		t.Fatalf("encodeRecordContent() = %q, %v", body.Content, err)
	}
}

func TestRoundTripSparseLOCData(t *testing.T) {
	var wire recordWire
	if err := json.Unmarshal([]byte(`{"type":"LOC","data":{"lat_degrees":37,"lat_direction":"N","long_degrees":122,"long_direction":"W","altitude":80,"precision_vert":20}}`), &wire); err != nil {
		t.Fatal(err)
	}
	wire.ID, wire.Name, wire.TTL = recordTestID, "example.test", 300
	record, err := decodeRecord(wire, recordTestID)
	if err != nil || record.Content != "37 0 0 N 122 0 0 W 80 1 10000 20" {
		t.Fatalf("decodeRecord() = %q, %v", record.Content, err)
	}
	mutation, err := encodeRecord(record.IndividualRecordInput)
	if err != nil || mutation.Data["size"] != float64(1) || mutation.Data["precision_horz"] != float64(10000) ||
		mutation.Data["precision_vert"] != float64(20) {
		t.Fatalf("encodeRecord() = %+v, %v", mutation, err)
	}
}

func recordTestClient(t *testing.T, records func(abi.HTTPRequest) abi.HTTPResponse) *Client {
	t.Helper()
	return &Client{
		accountID: recordTestAccount,
		exchange: func(_ context.Context, request abi.HTTPRequest) (abi.HTTPResponse, error) {
			if request.Method == http.MethodGet && strings.Join(request.Parts, "/") == recordTestZonePath {
				return recordTestResponse(recordTestZoneWire, ""), nil
			}
			if !strings.HasPrefix(strings.Join(request.Parts, "/"), recordTestListPath) {
				t.Fatalf("unexpected route: %#v", request.Parts)
			}
			return records(request), nil
		},
	}
}

func recordTestResponse(result, metadata string) abi.HTTPResponse {
	return abi.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"success":true,"result":` + result + metadata + `}`)}
}
