# HTTP Source

dnsweaver can poll an HTTP endpoint that returns a Traefik-style dynamic configuration payload and extract hostnames from `http.routers.*.rule`.

## Configuration

```yaml
environment:
  - DNSWEAVER_SOURCES=traefik,http
  - DNSWEAVER_SOURCE_HTTP_ENDPOINT=http://mantrae:3000/api/thefordestate?token=vmtuuu26jm
  - DNSWEAVER_SOURCE_HTTP_POLL_INTERVAL=5s
  - DNSWEAVER_SOURCE_HTTP_POLL_TIMEOUT=5s
  - DNSWEAVER_SOURCE_HTTP_HEADERS=Traefik-Instance-Name:thefordestate-traefik-prod,Traefik-Instance-Url:http://traefik.lab.thefordestate.com:8080
```

## Supported Payload Formats

- YAML (`http.routers.*.rule`)
- TOML (`[http.routers.<name>]`)

## Behavior

- Non-200 responses are treated as discovery errors.
- Malformed payloads are logged as parse errors.
- Hostname extraction and deduplication follow the same Traefik file parser logic used by `sources/traefik`.
