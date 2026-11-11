# API

Everything is under `/api/v1`, JSON in and out, with a session cookie, `Authorization: Bearer ppf_…` (Settings → API & shortcuts) or a Pocket Core link token. Errors are `{"error": "…"}`. Everything is per person: another person's printer, model, spool, job or print is a 404.

Times are ISO 8601 UTC; days are local `YYYY-MM-DD`. Money is in minor units (cents) of the person's currency. Weights are grams, sizes millimetres, times seconds.

## The farm

| | |
|---|---|
| `GET /farm` | Every printer as it is now: `state` (idle, printing, paused, done, stopped, error, busy, offline), `live` (what the printer said: file, progress, elapsed, left, layer, temperatures), `print` (the print on it), `progress`, `left`, `ends_at`, `needs_clear`, `spool`, `next` (the job it would take), `camera`, `upkeep_due`; and `queue` |
| `GET /home` | Counts for the sidebar |
| `GET /summary` | Each printer now, the queue, the week, low filament — for Homepage and Pocket AI |
| `GET /day/YYYY-MM-DD` | What was printed (done, failed, started), upkeep done and models added on a day |
| `GET /search?q=` | `{hits: [{kind: model\|printer\|filament\|print\|job, id, title, subtitle, url, path}]}` |
| `GET /stats?months=12` | By month, printer, material and model; why prints failed; totals with the cost |

## Printers

| | |
|---|---|
| `GET /printers`, `GET /printers/ID` | The list (`?archived=1` too); one with its upkeep, log book, last prints and the last 30 days |
| `POST /printers`, `PUT /printers/ID`, `DELETE /printers/ID` | `{name, model, kind (fdm\|resin), connection (manual\|moonraker\|octoprint\|prusalink), url, api_key, webcam_url, bed_x, bed_y, bed_z, nozzle, materials, watts, wear_per_hour, bought_on, price, start_hours, location, color, notes, home_ref, archived}` (the API key is never sent back) |
| `POST /printers/test` | `{connection, url, api_key}` or `{id}` → `{ok, message, status}` |
| `PUT /printers/order` | `{ids: [3, 1, 2]}` |
| `POST /printers/ID/control` | `{action: pause\|resume\|cancel\|clear\|refresh}` — `clear` says the bed is clear |
| `PUT /printers/ID/spool` | `{spool_id}` (null takes it out) |
| `GET /printers/ID/snapshot` | A picture from its camera |
| `GET/POST/DELETE /printers/ID/photo` | Its photo (multipart `file`) |

## The queue

| | |
|---|---|
| `GET /jobs` | `?status=open` (queued and printing, the default), `done,cancelled`, `all` |
| `POST /jobs`, `PUT /jobs/ID`, `DELETE /jobs/ID` | `{title, model_id, file_id, printer_id, filament_id, material, quantity, done_count, status (queued\|done\|cancelled), due_on, for_whom, contact_ref, price, notes}` — a model without `file_id` takes its newest G-code |
| `PUT /jobs/order` | `{ids}` |
| `POST /jobs/ID/start` | `{printer_id, spool_id, copies, send, force}` → the print. A connected printer gets the G-code and starts it |
| `POST /jobs/ID/invoice` | `{client_id, price}` → a draft invoice in Pocket Invoicing |
| `POST /jobs/ID/task` | A task in Pocket Tasks |

## Prints

| | |
|---|---|
| `GET /prints` | `?printer=&model=&job=&spool=&status=done,failed&month=2026-11&q=&keep=1&limit&offset` → `{prints, total}` |
| `GET /prints/ID` | `{print, model, job, price (a price to sell one at), currency}` |
| `POST /prints` | Start: `{printer_id, model_id \| file_id \| job_id, spool_id, copies}`. Or write one down: `{record: true, title \| model_id, printer_id, spool_id, status, started_at, hours, grams, fail_reason, rating, notes}` |
| `POST /prints/ID/finish` | `{status: done\|failed\|cancelled, fail_reason, fail_note, grams, seconds, rating}` — grams come off the spool, the cost is worked out |
| `PUT /prints/ID`, `DELETE /prints/ID` | `{title, notes, rating, fail_reason, fail_note, settings, keep, grams, seconds, copies, spool_id}`; deleting gives the grams back to the spool |
| `GET/POST/DELETE /prints/ID/photo` | Its photo |
| `POST /prints/ID/inventory`, `/chat`, `/issue`, `/calendar`, `/files` | To Pocket Inventory, Chat, Projects, Calendar, Files |
| `POST /prints/ID/diagnose` | Pocket AI: what probably went wrong |

