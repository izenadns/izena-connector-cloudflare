package internal

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/izenadns/izena-connector-contract/connector"
)

const (
	automaticRecordTTL = 1
	maxRecordPageSize  = 100
)

type recordWire struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Type              string                     `json:"type"`
	TTL               int64                      `json:"ttl"`
	Content           string                     `json:"content"`
	Data              map[string]json.RawMessage `json:"data"`
	Priority          *uint16                    `json:"priority"`
	Proxiable         bool                       `json:"proxiable"`
	Proxied           bool                       `json:"proxied"`
	Comment           string                     `json:"comment"`
	CommentModifiedOn *time.Time                 `json:"comment_modified_on"`
}

type recordMutationWire struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	TTL      int64          `json:"ttl"`
	Content  string         `json:"content,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Priority *uint16        `json:"priority,omitempty"`
	Proxied  *bool          `json:"proxied,omitempty"`
	Comment  string         `json:"comment"`
}

func (c *Client) ListRecords(ctx context.Context, zoneID string) ([]connector.IndividualRecord, error) {
	if _, err := c.readZone(ctx, zoneID); err != nil {
		return nil, err
	}
	records := []connector.IndividualRecord{}
	input := connector.PageInput{Limit: maxRecordPageSize}
	for {
		page, err := c.listRecordsPage(ctx, zoneID, input)
		if err != nil {
			return nil, err
		}
		records = append(records, page.Items...)
		if page.NextCursor == "" {
			return records, nil
		}
		input.Cursor = page.NextCursor
	}
}

func (c *Client) ListRecordsPage(ctx context.Context, zoneID string, input connector.PageInput) (connector.Page[connector.IndividualRecord], error) {
	if _, err := c.readZone(ctx, zoneID); err != nil {
		return connector.Page[connector.IndividualRecord]{}, err
	}
	return c.listRecordsPage(ctx, zoneID, input)
}

func (c *Client) listRecordsPage(ctx context.Context, zoneID string, input connector.PageInput) (connector.Page[connector.IndividualRecord], error) {
	if input.Limit <= 0 {
		return connector.Page[connector.IndividualRecord]{}, invalidPage()
	}
	// Cloudflare selects native pagination when cursor is present, even when empty.
	query := url.Values{
		"cursor":   {input.Cursor},
		"per_page": {strconv.Itoa(min(input.Limit, maxRecordPageSize))},
	}
	var wire []recordWire
	info, err := c.request(ctx, http.MethodGet, []string{"zones", zoneID, "dns_records"}, query, nil, &wire)
	if err != nil {
		return connector.Page[connector.IndividualRecord]{}, err
	}
	records := make([]connector.IndividualRecord, 0, len(wire))
	for _, row := range wire {
		record, err := decodeRecord(row, "")
		if err != nil {
			return connector.Page[connector.IndividualRecord]{}, err
		}
		records = append(records, record)
	}
	return connector.Page[connector.IndividualRecord]{Items: records, NextCursor: info.Cursor}, nil
}

func (c *Client) GetRecord(ctx context.Context, zoneID, recordID string) (connector.IndividualRecord, error) {
	if !validID(recordID) {
		return connector.IndividualRecord{}, invalidRecord("invalid_record_id")
	}
	if _, err := c.readZone(ctx, zoneID); err != nil {
		return connector.IndividualRecord{}, err
	}
	var wire recordWire
	_, err := c.request(ctx, http.MethodGet, []string{"zones", zoneID, "dns_records", recordID}, nil, nil, &wire)
	if err != nil {
		return connector.IndividualRecord{}, err
	}
	return decodeRecord(wire, recordID)
}

func (c *Client) CreateRecord(ctx context.Context, zoneID string, input connector.IndividualRecordInput) (connector.IndividualRecord, error) {
	body, err := encodeRecord(input)
	if err != nil {
		return connector.IndividualRecord{}, err
	}
	if err := c.requireRecordWrite(ctx, zoneID); err != nil {
		return connector.IndividualRecord{}, err
	}
	var wire recordWire
	_, err = c.request(ctx, http.MethodPost, []string{"zones", zoneID, "dns_records"}, nil, body, &wire)
	if err != nil {
		return connector.IndividualRecord{}, err
	}
	return decodeRecord(wire, "")
}

func (c *Client) UpdateRecord(ctx context.Context, zoneID, recordID string, input connector.IndividualRecordInput) (connector.IndividualRecord, error) {
	if !validID(recordID) {
		return connector.IndividualRecord{}, invalidRecord("invalid_record_id")
	}
	body, err := encodeRecord(input)
	if err != nil {
		return connector.IndividualRecord{}, err
	}
	if err := c.requireRecordWrite(ctx, zoneID); err != nil {
		return connector.IndividualRecord{}, err
	}
	var wire recordWire
	_, err = c.request(ctx, http.MethodPatch, []string{"zones", zoneID, "dns_records", recordID}, nil, body, &wire)
	if err != nil {
		return connector.IndividualRecord{}, err
	}
	return decodeRecord(wire, recordID)
}

func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	if !validID(recordID) {
		return invalidRecord("invalid_record_id")
	}
	if err := c.requireRecordWrite(ctx, zoneID); err != nil {
		return err
	}
	_, err := c.request(ctx, http.MethodDelete, []string{"zones", zoneID, "dns_records", recordID}, nil, nil, nil)
	return err
}

func (c *Client) requireRecordWrite(ctx context.Context, zoneID string) error {
	zone, err := c.readZone(ctx, zoneID)
	if err != nil {
		return err
	}
	if !zoneSupportsRecordWrites(zone.Type) {
		return connector.ErrUnsupported
	}
	return nil
}

func encodeRecord(input connector.IndividualRecordInput) (recordMutationWire, error) {
	body := recordMutationWire{
		Name: strings.TrimSuffix(input.Name, "."), Type: strings.ToUpper(input.Type),
		TTL: input.TTL, Content: input.Content,
	}
	if !validRecordText(body.Name, 255) || !validRecordText(body.Content, 65535) {
		return recordMutationWire{}, invalidRecord("invalid_record_fields")
	}
	if input.Disabled || len(input.Comments) > 1 {
		return recordMutationWire{}, connector.ErrUnsupported
	}
	if len(input.Comments) == 1 {
		body.Comment = input.Comments[0].Content
	}
	for key, value := range input.Options {
		if key != proxiedKey || !proxyRecordType(body.Type) {
			return recordMutationWire{}, connector.ErrUnsupported
		}
		if value.Boolean == nil || value.String != nil || value.Integer != nil || value.StringList != nil {
			return recordMutationWire{}, invalidRecord("invalid_record_proxy_setting")
		}
		body.Proxied = value.Boolean
	}
	switch {
	case input.AutomaticTTL && input.TTL != 0:
		return recordMutationWire{}, invalidRecord("invalid_record_ttl")
	case input.AutomaticTTL || body.Proxied != nil && *body.Proxied:
		// Cloudflare always applies automatic TTL to proxied records.
		body.TTL = automaticRecordTTL
	case body.TTL < 30 || body.TTL > 86400:
		return recordMutationWire{}, invalidRecord("invalid_record_ttl")
	}
	if err := encodeRecordContent(&body); err != nil {
		return recordMutationWire{}, err
	}
	return body, nil
}

func decodeRecord(wire recordWire, requestedID string) (connector.IndividualRecord, error) {
	if !validID(wire.ID) || requestedID != "" && wire.ID != requestedID ||
		!validRecordText(wire.Name, 255) || !validRecordText(wire.Type, 64) ||
		wire.TTL < automaticRecordTTL || wire.TTL > 86400 {
		return connector.IndividualRecord{}, malformedRecord()
	}

	content := decodeRecordContent(wire)
	if !validRecordText(content, 65535) {
		return connector.IndividualRecord{}, malformedRecord()
	}
	record := connector.IndividualRecord{
		ID: wire.ID,
		IndividualRecordInput: connector.IndividualRecordInput{
			Name: strings.TrimSuffix(wire.Name, ".") + ".", Type: wire.Type,
			TTL: wire.TTL, Content: content, Comments: []connector.Comment{},
		},
	}
	if wire.TTL == automaticRecordTTL || wire.Proxied {
		record.AutomaticTTL = true
		record.TTL = 0
	}
	if wire.Proxiable && proxyRecordType(wire.Type) {
		record.Options = map[string]connector.SettingValue{proxiedKey: {Boolean: &wire.Proxied}}
	}
	if wire.Comment != "" {
		comment := connector.Comment{Content: wire.Comment}
		if wire.CommentModifiedOn != nil {
			comment.ModifiedAt = wire.CommentModifiedOn.UTC()
		}
		record.Comments = append(record.Comments, comment)
	}
	return record, nil
}

func validRecordText(value string, maxBytes int) bool {
	return value != "" && utf8.ValidString(value) && len(value) <= maxBytes && !strings.ContainsAny(value, "\x00\r\n")
}

func invalidRecord(code string) error {
	return &connector.Error{Kind: connector.ErrValidation, Code: code}
}

func malformedRecord() error {
	return &connector.Error{Kind: connector.ErrMalformed, Code: "invalid_record_fields"}
}

type recordFieldKind int

const (
	recordWord recordFieldKind = iota
	recordUint8
	recordUint16
	recordMeters
	recordDecimal
	recordCharacterString
	recordTail
)

type recordField struct {
	name string
	kind recordFieldKind
}

// Field order follows DNS presentation format. Cloudflare accepts structured
// records as a data object and returns their formatted content.
var recordDataFields = map[string][]recordField{
	"CAA":    {{"flags", recordUint8}, {"tag", recordWord}, {"value", recordCharacterString}},
	"CERT":   {{"type", recordUint16}, {"key_tag", recordUint16}, {"algorithm", recordUint8}, {"certificate", recordTail}},
	"DNSKEY": {{"flags", recordUint16}, {"protocol", recordUint8}, {"algorithm", recordUint8}, {"public_key", recordTail}},
	"DS":     {{"key_tag", recordUint16}, {"algorithm", recordUint8}, {"digest_type", recordUint8}, {"digest", recordTail}},
	"HTTPS":  {{"priority", recordUint16}, {"target", recordWord}, {"value", recordTail}},
	"LOC": {
		{"lat_degrees", recordUint8}, {"lat_minutes", recordUint8}, {"lat_seconds", recordDecimal}, {"lat_direction", recordWord},
		{"long_degrees", recordUint8}, {"long_minutes", recordUint8}, {"long_seconds", recordDecimal}, {"long_direction", recordWord},
		{"altitude", recordMeters}, {"size", recordMeters}, {"precision_horz", recordMeters}, {"precision_vert", recordMeters},
	},
	"NAPTR": {
		{"order", recordUint16}, {"preference", recordUint16}, {"flags", recordCharacterString},
		{"service", recordCharacterString}, {"regex", recordCharacterString}, {"replacement", recordWord},
	},
	"SMIMEA": {{"usage", recordUint8}, {"selector", recordUint8}, {"matching_type", recordUint8}, {"certificate", recordTail}},
	"SRV":    {{"priority", recordUint16}, {"weight", recordUint16}, {"port", recordUint16}, {"target", recordWord}},
	"SSHFP":  {{"algorithm", recordUint8}, {"type", recordUint8}, {"fingerprint", recordTail}},
	"SVCB":   {{"priority", recordUint16}, {"target", recordWord}, {"value", recordTail}},
	"TLSA":   {{"usage", recordUint8}, {"selector", recordUint8}, {"matching_type", recordUint8}, {"certificate", recordTail}},
	"URI":    {{"priority", recordUint16}, {"weight", recordUint16}, {"target", recordCharacterString}},
}

func encodeRecordContent(body *recordMutationWire) error {
	if !slices.Contains(creatableRecordTypes(), body.Type) {
		return connector.ErrUnsupported
	}
	if body.Type == "MX" {
		priority, rest, _ := splitRecordWord(body.Content)
		target, trailing, _ := splitRecordWord(rest)
		number, err := strconv.ParseUint(priority, 10, 16)
		if err != nil || target == "" || trailing != "" {
			return invalidRecord("invalid_record_content")
		}
		value := uint16(number)
		body.Priority, body.Content = &value, target
		return nil
	}
	if body.Type == "TXT" && !strings.HasPrefix(strings.TrimSpace(body.Content), `"`) {
		body.Content = quoteRecordString(body.Content)
		return nil
	}
	fields := recordDataFields[body.Type]
	if len(fields) == 0 {
		return nil
	}

	data := make(map[string]any, len(fields))
	remaining := body.Content
	if body.Type == "LOC" {
		remaining = normalizeLOCContent(remaining)
	}
	for _, field := range fields {
		if remaining == "" && optionalRecordData(body.Type, field.name) {
			continue
		}
		value, rest, ok := parseRecordField(remaining, field.kind)
		if !ok {
			return invalidRecord("invalid_record_content")
		}
		data[field.name] = value
		remaining = rest
	}
	if remaining != "" {
		return invalidRecord("invalid_record_content")
	}
	if body.Type == "URI" {
		priority, ok := data["priority"].(uint16)
		if !ok {
			return invalidRecord("invalid_record_content")
		}
		body.Priority = &priority
		delete(data, "priority")
	}
	body.Content, body.Data = "", data
	return nil
}

