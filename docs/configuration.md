# Configuration

Environment variables; every one has a default. Personal settings (currency and electricity price, filament, how prints start and end, notifications, connections, AI) live in the UI per person — the `OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings.

| Variable | Default | What it does |
|---|---|---|
| `BASE_URL` | – | Where the app is (links in pushes, events and other apps, the OIDC callback) |
| `HOST` / `PORT` | `0.0.0.0` / `8080` | Where it listens |
| `DATA_DIR` | `./data` | The database, the model files and photos (`media/<user>/`) and the Pocket Core token |
| `DB_PATH` | `$DATA_DIR/pocket-printfarm.db` | The SQLite file |
| `AUTH_MODE` | `local` | `local` (passwords), `proxy` (a header from your reverse proxy), `none` (one user, for trying it out) |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | The first account, made on an empty database |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL` / `NOTIFY_FORMAT` | – / `apprise` | Pushes: an Apprise URL, an ntfy topic or a JSON webhook |
| `NOTIFY_HOUR` | `9` | Default hour upkeep reminders go at |
| `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` | – | AI without Pocket Core |
| `CORE_URL` / `CORE_REGISTRATION_KEY` / `CORE_APP_URL` | – | Pocket Core |
| `PRINTER_POLL` | `10` | Seconds between asking connected printers how they are; `0` = only when a page opens |
| `MAX_UPLOAD_BYTES` | `1073741824` | The biggest model file (1 GB) |
| `TRUST_PROXY` | `true` | Take the client address from `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | Prometheus `/metrics` |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request (an export being brought back). Model uploads stream to disk (see `MAX_UPLOAD_BYTES`) |
| `DEMO_DATA` | `false` | Seed four printers (two printing now), nine filaments and fourteen spools, thirteen models with their shapes and sliced G-code, a queue, nine months of prints and the printers' upkeep |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | |

Connections to Pocket Files, Inventory, Money, Invoicing, Contacts, Tasks, Calendar, Chat, Parcels, Projects, Home and Alerts are made per person under Settings → Connected apps (through Pocket Core with nothing to paste, or directly with a URL and a token).

## Printers

Each printer is added in the app (Printers → Add a printer), with how it is reached:

| Connection | Address | API key |
|---|---|---|
| Klipper (Moonraker) | `http://voron.local` or `http://IP:7125` | Only if Moonraker asks for one |
| OctoPrint | `http://octopi.local` | Settings → Application keys |
| PrusaLink | `http://IP` of the printer | On the printer: Settings → Network → PrusaLink |
| By hand | – | – |

The server asks them over your network, so it must reach them (a Docker container on the same box does). The camera is a snapshot address (one JPEG; an MJPEG stream gives its first frame), or for Moonraker the camera it knows.

## Behind a reverse proxy

Model files can be large: allow request bodies of a few hundred megabytes to 1 GB.
