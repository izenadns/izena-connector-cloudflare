package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/izenadns/izena-connector-contract/abi"
	"github.com/izenadns/izena-connector-contract/connector"
)

const maxResponseBytes = 16 << 20

// Exchange delegates a request to Izena's connection-scoped HTTP host.
type Exchange func(context.Context, abi.HTTPRequest) (abi.HTTPResponse, error)

type Client struct {
	exchange  Exchange
	accountID string
}

type pageInfo struct {
	Cursor     string `json:"cursor"`
	TotalPages int    `json:"total_pages"`
}

type responseEnvelope struct {
	Success *bool           `json:"success"`
	Result  json.RawMessage `json:"result"`
	Page    pageInfo        `json:"result_info"`
	Errors  []struct {
		Code int `json:"code"`
	} `json:"errors"`
}

func (c *Client) request(ctx context.Context, method string, parts []string, query url.Values, input, output any) (pageInfo, error) {
	call := abi.HTTPRequest{
		Method: method, Parts: parts, Query: query, Limit: maxResponseBytes,
		Headers: http.Header{"Accept": {"application/json"}},
	}
	if input != nil {
		body, err := abi.Marshal(input)
		if err != nil {
			return pageInfo{}, connector.ErrValidation
		}
		call.Body = body
		call.Headers.Set("Content-Type", "application/json")
	}
	response, err := c.exchange(ctx, call)
	if err != nil {
		return pageInfo{}, err
	}
	var envelope responseEnvelope
	decodeErr := json.Unmarshal(response.Body, &envelope)
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		return pageInfo{}, statusError(response.Status, envelope)
	}
	if decodeErr != nil || envelope.Success == nil {
		return pageInfo{}, &connector.Error{Kind: connector.ErrMalformed, StatusCode: response.Status, Code: "malformed_response"}
	}
	if !*envelope.Success {
		return pageInfo{}, statusError(response.Status, envelope)
	}
	if output != nil && (len(envelope.Result) == 0 || bytes.Equal(envelope.Result, []byte("null")) ||
		json.Unmarshal(envelope.Result, output) != nil) {
		return pageInfo{}, &connector.Error{Kind: connector.ErrMalformed, StatusCode: response.Status, Code: "malformed_result"}
	}
	return envelope.Page, nil
}

func statusError(status int, envelope responseEnvelope) *connector.Error {
	failure := &connector.Error{Kind: connector.ErrUnavailable, StatusCode: status, Code: "unavailable"}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		failure.Kind, failure.Code = connector.ErrAuthentication, "authentication_failed"
	case status == http.StatusNotFound:
		failure.Kind, failure.Code = connector.ErrNotFound, "not_found"
	case status == http.StatusConflict:
		failure.Kind, failure.Code = connector.ErrConflict, "conflict"
	case status == http.StatusTooManyRequests:
		failure.Code = "rate_limited"
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		failure.Kind, failure.Code = connector.ErrTimeout, "timeout"
	case status >= http.StatusMultipleChoices && status < http.StatusBadRequest:
		failure.Kind, failure.Code = connector.ErrRedirect, "redirect_rejected"
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError,
		status >= http.StatusOK && status < http.StatusMultipleChoices:
		failure.Kind, failure.Code = connector.ErrValidation, "validation_failed"
	}
	if len(envelope.Errors) > 0 && envelope.Errors[0].Code > 0 {
		failure.Detail = fmt.Sprintf("Cloudflare error %d", envelope.Errors[0].Code)
	}
	return failure
}

func invalidPage() *connector.Error {
	return &connector.Error{Kind: connector.ErrValidation, Code: "invalid_page"}
}

var _ connector.Client = (*Client)(nil)