// Cloudflare may omit formatted content, and some types report their priority
// separately. Use structured data when available to recover complete RDATA.
func decodeRecordContent(wire recordWire) string {
	if content := formatRecordData(wire); content != "" {
		return content
	}
	switch wire.Type {
	case "MX", "SRV", "URI":
		// The decoder's input is also valid presentation for a subsequent edit.
		body := recordMutationWire{Type: wire.Type, Content: wire.Content}
		if encodeRecordContent(&body) == nil {
			return wire.Content
		}
		if wire.Priority != nil && wire.Content != "" {
			body.Content = strconv.Itoa(int(*wire.Priority)) + " " + wire.Content
			content := body.Content
			if encodeRecordContent(&body) == nil {
				return content
			}
		}
		return ""
	default:
		return wire.Content
	}
}

func formatRecordData(wire recordWire) string {
	fields := recordDataFields[wire.Type]
	if len(fields) == 0 || len(wire.Data) == 0 {
		return ""
	}
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		raw, ok := wire.Data[field.name]
		if !ok {
			if field.name == "priority" && wire.Priority != nil {
				words = append(words, strconv.Itoa(int(*wire.Priority)))
				continue
			}
			if wire.Type == "LOC" {
				if value := defaultLOCField(field.name); value != "" {
					words = append(words, value)
					continue
				}
			}
			if optionalRecordData(wire.Type, field.name) {
				continue
			}
			return ""
		}
		switch field.kind {
		case recordWord, recordCharacterString, recordTail:
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return ""
			}
			if field.kind == recordCharacterString {
				value = quoteRecordString(value)
			}
			words = append(words, value)
		default:
			var value json.Number
			if json.Unmarshal(raw, &value) != nil || value == "" {
				return ""
			}
			words = append(words, string(value))
		}
	}
	return strings.Join(words, " ")
}

