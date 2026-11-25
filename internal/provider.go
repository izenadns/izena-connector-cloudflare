// Package internal implements the Cloudflare connector and its upstream operations.
package internal

import (
	"context"
	"strings"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

const (
	providerID       = "cloudflare"
	providerName     = "Cloudflare"
	baseURLKey       = "endpoint"
	baseURL          = "https://api.cloudflare.com/client/v4"
	accountIDKey     = "account_id"
	credentialKey    = "token"
	cloudflareIDSize = 32
)

// Provider adapts the Cloudflare HTTP client to Izena's connector contract.
type Provider struct {
	exchange Exchange
}

func NewProvider(exchange Exchange) *Provider {
	return &Provider{exchange: exchange}
}

func (*Provider) Describe() connector.Description {
	return connector.Description{
		ID:   providerID,
		Name: providerName,
		ConnectionFields: []connector.ConnectionField{
			{Key: baseURLKey, Label: "API base URL", Default: baseURL, URL: true, MaxLength: len(baseURL)},
			{Key: accountIDKey, Label: "Account ID", Required: true, MaxLength: cloudflareIDSize},
			{Key: credentialKey, Label: "API token", Secret: true, Required: true, Editable: true, MaxLength: 4096},
		},
	}
}

func (p *Provider) Manifest() abi.Manifest {
	return abi.Manifest{
		Description: p.Describe(),
		HTTP: abi.HTTPBinding{
			URLSetting:  baseURLKey,
			Credentials: []abi.HTTPHeaderCredential{{Header: "Authorization", SecretField: credentialKey, Prefix: "Bearer "}},
		},
	}
}

func (*Provider) PrepareSettings(_ context.Context, settings map[string]string) (connector.PreparedSettings, error) {
	normalized, err := normalizeSettings(settings)
	if err != nil {
		return connector.PreparedSettings{}, err
	}
	return connector.PreparedSettings{Settings: normalized, TargetKey: normalized[accountIDKey]}, nil
}

// GuestClient scopes API requests before invoking the host transport.
func GuestClient(settings map[string]string, allowed func(abi.HTTPRequest) bool, exchange Exchange) (*Client, error) {
	if exchange == nil {
		return nil, connector.ErrUnavailable
	}
	if settings[baseURLKey] != baseURL {
		return nil, connector.ErrInvalidServerURL
	}
	accountID := settings[accountIDKey]
	if !validID(accountID) {
		return nil, &connector.Error{Kind: connector.ErrInvalidServer, Code: "invalid_account_id"}
	}
	return &Client{accountID: accountID, exchange: func(ctx context.Context, request abi.HTTPRequest) (abi.HTTPResponse, error) {
		if allowed != nil && !allowed(request) {
			return abi.HTTPResponse{}, connector.ErrValidation
		}
		return exchange(ctx, request)
	}}, nil
}

func normalizeSettings(settings map[string]string) (map[string]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(settings[baseURLKey]), "/")
	if endpoint != "" && endpoint != baseURL {
		return nil, connector.ErrInvalidServerURL
	}
	accountID := strings.ToLower(strings.TrimSpace(settings[accountIDKey]))
	if !validID(accountID) {
		return nil, &connector.Error{Kind: connector.ErrInvalidServer, Code: "invalid_account_id"}
	}
	return map[string]string{baseURLKey: baseURL, accountIDKey: accountID}, nil
}

func (p *Provider) Connect(config connector.ConnectionConfig) (connector.Client, error) {
	client, err := GuestClient(config.Settings, nil, p.exchange)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func validID(id string) bool {
	if len(id) != cloudflareIDSize {
		return false
	}
	for _, char := range id {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

var _ connector.Provider = (*Provider)(nil)