Fail reasons: `adhesion, spaghetti, warping, clog, under_extrusion, layer_shift, stringing, supports, filament_out, power, cancelled_by_me, other`.

## Models

| | |
|---|---|
| `GET /models` | `?q=&tag=&fav=1&sort=added\|name\|printed&limit&offset` → `{models, total}` |
| `GET /models/ID` | `{model, files, prints, kept (prints whose settings worked), queued}` |
| `POST /models`, `PUT /models/ID`, `DELETE /models/ID` | `{name, description, tags, designer, license, url, favourite, archived}` |
| `POST /models/upload` | multipart: `model_id` (optional), `together` (`1`: all files in one model), then `file` (STL, 3MF, OBJ, STEP, G-code; several) |
| `POST /models/link` | `{url, name, tags, model_id}` — a direct link to a model file |
| `GET /models/tags` | `{tags: [{tag, count}]}` |
| `GET/POST/DELETE /models/ID/cover` | Its picture |
| `POST /models/ID/settings` | Pocket AI: settings to start from (`{printer_id, material, ask}`) |
| `GET /files/ID/download`, `/mesh`, `/thumb` | The file, its shape as binary STL (for 3MF and OBJ too), its picture |
| `PUT /files/ID`, `DELETE /files/ID` | `{name, model_id, material}` |
| `POST /files/ID/files` | A copy in Pocket Files |
| `POST /apps/files/import` | `{space, paths, model_id, together}` — from Pocket Files |

## Filament

| | |
|---|---|
| `GET /filaments`, `GET /filaments/ID` | Kinds of filament with how many spools and grams left |
| `POST /filaments`, `PUT /filaments/ID`, `DELETE /filaments/ID` | `{brand, material, name, color, diameter, density, nozzle_temp, bed_temp, net_weight, spool_weight, price, url, notes}` |
| `GET /spools` | `?archived=1&filament=&material=` → `{spools, low, left_g, low_grams}` |
| `POST /spools` | `{filament_id \| filament: {…}, initial_g, left_g, price, bought_on, location, lot, notes, count}` |
| `GET /spools/ID`, `PUT /spools/ID`, `DELETE /spools/ID` | With its log and prints |
| `POST /spools/ID/weigh` | `{gross_g}` (on the scale, with the empty spool), `{left_g}` or `{used_g}` |
| `POST /spools/ID/money` | An expense in Pocket Money |
| `POST /import/spoolman` | `{url}` (Spoolman's address) or its JSON spool list — again and again |

## Upkeep

| | |
|---|---|
| `GET /upkeep` | `?printer=` → `{upkeep (with due, soon, due_hours, due_on), log}` |
| `POST /upkeep`, `PUT /upkeep/ID`, `DELETE /upkeep/ID` | `{printer_id, title, every_hours, every_days, last_done_on, last_hours, notes}` |
| `POST /upkeep/done` | `{upkeep_id \| printer_id + title, done_on, cost, note, to_home}` |
| `DELETE /upkeep/log/ID` | |

## Data

`GET /export.json`, `POST /import` (the same file twice adds nothing twice), `GET /export.csv` (one row per print), `GET /backup` (admin: the whole database).

## Events (Pocket Core)

`printfarm.print_started`, `print_finished`, `print_failed`, `job_added`, `job_done`, `spool_low`, `upkeep_due`, `model_added` — each with `user_email` and a `url`.
