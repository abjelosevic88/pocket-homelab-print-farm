CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);


-- ---------- Pocket Print Farm ----------
-- Everything belongs to the person who made it (user_id); nobody else on the
-- box sees it, box admins included. Times are UTC ISO strings; days are
-- local YYYY-MM-DD. Money is in minor units (cents) of the person's currency.
-- Weights are grams, lengths millimetres, times seconds. Model files and
-- photos live in DATA_DIR/media/<user id>/.

-- The printers: how each is reached (or "manual": you say when a print
-- starts and ends) and what it costs to run.
CREATE TABLE printers (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  model         TEXT NOT NULL DEFAULT '',          -- "Prusa MK4", "Voron 2.4"
  kind          TEXT NOT NULL DEFAULT 'fdm',       -- fdm | resin
  connection    TEXT NOT NULL DEFAULT 'manual',    -- manual | moonraker | octoprint | prusalink
  url           TEXT NOT NULL DEFAULT '',
  api_key       TEXT NOT NULL DEFAULT '',
  webcam_url    TEXT NOT NULL DEFAULT '',          -- a snapshot (JPEG) address; '' = what the printer says
  bed_x         REAL NOT NULL DEFAULT 0,
  bed_y         REAL NOT NULL DEFAULT 0,
  bed_z         REAL NOT NULL DEFAULT 0,
  nozzle        REAL NOT NULL DEFAULT 0.4,
  materials     TEXT NOT NULL DEFAULT '',          -- what it can print, "PLA, PETG, TPU"
  watts         INTEGER NOT NULL DEFAULT 0,        -- average power while printing
  wear_per_hour INTEGER NOT NULL DEFAULT 0,        -- minor units: nozzles, belts, the printer itself
  bought_on     TEXT NOT NULL DEFAULT '',
  price         INTEGER NOT NULL DEFAULT 0,
  start_hours   REAL NOT NULL DEFAULT 0,           -- hours it had printed before Print Farm
  location      TEXT NOT NULL DEFAULT '',
  color         TEXT NOT NULL DEFAULT '',
  notes         TEXT NOT NULL DEFAULT '',
  spool_id      INTEGER,                           -- the spool loaded now
  photo         TEXT NOT NULL DEFAULT '',
  archived      INTEGER NOT NULL DEFAULT 0,
  position      INTEGER NOT NULL DEFAULT 0,
  -- the last thing the printer said (the poller keeps this up to date)
  last_state    TEXT NOT NULL DEFAULT '',          -- idle | printing | paused | done | error | offline
  last_seen_at  TEXT,
  home_ref      TEXT NOT NULL DEFAULT '',          -- the appliance in Pocket Home
  source        TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);
CREATE INDEX printers_user ON printers(user_id, archived, position);

-- A kind of filament (brand + material + colour), like Spoolman's filaments.
CREATE TABLE filaments (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  brand          TEXT NOT NULL DEFAULT '',
  material       TEXT NOT NULL DEFAULT 'PLA',       -- PLA, PETG, ABS, ASA, TPU, PA, PC, Resin…
  name           TEXT NOT NULL DEFAULT '',          -- the colour's name, "Galaxy Black"
  color          TEXT NOT NULL DEFAULT '',          -- #rrggbb
  diameter       REAL NOT NULL DEFAULT 1.75,
  density        REAL NOT NULL DEFAULT 1.24,        -- g/cm³
  nozzle_temp    INTEGER NOT NULL DEFAULT 0,
  bed_temp       INTEGER NOT NULL DEFAULT 0,
  net_weight     INTEGER NOT NULL DEFAULT 1000,     -- grams of filament on a new spool
  spool_weight   INTEGER NOT NULL DEFAULT 0,        -- grams of the empty spool (for weighing)
  price          INTEGER NOT NULL DEFAULT 0,        -- a new spool, minor units
  url            TEXT NOT NULL DEFAULT '',          -- where to buy it
  notes          TEXT NOT NULL DEFAULT '',
  external_id    TEXT NOT NULL DEFAULT '',          -- spoolman:12
  source         TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT
);
CREATE INDEX filaments_user ON filaments(user_id, material);

