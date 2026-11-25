package internal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

func TestManifestDeclaresCloudflareBearerBinding(t *testing.T) {
	manifest := (&Provider{}).Manifest()
	want := abi.HTTPBinding{
		URLSetting:  "endpoint",
		Credentials: []abi.HTTPHeaderCredential{{Header: "Authorization", SecretField: "token", Prefix: "Bearer "}},
	}
	if !reflect.DeepEqual(manifest.HTTP, want) {
		t.Fatalf("Manifest().HTTP = %+v, want %+v", manifest.HTTP, want)
	}

	fields := make(map[string]connector.ConnectionField)
	for _, field := range manifest.Description.ConnectionFields {
		fields[field.Key] = field
	}
	endpoint := fields["endpoint"]
	if !endpoint.URL || endpoint.Secret || endpoint.Default != "https://api.cloudflare.com/client/v4" {
		t.Fatalf("endpoint field = %+v", endpoint)
	}
	if token := fields["token"]; !token.Secret || !token.Required {
		t.Fatalf("token field = %+v", token)
	}
}

func TestPrepareSettingsBindsTheAccountToCloudflare(t *testing.T) {
	provider := Provider{}
	raw := map[string]string{accountIDKey: " " + strings.ToUpper(fixtureAccountID) + " ", credentialKey: "fixture-token"}
	got, err := provider.PrepareSettings(t.Context(), raw)
	if err != nil || got.TargetKey != fixtureAccountID || !reflect.DeepEqual(got.Settings, fixtureSettings()) {
		t.Fatalf("PrepareSettings() = %+v, %v", got, err)
	}
	if _, err := provider.PrepareSettings(t.Context(), map[string]string{baseURLKey: "https://example.test", accountIDKey: fixtureAccountID}); !errors.Is(err, connector.ErrInvalidServerURL) {
		t.Fatalf("PrepareSettings() with separate endpoint = %v, want invalid URL", err)
	}
	if _, err := provider.PrepareSettings(t.Context(), map[string]string{accountIDKey: "wrong"}); !errors.Is(err, connector.ErrInvalidServer) {
		t.Fatalf("PrepareSettings() with invalid account = %v, want invalid configuration", err)
	}
}

func TestConnectRequiresSettingsAndDefersHTTP(t *testing.T) {
	settings := connector.ConnectionConfig{Settings: fixtureSettings()}
	if _, err := (&Provider{}).Connect(settings); !errors.Is(err, connector.ErrUnavailable) {
		t.Fatalf("Connect() without HTTP = %v, want unavailable", err)
	}

	provider := NewProvider(func(context.Context, abi.HTTPRequest) (abi.HTTPResponse, error) {
		t.Fatal("Connect must not make an HTTP request")
		return abi.HTTPResponse{}, nil
	})
	if _, err := provider.Connect(connector.ConnectionConfig{}); !errors.Is(err, connector.ErrInvalidServerURL) {
		t.Fatalf("Connect() without endpoint = %v, want invalid URL", err)
	}
	if _, err := provider.Connect(connector.ConnectionConfig{Settings: map[string]string{baseURLKey: baseURL}}); !errors.Is(err, connector.ErrInvalidServer) {
		t.Fatalf("Connect() without account = %v, want invalid configuration", err)
	}
	if client, err := provider.Connect(settings); err != nil || client == nil {
		t.Fatalf("Connect() = %v, %v", client, err)
	}
}
