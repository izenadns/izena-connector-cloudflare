package internal

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestProbeChecksTheCloudflareZoneInventory(t *testing.T) {
	client := Client{accountID: fixtureAccountID, exchange: func(_ context.Context, request abi.HTTPRequest) (abi.HTTPResponse, error) {
		if request.Method != http.MethodGet || !slices.Equal(request.Parts, []string{"zones"}) ||
			request.Query.Get("account.id") != fixtureAccountID || request.Query.Get("per_page") != "5" {
			t.Fatalf("unexpected probe request: %+v", request)
		}
		return abi.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"success":true,"result":[]}`)}, nil
	}}
	want := connector.Profile{DaemonType: providerName, Version: "v4"}
	if got, err := client.Probe(t.Context()); got != want || err != nil {
		t.Fatalf("Probe() = %+v, %v, want %+v", got, err, want)
	}
}
