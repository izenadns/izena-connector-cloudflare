# Izena Cloudflare connector

A WASI connector for Cloudflare DNS. It manages zones, individual DNS records,
and DNSSEC. Eligible A, AAAA, and CNAME records support Cloudflare proxying.

Use a Cloudflare account ID and an API token scoped to the zones you want to
manage. Grant Zone Read and DNS Read for inspection, DNS Write for record and
DNSSEC changes, and Zone Write for zone changes. Before changing a zone's
setup type, prepare the DNS migration described by Cloudflare for
[Full to Partial](https://developers.cloudflare.com/dns/zone-setups/conversions/convert-full-to-partial/)
or [Partial to Full](https://developers.cloudflare.com/dns/zone-setups/conversions/convert-partial-to-full/).

## Development

```sh
make test
make test-integration-fixtures
make build
```

`make build` writes the Wasm artifact and its `.sha256` file. Run
`make artifact-path` for the artifact path and `make help` for other commands.

## License

[Apache License 2.0](LICENSE)
