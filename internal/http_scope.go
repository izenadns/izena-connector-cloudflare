package internal

import (
	"net/http"

	"github.com/izenadns/izena-connector-contract/abi"
)

func allowedRequest(call abi.Request, request abi.HTTPRequest) bool {
	method, parts := request.Method, request.Parts
	zone := len(parts) >= 2 && parts[0] == "zones" && parts[1] == call.ZoneID && validID(call.ZoneID)
	zoneResource := zone && len(parts) == 2
	collection := len(parts) == 1 && parts[0] == "zones"
	dnssec := zone && len(parts) == 3 && parts[2] == "dnssec"
	records := zone && len(parts) >= 3 && parts[2] == "dns_records"
	recordCollection := records && len(parts) == 3
	recordResource := records && len(parts) == 4 && parts[3] == call.RecordID && validID(call.RecordID)

	switch call.Operation {
	case abi.Probe, abi.ListZones, abi.ListZonesPage:
		return method == http.MethodGet && collection && request.Query.Get("account.id") == call.Settings[accountIDKey]
	case abi.GetZone, abi.GetZoneMetadata:
		return method == http.MethodGet && zoneResource
	case abi.CreateZone:
		return method == http.MethodPost && collection
	case abi.UpdateZone:
		return zoneResource && (method == http.MethodGet || method == http.MethodPatch)
	case abi.DeleteZone:
		return zoneResource && (method == http.MethodGet || method == http.MethodDelete)
	case abi.GetDNSSEC:
		return method == http.MethodGet && (zoneResource || dnssec)
	case abi.SetDNSSEC:
		return method == http.MethodGet && zoneResource || method == http.MethodPatch && dnssec
	case abi.ListRecords, abi.ListRecordsPage:
		return method == http.MethodGet && (zoneResource || recordCollection)
	case abi.GetRecord:
		return method == http.MethodGet && (zoneResource || recordResource)
	case abi.CreateRecord:
		return method == http.MethodGet && zoneResource || method == http.MethodPost && recordCollection
	case abi.UpdateRecord:
		return method == http.MethodGet && (zoneResource || recordResource) || method == http.MethodPatch && recordResource
	case abi.DeleteRecord:
		return method == http.MethodGet && zoneResource || method == http.MethodDelete && recordResource
	default:
		return false
	}
}
