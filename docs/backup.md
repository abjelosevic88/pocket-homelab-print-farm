# Backup

Everything lives in `DATA_DIR`: the SQLite file `pocket-printfarm.db` (WAL mode) and `media/<user id>/` — the model files, their pictures and the photos of prints. Take a consistent copy of the database with `pocket-printfarm backup` (writes `DATA_DIR/backups/pocket-printfarm-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin), and copy `media/` as it is. Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-printfarm.db` (remove the `-wal`/`-shm` files) and `media/`, start.

Every person can also export everything they made — printers (without their API keys), filament and spools, models with what was read from their files, the queue, every print and the upkeep — as one JSON file (Settings → Backup & data), and bring it back on any box; importing the same file twice adds nothing twice. The model files themselves come back with a backup of the folder.
