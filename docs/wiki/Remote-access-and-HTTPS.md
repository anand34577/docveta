# Remote access and HTTPS

Docveta serves plain HTTP, fine on your own computer or home network. To use it from outside,
put it behind **HTTPS**: your documents and password then travel encrypted, and Docveta marks
its cookies as secure.

Whatever you choose, set `DOCVETA_BASE_URL` to the address people use, and
`DOCVETA_TRUSTED_PROXIES` to the proxy's address so Docveta sees real client IPs (important for
the login rate limits and the audit log):

```ini
DOCVETA_BASE_URL=https://docs.example.com
DOCVETA_TRUSTED_PROXIES=127.0.0.1
```

## Option 1: Tailscale, NetBird or ZeroTier (easiest, nothing exposed)

Install it on the Docveta machine and on your phone/laptop, then open
`http://<machine-name>:8080` from anywhere. Nothing is opened to the internet. Tailscale can
also add HTTPS: `tailscale serve --bg 8080`.

## Option 2: Caddy (automatic HTTPS certificates)

With a domain pointing at your server and ports 80/443 open:

```
# /etc/caddy/Caddyfile
docs.example.com {
    reverse_proxy 127.0.0.1:8080
    request_body {
        max_size 600MB
    }
}
```

Set `DOCVETA_LISTEN=127.0.0.1:8080` so Docveta is reachable only through Caddy.

## Option 3: Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name docs.example.com;
    # ssl_certificate … (e.g. from certbot)

    client_max_body_size 600m;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 1h;          # live updates (Server-Sent Events)
        proxy_buffering off;
    }
}
```

## Option 4: Cloudflare Tunnel

`cloudflared tunnel --url http://localhost:8080` publishes Docveta without opening ports.
Raise Cloudflare's upload limit or upload large scans from your home network.

## Before you open Docveta to the internet

- Use long passwords; consider single sign-on with two-factor login in your identity provider.
- Keep `DOCVETA_ALLOW_LOCAL_TARGETS` off.
- Don't expose `/metrics`; block it in the proxy if needed.
- Keep Docveta updated: [Upgrading](Upgrading).
