# Reverse proxy

Set `BASE_URL` to the public address and `SECURE_COOKIES=true` behind HTTPS. See [deploy/reverse-proxy/Caddyfile](../deploy/reverse-proxy/Caddyfile).

- **Uploads**: a model file comes in one request (a 3MF or G-code can be a few hundred MB). If your proxy limits request bodies, allow at least 1 GB (`MAX_UPLOAD_BYTES`).
- **Printers**: the server, not the browser, talks to the printers and their cameras, so the printers can stay on your home network.