-- One spool on the shelf: how much is left of it.
CREATE TABLE spools (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  filament_id  INTEGER NOT NULL REFERENCES filaments(id) ON DELETE CASCADE,
  initial_g    REAL NOT NULL DEFAULT 1000,
  used_g       REAL NOT NULL DEFAULT 0,
  price        INTEGER NOT NULL DEFAULT 0,          -- what this spool cost (0 = the filament's price)
  bought_on    TEXT NOT NULL DEFAULT '',
  opened_on    TEXT NOT NULL DEFAULT '',
  dried_on     TEXT NOT NULL DEFAULT '',
  location     TEXT NOT NULL DEFAULT '',
  lot          TEXT NOT NULL DEFAULT '',
  notes        TEXT NOT NULL DEFAULT '',
  archived     INTEGER NOT NULL DEFAULT 0,          -- empty or gone
  money_ref    TEXT NOT NULL DEFAULT '',            -- the expense in Pocket Money
  inventory_ref TEXT NOT NULL DEFAULT '',
  external_id  TEXT NOT NULL DEFAULT '',            -- spoolman:34
  source       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT
);
CREATE INDEX spools_user ON spools(user_id, archived);
CREATE INDEX spools_filament ON spools(user_id, filament_id);

-- What happened to a spool's weight: a print, a weigh-in, a correction.
CREATE TABLE spool_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  spool_id   INTEGER NOT NULL REFERENCES spools(id) ON DELETE CASCADE,
  print_id   INTEGER,
  kind       TEXT NOT NULL DEFAULT 'print',         -- print | weigh | fix
  grams      REAL NOT NULL DEFAULT 0,               -- used (+) or given back (-)
  note       TEXT NOT NULL DEFAULT '',
  at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX spool_log_spool ON spool_log(user_id, spool_id, at);

-- The model library: things to print.
CREATE TABLE models (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  tags        TEXT NOT NULL DEFAULT '',             -- comma separated
  designer    TEXT NOT NULL DEFAULT '',
  license     TEXT NOT NULL DEFAULT '',
  url         TEXT NOT NULL DEFAULT '',             -- where it came from (Printables, Thingiverse, MakerWorld…)
  cover       TEXT NOT NULL DEFAULT '',             -- a picture, relative to the media dir
  favourite   INTEGER NOT NULL DEFAULT 0,
  archived    INTEGER NOT NULL DEFAULT 0,
  source      TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT
);
CREATE INDEX models_user ON models(user_id, archived, created_at);

-- The files of a model: the shape (STL, 3MF, OBJ, STEP) and the sliced
-- G-code for a printer, with what was read from them.
CREATE TABLE model_files (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  model_id      INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  kind          TEXT NOT NULL DEFAULT 'stl',        -- stl | 3mf | obj | step | gcode | bgcode
  path          TEXT NOT NULL DEFAULT '',           -- relative to the media dir
  size          INTEGER NOT NULL DEFAULT 0,
  sha256        TEXT NOT NULL DEFAULT '',
  thumb         TEXT NOT NULL DEFAULT '',           -- picture from the slicer (or drawn here), relative to the media dir
  size_x        REAL NOT NULL DEFAULT 0,
  size_y        REAL NOT NULL DEFAULT 0,
  size_z        REAL NOT NULL DEFAULT 0,
  volume        REAL NOT NULL DEFAULT 0,            -- mm³ (meshes)
  triangles     INTEGER NOT NULL DEFAULT 0,
  est_seconds   INTEGER NOT NULL DEFAULT 0,         -- the slicer's estimate (G-code)
  est_grams     REAL NOT NULL DEFAULT 0,
  est_mm        REAL NOT NULL DEFAULT 0,            -- filament length
  layer_height  REAL NOT NULL DEFAULT 0,
  nozzle        REAL NOT NULL DEFAULT 0,
  material      TEXT NOT NULL DEFAULT '',
  slicer        TEXT NOT NULL DEFAULT '',
  printer_model TEXT NOT NULL DEFAULT '',
  settings      TEXT NOT NULL DEFAULT '{}',         -- infill, walls, supports, temperatures… (JSON)
  source        TEXT NOT NULL DEFAULT 'upload',     -- upload | files | link
  source_ref    TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX model_files_model ON model_files(user_id, model_id);
CREATE INDEX model_files_name ON model_files(user_id, name);

-- The queue: what to print next, how many, for whom.
CREATE TABLE jobs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title        TEXT NOT NULL,
  model_id     INTEGER REFERENCES models(id) ON DELETE SET NULL,
  file_id      INTEGER REFERENCES model_files(id) ON DELETE SET NULL,
  printer_id   INTEGER REFERENCES printers(id) ON DELETE SET NULL,   -- NULL = the first one free
  filament_id  INTEGER REFERENCES filaments(id) ON DELETE SET NULL,  -- the colour wanted
  material     TEXT NOT NULL DEFAULT '',
  quantity     INTEGER NOT NULL DEFAULT 1,
  done_count   INTEGER NOT NULL DEFAULT 0,
  status       TEXT NOT NULL DEFAULT 'queued',      -- queued | printing | done | cancelled
  position     INTEGER NOT NULL DEFAULT 0,
  due_on       TEXT NOT NULL DEFAULT '',
  for_whom     TEXT NOT NULL DEFAULT '',
  contact_ref  TEXT NOT NULL DEFAULT '',
  price        INTEGER NOT NULL DEFAULT 0,          -- what one is sold for (0 = not sold)
  notes        TEXT NOT NULL DEFAULT '',
  invoice_ref  TEXT NOT NULL DEFAULT '',
  task_ref     TEXT NOT NULL DEFAULT '',
  source       TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT,
  finished_at  TEXT
);
CREATE INDEX jobs_user ON jobs(user_id, status, position);

-- Each print on a printer: the history, with what it cost and how it went.
CREATE TABLE prints (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  printer_id    INTEGER REFERENCES printers(id) ON DELETE SET NULL,
  job_id        INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
  model_id      INTEGER REFERENCES models(id) ON DELETE SET NULL,
  file_id       INTEGER REFERENCES model_files(id) ON DELETE SET NULL,
  spool_id      INTEGER REFERENCES spools(id) ON DELETE SET NULL,
  title         TEXT NOT NULL,
  file_name     TEXT NOT NULL DEFAULT '',           -- the name on the printer
  status        TEXT NOT NULL DEFAULT 'printing',   -- printing | paused | done | failed | cancelled
  started_at    TEXT NOT NULL,
  ended_at      TEXT,
  seconds       INTEGER NOT NULL DEFAULT 0,         -- time it printed
  est_seconds   INTEGER NOT NULL DEFAULT 0,
  progress      REAL NOT NULL DEFAULT 0,            -- 0..1 (last known)
  grams         REAL NOT NULL DEFAULT 0,            -- filament used
  est_grams     REAL NOT NULL DEFAULT 0,
  copies        INTEGER NOT NULL DEFAULT 1,         -- pieces on the plate
  fail_reason   TEXT NOT NULL DEFAULT '',           -- adhesion | spaghetti | warping | clog | layer_shift | stringing | supports | power | filament_out | other
  fail_note     TEXT NOT NULL DEFAULT '',
  rating        INTEGER NOT NULL DEFAULT 0,         -- 1..5 how good it came out
  notes         TEXT NOT NULL DEFAULT '',
  settings      TEXT NOT NULL DEFAULT '{}',         -- layer height, infill, walls, supports, temperatures, speed (JSON)
  keep          INTEGER NOT NULL DEFAULT 0,         -- "these settings worked": shown on the model
  photo         TEXT NOT NULL DEFAULT '',
  filament_cost INTEGER NOT NULL DEFAULT 0,         -- minor units, worked out when it ends
  power_cost    INTEGER NOT NULL DEFAULT 0,
  wear_cost     INTEGER NOT NULL DEFAULT 0,
  external_id   TEXT NOT NULL DEFAULT '',           -- the printer's own job id
  inventory_ref TEXT NOT NULL DEFAULT '',
  calendar_ref  TEXT NOT NULL DEFAULT '',
  source        TEXT NOT NULL DEFAULT '',           -- '' here | printer (seen on the printer) | an app's slug
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);
CREATE INDEX prints_user ON prints(user_id, started_at);
CREATE INDEX prints_printer ON prints(user_id, printer_id, started_at);
CREATE INDEX prints_model ON prints(user_id, model_id);

-- Upkeep: what a printer needs every so many print hours or days.
CREATE TABLE upkeep (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  printer_id   INTEGER NOT NULL REFERENCES printers(id) ON DELETE CASCADE,
  title        TEXT NOT NULL,
  every_hours  REAL NOT NULL DEFAULT 0,             -- print hours (0 = not by hours)
  every_days   INTEGER NOT NULL DEFAULT 0,          -- (0 = not by days)
  last_done_on TEXT NOT NULL DEFAULT '',            -- YYYY-MM-DD
  last_hours   REAL NOT NULL DEFAULT 0,             -- the printer's hours then
  notes        TEXT NOT NULL DEFAULT '',
  task_ref     TEXT NOT NULL DEFAULT '',            -- the task in Pocket Tasks for the next time
  told_at      TEXT NOT NULL DEFAULT '',            -- when we last said it is due
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX upkeep_printer ON upkeep(user_id, printer_id);

-- Upkeep done (and repairs): the printer's log book.
CREATE TABLE upkeep_log (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  printer_id  INTEGER NOT NULL REFERENCES printers(id) ON DELETE CASCADE,
  upkeep_id   INTEGER REFERENCES upkeep(id) ON DELETE SET NULL,
  title       TEXT NOT NULL,
  done_on     TEXT NOT NULL,                        -- YYYY-MM-DD
  hours       REAL NOT NULL DEFAULT 0,              -- the printer's hours then
  cost        INTEGER NOT NULL DEFAULT 0,
  note        TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX upkeep_log_printer ON upkeep_log(user_id, printer_id, done_on);
