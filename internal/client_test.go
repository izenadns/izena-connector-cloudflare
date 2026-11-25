package internal

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

const (
	fixtureAccountID = "0123456789abcdef0000000000000001"
	fixtureZoneID    = "0123456789abcdef0000000000000002"
	fixtureRecordID  = "0123456789abcdef0000000000000003"
)

func fixtureSettings() map[string]string {
	return map[string]string{baseURLKey: baseURL, accountIDKey: fixtureAccountID}
}

func TestRequestClassifiesCloudflareResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
		code   string
	}{
		{"authentication", http.StatusForbidden, "", connector.ErrAuthentication, "authentication_failed"},
		{"missing", http.StatusNotFound, "", connector.ErrNotFound, "not_found"},
		{"rate limited", http.StatusTooManyRequests, "", connector.ErrUnavailable, "rate_limited"},
		{"unavailable", http.StatusServiceUnavailable, "", connector.ErrUnavailable, "unavailable"},
		{"failed envelope", http.StatusOK, `{"success":false,"errors":[{"code":81053,"message":"upstream text"}]}`, connector.ErrValidation, "validation_failed"},
		{"malformed response", http.StatusOK, `{"result":[]}`, connector.ErrMalformed, "malformed_response"},
		{"missing result", http.StatusOK, `{"success":true}`, connector.ErrMalformed, "malformed_result"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := Client{exchange: func(context.Context, abi.HTTPRequest) (abi.HTTPResponse, error) {
				return abi.HTTPResponse{Status: test.status, Body: []byte(test.body)}, nil
			}}
			var result []string
			_, err := client.request(t.Context(), http.MethodGet, []string{"zones"}, nil, nil, &result)
			var failure *connector.Error
			if !errors.Is(err, test.want) || !errors.As(err, &failure) || failure.Code != test.code {
				t.Fatalf("request() = %v, want %v (%s)", err, test.want, test.code)
			}
			if test.name == "failed envelope" && failure.Detail != "Cloudflare error 81053" {
				t.Fatalf("diagnostic = %q, want only the upstream code", failure.Detail)
			}
		})
	}
}