// LOC presentation permits omitting minutes and seconds for either axis.
func normalizeLOCContent(content string) string {
	fields := strings.Fields(content)
	normalized := make([]string, 0, 12)
	for _, directions := range []string{"NS", "EW"} {
		index := slices.IndexFunc(fields, func(field string) bool {
			return len(field) == 1 && strings.Contains(directions, strings.ToUpper(field))
		})
		if index < 1 || index > 3 {
			return ""
		}
		normalized = append(normalized, fields[:index]...)
		for omitted := 3 - index; omitted > 0; omitted-- {
			normalized = append(normalized, "0")
		}
		normalized = append(normalized, strings.ToUpper(fields[index]))
		fields = fields[index+1:]
	}
	return strings.Join(append(normalized, fields...), " ")
}

// RFC 1876 defines defaults for omitted LOC coordinates and precision.
func defaultLOCField(field string) string {
	switch field {
	case "lat_minutes", "lat_seconds", "long_minutes", "long_seconds":
		return "0"
	case "size":
		return "1"
	case "precision_horz":
		return "10000"
	case "precision_vert":
		return "10"
	default:
		return ""
	}
}

func quoteRecordString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func optionalRecordData(recordType, field string) bool {
	switch recordType {
	case "HTTPS", "SVCB":
		return field == "value"
	case "LOC":
		return field == "size" || field == "precision_horz" || field == "precision_vert"
	default:
		return false
	}
}

