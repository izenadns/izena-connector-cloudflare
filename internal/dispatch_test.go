package internal

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestDispatchCloudflareOperation(t *testing.T) {
	calls := 0
	var provider Provider
	exchange := func(_ context.Context, request abi.HTTPRequest) (abi.HTTPResponse, error) {
		calls++
		if request.Method != http.MethodGet || !reflect.DeepEqual(request.Parts, []string{"zones"}) ||
			request.Query.Get("account.id") != fixtureAccountID || request.Headers.Get("Accept") != "application/json" {
			t.Fatalf("unexpected probe request: %+v", request)
		}
		return abi.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"success":true,"result":[]}`)}, nil
	}
	capabilities, _ := (&Client{}).Capabilities(t.Context())
	settings := fixtureSettings()
	tests := []struct {
		op    abi.Operation
		input string
		want  any
		err   error
	}{
		{op: abi.Describe, want: provider.Manifest()},
		{op: abi.PrepareSettings, want: connector.PreparedSettings{Settings: settings, TargetKey: fixtureAccountID}},
		{op: abi.Probe, want: connector.Profile{DaemonType: providerName, Version: "v4"}},
		{op: abi.Capabilities, want: capabilities},
		{op: abi.ListZonesPage, input: `[`, err: connector.ErrValidation},
		{op: abi.ListRecordsPage, input: `[`, err: connector.ErrValidation},
		{op: abi.CreateRecord, input: `[`, err: connector.ErrValidation},
		{op: abi.SetDNSSEC, input: "null", err: connector.ErrValidation},
		{op: abi.GetRRset, err: connector.ErrUnsupported},
		{op: abi.ListRRsetsPage, err: connector.ErrUnsupported},
		{op: abi.SubmitRecordBatch, err: connector.ErrUnsupported},
		{op: abi.RectifyZone, err: connector.ErrUnsupported},
		{op: abi.NotifyZone, err: connector.ErrUnsupported},
		{op: abi.AxfrZone, err: connector.ErrUnsupported},
		{op: "unknown_operation", err: connector.ErrUnsupported},
	}
	for _, test := range tests {
		t.Run(string(test.op), func(t *testing.T) {
			got, err := Dispatch(t.Context(), abi.Request{
				Operation: test.op, Input: []byte(test.input), Settings: settings, ZoneID: fixtureZoneID,
			}, exchange)
			if !errors.Is(err, test.err) {
				t.Fatalf("Dispatch() error = %v, want %v", err, test.err)
			}
			if test.want != nil && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Dispatch() = %#v, want %#v", got, test.want)
			}
		})
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want one probe request", calls)
	}
}
