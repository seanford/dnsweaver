---
title: Sources
description: How dnsweaver discovers hostnames from Docker containers, Kubernetes resources, and Proxmox VE workloads
icon: material/source-branch
---

# Sources

dnsweaver discovers hostnames to manage from multiple **sources**. Each source type extracts hostnames differently, allowing dnsweaver to work with existing reverse proxy configurations on Docker and Kubernetes, and with VMs and LXC containers on Proxmox VE.

## Available Sources

<div class="grid cards" markdown>

-   :material-label:{ .lg .middle } **Docker Labels**

    ---

    Parse hostnames from Traefik router labels on Docker containers.

    [:octicons-arrow-right-24: Docker Labels](docker.md)

-   :fontawesome-brands-docker:{ .lg .middle } **Docker Swarm**

    ---

    Discover services in Docker Swarm mode with support for service labels and tasks.

    [:octicons-arrow-right-24: Swarm Mode](swarm.md)

-   :material-file-document:{ .lg .middle } **Traefik Files**

    ---

    Watch Traefik dynamic configuration files for hostname changes.

    [:octicons-arrow-right-24: Traefik Files](traefik-files.md)

-   :material-cloud-download:{ .lg .middle } **HTTP Provider**

    ---

    Poll a Traefik-style dynamic configuration endpoint over HTTP.

    [:octicons-arrow-right-24: HTTP Source](http.md)

-   :material-rocket-launch:{ .lg .middle } **Caddy Labels**

    ---

    Parse hostnames from caddy-docker-proxy style container labels.

    [:octicons-arrow-right-24: Caddy Labels](caddy.md)

-   :simple-nginx:{ .lg .middle } **nginx-proxy Labels**

    ---

    Parse `VIRTUAL_HOST` labels used by jwilder/nginx-proxy.

    [:octicons-arrow-right-24: nginx-proxy Labels](nginx-proxy.md)

-   :material-tag-text:{ .lg .middle } **Native Labels**

    ---

    Use dnsweaver-specific labels for explicit DNS record configuration.

    [:octicons-arrow-right-24: Native Labels](native-labels.md)

-   :material-kubernetes:{ .lg .middle } **Kubernetes**

    ---

    Automatic hostname extraction from Ingress, IngressRoute, HTTPRoute, and Service resources.

    [:octicons-arrow-right-24: Kubernetes](kubernetes.md)

-   :material-server:{ .lg .middle } **Proxmox VE**

    ---

    Discover VMs and LXC containers on a Proxmox cluster and create A records from VM names.

    [:octicons-arrow-right-24: Proxmox](proxmox.md)

</div>

## Source Priority

When multiple sources provide the same hostname, dnsweaver uses the following priority:

1. **Native labels** (explicit dnsweaver configuration)
2. **Traefik labels** (reverse proxy configuration)
3. **Caddy labels** (caddy-docker-proxy configuration)
4. **nginx-proxy labels** (`VIRTUAL_HOST`)
5. **Traefik files** (dynamic configuration)
6. **HTTP provider** (remote Traefik-style dynamic config)
7. **Kubernetes** (resource spec hostnames)
8. **Proxmox VE** (VM/LXC name + domain suffix)

## Hostname Extraction

Each source extracts hostnames differently:

| Source | Extracts From | Example Label/Config |
| :----- | :------------ | :------------------- |
| Docker (Traefik) | `traefik.http.routers.*.rule` | `` Host(`app.example.com`) `` |
| Docker (Caddy) | `caddy` / `caddy_<n>` | `caddy=app.example.com` |
| Docker (nginx-proxy) | `VIRTUAL_HOST` label | `VIRTUAL_HOST=app.example.com` |
| Docker Swarm | Service labels | Same as Docker |
| Traefik Files | `http.routers.*.rule` in YAML/TOML | Standard Traefik config |
| HTTP Provider | Traefik config payload over HTTP | `http.routers.*.rule` in fetched YAML/TOML |
| Native | `dnsweaver.hostname` | `dnsweaver.hostname=app.example.com` |
| Kubernetes | Resource spec fields | `.spec.rules[].host` (Ingress) |
| Proxmox VE | VM/LXC name + domain suffix | `webserver` + `home.example.com` |

!!! info "Multiple hostnames"
    Containers and Kubernetes resources can expose multiple hostnames. All discovered hostnames are processed independently and matched against configured provider domains.