func parseRecordField(input string, kind recordFieldKind) (any, string, bool) {
	switch kind {
	case recordTail:
		if input == "" {
			return nil, "", false
		}
		return input, "", true
	case recordCharacterString:
		return readRecordCharacterString(input)
	}
	word, rest, ok := splitRecordWord(input)
	if !ok {
		return nil, "", false
	}
	switch kind {
	case recordUint8:
		value, err := strconv.ParseUint(word, 10, 8)
		return uint8(value), rest, err == nil
	case recordUint16:
		value, err := strconv.ParseUint(word, 10, 16)
		return uint16(value), rest, err == nil
	case recordMeters:
		word = strings.TrimSuffix(strings.ToLower(word), "m")
		fallthrough
	case recordDecimal:
		value, err := strconv.ParseFloat(word, 64)
		return value, rest, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
	default:
		return word, rest, true
	}
}

func splitRecordWord(input string) (string, string, bool) {
	input = strings.TrimSpace(input)
	index := strings.IndexAny(input, " \t")
	if index >= 0 {
		return input[:index], strings.TrimSpace(input[index:]), true
	}
	return input, "", input != ""
}

// A DNS character-string may contain whitespace and RFC 1035 escapes.
func readRecordCharacterString(input string) (string, string, bool) {
	input = strings.TrimSpace(input)
	if input == "" || input[0] != '"' {
		return splitRecordWord(input)
	}
	var value strings.Builder
	for index := 1; index < len(input); index++ {
		switch input[index] {
		case '"':
			rest := input[index+1:]
			if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				return "", "", false
			}
			result := value.String()
			return result, strings.TrimSpace(rest), utf8.ValidString(result)
		case '\\':
			index++
			if index >= len(input) {
				return "", "", false
			}
			if input[index] >= '0' && input[index] <= '9' {
				if index+3 > len(input) {
					return "", "", false
				}
				escaped, err := strconv.ParseUint(input[index:index+3], 10, 8)
				if err != nil {
					return "", "", false
				}
				value.WriteByte(byte(escaped))
				index += 2
			} else {
				value.WriteByte(input[index])
			}
		default:
			value.WriteByte(input[index])
		}
	}
	return "", "", false
}

var _ connector.IndividualRecordOperations = (*Client)(nil)
