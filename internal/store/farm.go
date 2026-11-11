package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Every query here is scoped to one person (user_id); the only exceptions
// are the poller's helpers at the end, which work by printer id for the
// background loop and never return content to a request.

type scanner interface{ Scan(...any) error }

func np(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func ns(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func rawJSON(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" || !json.Valid([]byte(s)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}

func jsonText(r json.RawMessage) string {
	if len(r) == 0 || !json.Valid(r) {
		return "{}"
	}
	return string(r)
}

// ---------- printers ----------

const printerCols = `p.id, p.user_id, p.name, p.model, p.kind, p.connection, p.url, p.api_key, p.webcam_url, p.bed_x, p.bed_y, p.bed_z, p.nozzle,
 p.materials, p.watts, p.wear_per_hour, p.bought_on, p.price, p.start_hours, p.location, p.color, p.notes, p.spool_id, p.photo, p.archived, p.position,
 p.last_state, p.last_seen_at, p.home_ref, p.source, p.created_at, p.updated_at,
 p.start_hours + COALESCE((SELECT SUM(x.seconds) FROM prints x WHERE x.printer_id = p.id), 0) / 3600.0,
 (SELECT COUNT(1) FROM prints x WHERE x.printer_id = p.id AND x.status IN ('done','failed')),
 (SELECT COUNT(1) FROM prints x WHERE x.printer_id = p.id AND x.status = 'failed')`

func scanPrinter(r scanner) (*Printer, error) {
	var p Printer
	var spool sql.NullInt64
	var seen, upd sql.NullString
	var arch int
	if err := r.Scan(&p.ID, &p.UserID, &p.Name, &p.Model, &p.Kind, &p.Connection, &p.URL, &p.APIKey, &p.WebcamURL, &p.BedX, &p.BedY, &p.BedZ, &p.Nozzle,
		&p.Materials, &p.Watts, &p.WearPerHour, &p.BoughtOn, &p.Price, &p.StartHours, &p.Location, &p.Color, &p.Notes, &spool, &p.Photo, &arch, &p.Position,
		&p.LastState, &seen, &p.HomeRef, &p.Source, &p.CreatedAt, &upd, &p.Hours, &p.Prints, &p.Failed); err != nil {
		return nil, err
	}
	p.SpoolID, p.LastSeenAt, p.UpdatedAt, p.Archived = np(spool), ns(seen), ns(upd), arch == 1
	p.HasPhoto, p.HasKey = p.Photo != "", p.APIKey != ""
	return &p, nil
}

// ListPrinters lists a person's printers in their order.
func (s *Store) ListPrinters(ctx context.Context, uid int64, archived bool) ([]Printer, error) {
	q := `SELECT ` + printerCols + ` FROM printers p WHERE p.user_id = ?`
	if !archived {
		q += ` AND p.archived = 0`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY p.archived, p.position, p.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Printer{}
	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPrinter returns one of a person's printers.
func (s *Store) GetPrinter(ctx context.Context, uid, id int64) (*Printer, error) {
	p, err := scanPrinter(s.DB.QueryRowContext(ctx, `SELECT `+printerCols+` FROM printers p WHERE p.user_id = ? AND p.id = ?`, uid, id))
	return p, notFound(err)
}

// CreatePrinter adds a printer at the end of the list.
func (s *Store) CreatePrinter(ctx context.Context, p *Printer) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO printers (user_id, name, model, kind, connection, url, api_key, webcam_url, bed_x, bed_y, bed_z, nozzle, materials, watts,
		wear_per_hour, bought_on, price, start_hours, location, color, notes, spool_id, archived, position, home_ref, source)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?, (SELECT COALESCE(MAX(position),0)+1 FROM printers WHERE user_id = ?), ?, ?)`,
		p.UserID, p.Name, p.Model, p.Kind, p.Connection, p.URL, p.APIKey, p.WebcamURL, p.BedX, p.BedY, p.BedZ, p.Nozzle, p.Materials, p.Watts,
		p.WearPerHour, p.BoughtOn, p.Price, p.StartHours, p.Location, p.Color, p.Notes, nullI(p.SpoolID), boolInt(p.Archived), p.UserID, p.HomeRef, p.Source)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdatePrinter saves a printer's fields.
func (s *Store) UpdatePrinter(ctx context.Context, p *Printer) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE printers SET name=?, model=?, kind=?, connection=?, url=?, api_key=?, webcam_url=?, bed_x=?, bed_y=?, bed_z=?, nozzle=?,
		materials=?, watts=?, wear_per_hour=?, bought_on=?, price=?, start_hours=?, location=?, color=?, notes=?, spool_id=?, photo=?, archived=?, position=?, home_ref=?, updated_at=?
		WHERE user_id = ? AND id = ?`,
		p.Name, p.Model, p.Kind, p.Connection, p.URL, p.APIKey, p.WebcamURL, p.BedX, p.BedY, p.BedZ, p.Nozzle,
		p.Materials, p.Watts, p.WearPerHour, p.BoughtOn, p.Price, p.StartHours, p.Location, p.Color, p.Notes, nullI(p.SpoolID), p.Photo, boolInt(p.Archived), p.Position, p.HomeRef, Now(),
		p.UserID, p.ID)
	return affected(res, err)
}

// DeletePrinter removes a printer (its prints stay, without the printer).
func (s *Store) DeletePrinter(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM printers WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// ReorderPrinters sets the order of a person's printers.
func (s *Store) ReorderPrinters(ctx context.Context, uid int64, ids []int64) error {
	return s.reorder(ctx, "printers", uid, ids)
}

func (s *Store) reorder(ctx context.Context, table string, uid int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET position = ? WHERE user_id = ? AND id = ?`, i+1, uid, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SetPrinterState records what a printer last said (the poller).
func (s *Store) SetPrinterState(ctx context.Context, id int64, state string, seen bool) error {
	if seen {
		_, err := s.DB.ExecContext(ctx, `UPDATE printers SET last_state = ?, last_seen_at = ? WHERE id = ?`, state, Now(), id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE printers SET last_state = ? WHERE id = ?`, state, id)
	return err
}

// ConnectedPrinters lists every printer the poller asks (all people).
func (s *Store) ConnectedPrinters(ctx context.Context) ([]Printer, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+printerCols+` FROM printers p WHERE p.archived = 0 AND p.connection != 'manual' AND p.url != '' ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Printer{}
	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ---------- filaments ----------

const filamentCols = `f.id, f.user_id, f.brand, f.material, f.name, f.color, f.diameter, f.density, f.nozzle_temp, f.bed_temp, f.net_weight, f.spool_weight, f.price,
 f.url, f.notes, f.external_id, f.source, f.created_at, f.updated_at,
 (SELECT COUNT(1) FROM spools x WHERE x.filament_id = f.id AND x.archived = 0),
 COALESCE((SELECT SUM(MAX(x.initial_g - x.used_g, 0)) FROM spools x WHERE x.filament_id = f.id AND x.archived = 0), 0)`

func scanFilament(r scanner) (*Filament, error) {
	var f Filament
	var upd sql.NullString
	if err := r.Scan(&f.ID, &f.UserID, &f.Brand, &f.Material, &f.Name, &f.Color, &f.Diameter, &f.Density, &f.NozzleTemp, &f.BedTemp, &f.NetWeight, &f.SpoolWeight, &f.Price,
		&f.URL, &f.Notes, &f.ExternalID, &f.Source, &f.CreatedAt, &upd, &f.Spools, &f.LeftGrams); err != nil {
		return nil, err
	}
	f.UpdatedAt = ns(upd)
	return &f, nil
}

// ListFilaments lists a person's filaments.
func (s *Store) ListFilaments(ctx context.Context, uid int64) ([]Filament, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+filamentCols+` FROM filaments f WHERE f.user_id = ? ORDER BY f.material, f.brand, f.name, f.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Filament{}
	for rows.Next() {
		f, err := scanFilament(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetFilament returns one filament.
func (s *Store) GetFilament(ctx context.Context, uid, id int64) (*Filament, error) {
	f, err := scanFilament(s.DB.QueryRowContext(ctx, `SELECT `+filamentCols+` FROM filaments f WHERE f.user_id = ? AND f.id = ?`, uid, id))
	return f, notFound(err)
}

// FilamentByExternal finds a filament brought in from elsewhere (spoolman:12).
func (s *Store) FilamentByExternal(ctx context.Context, uid int64, ext string) (*Filament, error) {
	f, err := scanFilament(s.DB.QueryRowContext(ctx, `SELECT `+filamentCols+` FROM filaments f WHERE f.user_id = ? AND f.external_id = ?`, uid, ext))
	return f, notFound(err)
}

// CreateFilament adds a filament.
func (s *Store) CreateFilament(ctx context.Context, f *Filament) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO filaments (user_id, brand, material, name, color, diameter, density, nozzle_temp, bed_temp, net_weight, spool_weight, price, url, notes, external_id, source)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		f.UserID, f.Brand, f.Material, f.Name, f.Color, f.Diameter, f.Density, f.NozzleTemp, f.BedTemp, f.NetWeight, f.SpoolWeight, f.Price, f.URL, f.Notes, f.ExternalID, f.Source)
	if err != nil {
		return err
	}
	f.ID, _ = res.LastInsertId()
	return nil
}

// UpdateFilament saves a filament.
func (s *Store) UpdateFilament(ctx context.Context, f *Filament) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE filaments SET brand=?, material=?, name=?, color=?, diameter=?, density=?, nozzle_temp=?, bed_temp=?, net_weight=?, spool_weight=?, price=?,
		url=?, notes=?, updated_at=? WHERE user_id = ? AND id = ?`,
		f.Brand, f.Material, f.Name, f.Color, f.Diameter, f.Density, f.NozzleTemp, f.BedTemp, f.NetWeight, f.SpoolWeight, f.Price, f.URL, f.Notes, Now(), f.UserID, f.ID)
	return affected(res, err)
}

// DeleteFilament removes a filament and its spools.
func (s *Store) DeleteFilament(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM filaments WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// ---------- spools ----------

const spoolCols = `s.id, s.user_id, s.filament_id, s.initial_g, s.used_g, s.price, s.bought_on, s.opened_on, s.dried_on, s.location, s.lot, s.notes, s.archived,
 s.money_ref, s.inventory_ref, s.external_id, s.source, s.last_used_at, s.created_at, s.updated_at,
 f.brand, f.material, f.name, f.color, f.density, f.diameter, f.spool_weight, f.price,
 (SELECT p.id FROM printers p WHERE p.spool_id = s.id AND p.user_id = s.user_id LIMIT 1),
 COALESCE((SELECT p.name FROM printers p WHERE p.spool_id = s.id AND p.user_id = s.user_id LIMIT 1), '')`

func scanSpool(r scanner) (*Spool, error) {
	var x Spool
	var last, upd sql.NullString
	var pid sql.NullInt64
	var arch int
	if err := r.Scan(&x.ID, &x.UserID, &x.FilamentID, &x.InitialG, &x.UsedG, &x.Price, &x.BoughtOn, &x.OpenedOn, &x.DriedOn, &x.Location, &x.Lot, &x.Notes, &arch,
		&x.MoneyRef, &x.InventoryRef, &x.ExternalID, &x.Source, &last, &x.CreatedAt, &upd,
		&x.Brand, &x.Material, &x.Name, &x.Color, &x.Density, &x.Diameter, &x.SpoolWeight, &x.FilamentPrice, &pid, &x.Printer); err != nil {
		return nil, err
	}
	x.Archived, x.LastUsedAt, x.UpdatedAt, x.PrinterID = arch == 1, ns(last), ns(upd), np(pid)
	x.LeftG = x.InitialG - x.UsedG
	if x.LeftG < 0 {
		x.LeftG = 0
	}
	return &x, nil
}

// SpoolFilter narrows ListSpools.
type SpoolFilter struct {
	Archived   bool // the empty ones too
	FilamentID int64
	Material   string
}

// ListSpools lists a person's spools, the loaded ones first then by what is left.
func (s *Store) ListSpools(ctx context.Context, uid int64, f SpoolFilter) ([]Spool, error) {
	q := `SELECT ` + spoolCols + ` FROM spools s JOIN filaments f ON f.id = s.filament_id WHERE s.user_id = ?`
	args := []any{uid}
	if !f.Archived {
		q += ` AND s.archived = 0`
	}
	if f.FilamentID > 0 {
		q += ` AND s.filament_id = ?`
		args = append(args, f.FilamentID)
	}
	if f.Material != "" {
		q += ` AND f.material = ? COLLATE NOCASE`
		args = append(args, f.Material)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY s.archived, f.material, f.brand, f.name, s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Spool{}
	for rows.Next() {
		x, err := scanSpool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetSpool returns one spool.
func (s *Store) GetSpool(ctx context.Context, uid, id int64) (*Spool, error) {
	x, err := scanSpool(s.DB.QueryRowContext(ctx, `SELECT `+spoolCols+` FROM spools s JOIN filaments f ON f.id = s.filament_id WHERE s.user_id = ? AND s.id = ?`, uid, id))
	return x, notFound(err)
}

// SpoolByExternal finds a spool brought in from elsewhere.
func (s *Store) SpoolByExternal(ctx context.Context, uid int64, ext string) (*Spool, error) {
	x, err := scanSpool(s.DB.QueryRowContext(ctx, `SELECT `+spoolCols+` FROM spools s JOIN filaments f ON f.id = s.filament_id WHERE s.user_id = ? AND s.external_id = ?`, uid, ext))
	return x, notFound(err)
}

// CreateSpool adds a spool.
func (s *Store) CreateSpool(ctx context.Context, x *Spool) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO spools (user_id, filament_id, initial_g, used_g, price, bought_on, opened_on, dried_on, location, lot, notes, archived, money_ref, inventory_ref, external_id, source)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.UserID, x.FilamentID, x.InitialG, x.UsedG, x.Price, x.BoughtOn, x.OpenedOn, x.DriedOn, x.Location, x.Lot, x.Notes, boolInt(x.Archived), x.MoneyRef, x.InventoryRef, x.ExternalID, x.Source)
	if err != nil {
		return err
	}
	x.ID, _ = res.LastInsertId()
	return nil
}

// UpdateSpool saves a spool.
func (s *Store) UpdateSpool(ctx context.Context, x *Spool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE spools SET filament_id=?, initial_g=?, used_g=?, price=?, bought_on=?, opened_on=?, dried_on=?, location=?, lot=?, notes=?, archived=?,
		money_ref=?, inventory_ref=?, updated_at=? WHERE user_id = ? AND id = ?`,
		x.FilamentID, x.InitialG, x.UsedG, x.Price, x.BoughtOn, x.OpenedOn, x.DriedOn, x.Location, x.Lot, x.Notes, boolInt(x.Archived), x.MoneyRef, x.InventoryRef, Now(), x.UserID, x.ID)
	if err != nil {
		return err
	}
	if x.Archived {
		_, _ = s.DB.ExecContext(ctx, `UPDATE printers SET spool_id = NULL WHERE user_id = ? AND spool_id = ?`, x.UserID, x.ID)
	}
	return affected(res, err)
}

// DeleteSpool removes a spool (it is unloaded from its printer).
func (s *Store) DeleteSpool(ctx context.Context, uid, id int64) error {
	_, _ = s.DB.ExecContext(ctx, `UPDATE printers SET spool_id = NULL WHERE user_id = ? AND spool_id = ?`, uid, id)
	res, err := s.DB.ExecContext(ctx, `DELETE FROM spools WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// UseSpool takes grams off a spool (negative gives them back) and logs why.
// A print's grams are logged once: using it again for the same print
// replaces the earlier entry.
func (s *Store) UseSpool(ctx context.Context, uid, spoolID int64, grams float64, kind, note string, printID *int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var ok int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM spools WHERE user_id = ? AND id = ?`, uid, spoolID).Scan(&ok); err != nil {
		return notFound(err)
	}
	if printID != nil && kind == "print" {
		var old float64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(grams),0) FROM spool_log WHERE user_id = ? AND spool_id = ? AND print_id = ? AND kind = 'print'`, uid, spoolID, *printID).Scan(&old); err == nil && old != 0 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM spool_log WHERE user_id = ? AND spool_id = ? AND print_id = ? AND kind = 'print'`, uid, spoolID, *printID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE spools SET used_g = used_g - ? WHERE user_id = ? AND id = ?`, old, uid, spoolID); err != nil {
				return err
			}
		}
	}
	if grams != 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO spool_log (user_id, spool_id, print_id, kind, grams, note) VALUES (?,?,?,?,?,?)`, uid, spoolID, nullI(printID), kind, grams, note); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE spools SET used_g = MAX(used_g + ?, 0), last_used_at = ?, opened_on = CASE WHEN opened_on = '' THEN ? ELSE opened_on END, updated_at = ? WHERE user_id = ? AND id = ?`,
		grams, Now(), Today(), Now(), uid, spoolID); err != nil {
		return err
	}
	return tx.Commit()
}

// LogSpool writes a change into a spool's log without changing its weight.
func (s *Store) LogSpool(ctx context.Context, uid, spoolID int64, kind string, grams float64, note string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO spool_log (user_id, spool_id, kind, grams, note) VALUES (?,?,?,?,?)`, uid, spoolID, kind, grams, note)
	return err
}

// SpoolLog is a spool's history, newest first.
func (s *Store) SpoolLog(ctx context.Context, uid, spoolID int64, limit int) ([]SpoolLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT l.id, l.spool_id, l.print_id, l.kind, l.grams, l.note, l.at, COALESCE(p.title, '')
		FROM spool_log l LEFT JOIN prints p ON p.id = l.print_id AND p.user_id = l.user_id
		WHERE l.user_id = ? AND l.spool_id = ? ORDER BY l.at DESC, l.id DESC LIMIT ?`, uid, spoolID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SpoolLog{}
	for rows.Next() {
		var l SpoolLog
		var pid sql.NullInt64
		if err := rows.Scan(&l.ID, &l.SpoolID, &pid, &l.Kind, &l.Grams, &l.Note, &l.At, &l.Title); err != nil {
			return nil, err
		}
		l.PrintID = np(pid)
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------- models ----------

const modelCols = `m.id, m.user_id, m.name, m.description, m.tags, m.designer, m.license, m.url, m.cover, m.favourite, m.archived, m.source, m.created_at, m.updated_at,
 (SELECT COUNT(1) FROM model_files f WHERE f.model_id = m.id),
 COALESCE((SELECT GROUP_CONCAT(DISTINCT f.kind) FROM model_files f WHERE f.model_id = m.id), ''),
 (SELECT COUNT(1) FROM prints p WHERE p.model_id = m.id AND p.status IN ('done','failed')),
 (SELECT COUNT(1) FROM prints p WHERE p.model_id = m.id AND p.status = 'done'),
 (SELECT MAX(p.started_at) FROM prints p WHERE p.model_id = m.id),
 COALESCE((SELECT f.est_seconds FROM model_files f WHERE f.model_id = m.id AND f.est_seconds > 0 ORDER BY f.id LIMIT 1), 0),
 COALESCE((SELECT f.est_grams FROM model_files f WHERE f.model_id = m.id AND f.est_grams > 0 ORDER BY f.id LIMIT 1), 0),
 COALESCE((SELECT f.size_x FROM model_files f WHERE f.model_id = m.id AND f.size_x > 0 ORDER BY f.kind IN ('gcode','bgcode'), f.id LIMIT 1), 0),
 COALESCE((SELECT f.size_y FROM model_files f WHERE f.model_id = m.id AND f.size_x > 0 ORDER BY f.kind IN ('gcode','bgcode'), f.id LIMIT 1), 0),
 COALESCE((SELECT f.size_z FROM model_files f WHERE f.model_id = m.id AND f.size_x > 0 ORDER BY f.kind IN ('gcode','bgcode'), f.id LIMIT 1), 0)`

func scanModel(r scanner) (*Model, error) {
	var m Model
	var upd, last sql.NullString
	var fav, arch int
	if err := r.Scan(&m.ID, &m.UserID, &m.Name, &m.Description, &m.Tags, &m.Designer, &m.License, &m.URL, &m.Cover, &fav, &arch, &m.Source, &m.CreatedAt, &upd,
		&m.Files, &m.Kinds, &m.Prints, &m.Good, &last, &m.EstSeconds, &m.EstGrams, &m.SizeX, &m.SizeY, &m.SizeZ); err != nil {
		return nil, err
	}
	m.Favourite, m.Archived, m.UpdatedAt, m.LastPrint, m.HasCover = fav == 1, arch == 1, ns(upd), ns(last), m.Cover != ""
	return &m, nil
}

// ModelFilter narrows ListModels.
type ModelFilter struct {
	Q         string
	Tag       string
	Favourite bool
	Archived  bool
	Sort      string // added | name | printed
	Limit     int
	Offset    int
}

// ListModels lists a person's models and how many there are in all.
func (s *Store) ListModels(ctx context.Context, uid int64, f ModelFilter) ([]Model, int, error) {
	where := ` WHERE m.user_id = ?`
	args := []any{uid}
	if !f.Archived {
		where += ` AND m.archived = 0`
	}
	if f.Favourite {
		where += ` AND m.favourite = 1`
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where += ` AND (m.name LIKE ? OR m.tags LIKE ? OR m.designer LIKE ? OR m.description LIKE ? OR EXISTS (SELECT 1 FROM model_files x WHERE x.model_id = m.id AND x.name LIKE ?))`
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like)
	}
	if t := strings.TrimSpace(f.Tag); t != "" {
		where += ` AND (',' || REPLACE(LOWER(m.tags), ', ', ',') || ',') LIKE ?`
		args = append(args, "%,"+strings.ToLower(t)+",%")
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM models m`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := ` ORDER BY m.created_at DESC, m.id DESC`
	switch f.Sort {
	case "name":
		order = ` ORDER BY m.name COLLATE NOCASE, m.id`
	case "printed":
		order = ` ORDER BY (SELECT MAX(p.started_at) FROM prints p WHERE p.model_id = m.id) IS NULL, (SELECT MAX(p.started_at) FROM prints p WHERE p.model_id = m.id) DESC, m.id DESC`
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+modelCols+` FROM models m`+where+order+` LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Model{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *m)
	}
	return out, total, rows.Err()
}

// GetModel returns one model.
func (s *Store) GetModel(ctx context.Context, uid, id int64) (*Model, error) {
	m, err := scanModel(s.DB.QueryRowContext(ctx, `SELECT `+modelCols+` FROM models m WHERE m.user_id = ? AND m.id = ?`, uid, id))
	return m, notFound(err)
}

// CreateModel adds a model.
func (s *Store) CreateModel(ctx context.Context, m *Model) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO models (user_id, name, description, tags, designer, license, url, cover, favourite, archived, source) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		m.UserID, m.Name, m.Description, m.Tags, m.Designer, m.License, m.URL, m.Cover, boolInt(m.Favourite), boolInt(m.Archived), m.Source)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

// UpdateModel saves a model.
func (s *Store) UpdateModel(ctx context.Context, m *Model) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE models SET name=?, description=?, tags=?, designer=?, license=?, url=?, cover=?, favourite=?, archived=?, updated_at=? WHERE user_id = ? AND id = ?`,
		m.Name, m.Description, m.Tags, m.Designer, m.License, m.URL, m.Cover, boolInt(m.Favourite), boolInt(m.Archived), Now(), m.UserID, m.ID)
	return affected(res, err)
}

// DeleteModel removes a model and its files' rows (the caller removes the files).
func (s *Store) DeleteModel(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM models WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// ModelTags counts the tags a person uses.
func (s *Store) ModelTags(ctx context.Context, uid int64) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tags FROM models WHERE user_id = ? AND archived = 0 AND tags != ''`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		for _, x := range strings.Split(t, ",") {
			if x = strings.ToLower(strings.TrimSpace(x)); x != "" {
				out[x]++
			}
		}
	}
	return out, rows.Err()
}

// ---------- model files ----------

const fileCols = `id, user_id, model_id, name, kind, path, size, sha256, thumb, size_x, size_y, size_z, volume, triangles, est_seconds, est_grams, est_mm,
 layer_height, nozzle, material, slicer, printer_model, settings, source, source_ref, created_at`

func scanFile(r scanner) (*ModelFile, error) {
	var f ModelFile
	var settings string
	if err := r.Scan(&f.ID, &f.UserID, &f.ModelID, &f.Name, &f.Kind, &f.Path, &f.Size, &f.SHA256, &f.Thumb, &f.SizeX, &f.SizeY, &f.SizeZ, &f.Volume, &f.Triangles,
		&f.EstSeconds, &f.EstGrams, &f.EstMM, &f.LayerHeight, &f.Nozzle, &f.Material, &f.Slicer, &f.PrinterModel, &settings, &f.Source, &f.SourceRef, &f.CreatedAt); err != nil {
		return nil, err
	}
	f.Settings, f.HasThumb = rawJSON(settings), f.Thumb != ""
	f.Printable = f.Kind == "gcode" || f.Kind == "bgcode"
	return &f, nil
}

func (s *Store) listFiles(ctx context.Context, q string, args ...any) ([]ModelFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+fileCols+` FROM model_files `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelFile{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// ListFiles lists a model's files, the shapes before the G-code.
func (s *Store) ListFiles(ctx context.Context, uid, modelID int64) ([]ModelFile, error) {
	return s.listFiles(ctx, `WHERE user_id = ? AND model_id = ? ORDER BY kind IN ('gcode','bgcode'), id`, uid, modelID)
}

// AllFiles lists every file of a person (export, stats).
func (s *Store) AllFiles(ctx context.Context, uid int64) ([]ModelFile, error) {
	return s.listFiles(ctx, `WHERE user_id = ? ORDER BY id`, uid)
}

// GetFile returns one file.
func (s *Store) GetFile(ctx context.Context, uid, id int64) (*ModelFile, error) {
	f, err := scanFile(s.DB.QueryRowContext(ctx, `SELECT `+fileCols+` FROM model_files WHERE user_id = ? AND id = ?`, uid, id))
	return f, notFound(err)
}

// FileByName finds the newest file with this name (a printer's file).
func (s *Store) FileByName(ctx context.Context, uid int64, name string) (*ModelFile, error) {
	f, err := scanFile(s.DB.QueryRowContext(ctx, `SELECT `+fileCols+` FROM model_files WHERE user_id = ? AND name = ? COLLATE NOCASE ORDER BY id DESC LIMIT 1`, uid, name))
	return f, notFound(err)
}

// FileBySHA finds a file already in the library.
func (s *Store) FileBySHA(ctx context.Context, uid int64, sha string) (*ModelFile, error) {
	f, err := scanFile(s.DB.QueryRowContext(ctx, `SELECT `+fileCols+` FROM model_files WHERE user_id = ? AND sha256 = ? ORDER BY id LIMIT 1`, uid, sha))
	return f, notFound(err)
}

// CreateFile adds a file to a model.
func (s *Store) CreateFile(ctx context.Context, f *ModelFile) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO model_files (user_id, model_id, name, kind, path, size, sha256, thumb, size_x, size_y, size_z, volume, triangles,
		est_seconds, est_grams, est_mm, layer_height, nozzle, material, slicer, printer_model, settings, source, source_ref) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		f.UserID, f.ModelID, f.Name, f.Kind, f.Path, f.Size, f.SHA256, f.Thumb, f.SizeX, f.SizeY, f.SizeZ, f.Volume, f.Triangles,
		f.EstSeconds, f.EstGrams, f.EstMM, f.LayerHeight, f.Nozzle, f.Material, f.Slicer, f.PrinterModel, jsonText(f.Settings), f.Source, f.SourceRef)
	if err != nil {
		return err
	}
	f.ID, _ = res.LastInsertId()
	return nil
}

// UpdateFile saves a file's name, picture and what was read from it.
func (s *Store) UpdateFile(ctx context.Context, f *ModelFile) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE model_files SET model_id=?, name=?, thumb=?, size_x=?, size_y=?, size_z=?, volume=?, triangles=?, est_seconds=?, est_grams=?, est_mm=?,
		layer_height=?, nozzle=?, material=?, slicer=?, printer_model=?, settings=? WHERE user_id = ? AND id = ?`,
		f.ModelID, f.Name, f.Thumb, f.SizeX, f.SizeY, f.SizeZ, f.Volume, f.Triangles, f.EstSeconds, f.EstGrams, f.EstMM,
		f.LayerHeight, f.Nozzle, f.Material, f.Slicer, f.PrinterModel, jsonText(f.Settings), f.UserID, f.ID)
	return affected(res, err)
}

// DeleteFile removes a file's row.
func (s *Store) DeleteFile(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM model_files WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// ---------- jobs (the queue) ----------

const jobCols = `j.id, j.user_id, j.title, j.model_id, j.file_id, j.printer_id, j.filament_id, j.material, j.quantity, j.done_count, j.status, j.position, j.due_on,
 j.for_whom, j.contact_ref, j.price, j.notes, j.invoice_ref, j.task_ref, j.source, j.created_at, j.updated_at, j.finished_at,
 COALESCE(m.name, ''), COALESCE(mf.name, ''), COALESCE(pr.name, ''),
 COALESCE(TRIM(f.brand || ' ' || f.material || ' ' || f.name), ''), COALESCE(f.color, ''),
 COALESCE(mf.est_seconds, 0), COALESCE(mf.est_grams, 0), COALESCE(m.cover, '') != '',
 (SELECT COUNT(1) FROM prints x WHERE x.job_id = j.id AND x.status IN ('printing','paused')),
 (SELECT COUNT(1) FROM prints x WHERE x.job_id = j.id AND x.status = 'failed')`

const jobFrom = ` FROM jobs j LEFT JOIN models m ON m.id = j.model_id LEFT JOIN model_files mf ON mf.id = j.file_id
 LEFT JOIN printers pr ON pr.id = j.printer_id LEFT JOIN filaments f ON f.id = j.filament_id`

func scanJob(r scanner) (*Job, error) {
	var j Job
	var model, file, printer, fil sql.NullInt64
	var upd, fin sql.NullString
	if err := r.Scan(&j.ID, &j.UserID, &j.Title, &model, &file, &printer, &fil, &j.Material, &j.Quantity, &j.DoneCount, &j.Status, &j.Position, &j.DueOn,
		&j.ForWhom, &j.ContactRef, &j.Price, &j.Notes, &j.InvoiceRef, &j.TaskRef, &j.Source, &j.CreatedAt, &upd, &fin,
		&j.ModelName, &j.FileName, &j.Printer, &j.Filament, &j.Color, &j.EstSeconds, &j.EstGrams, &j.HasCover, &j.Printing, &j.Failed); err != nil {
		return nil, err
	}
	j.ModelID, j.FileID, j.PrinterID, j.FilamentID, j.UpdatedAt, j.FinishedAt = np(model), np(file), np(printer), np(fil), ns(upd), ns(fin)
	return &j, nil
}

// ListJobs lists a person's jobs with these statuses (” = all), in queue order.
func (s *Store) ListJobs(ctx context.Context, uid int64, statuses ...string) ([]Job, error) {
	q := `SELECT ` + jobCols + jobFrom + ` WHERE j.user_id = ?`
	args := []any{uid}
	if len(statuses) > 0 {
		q += ` AND j.status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY CASE j.status WHEN 'printing' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END, CASE WHEN j.status IN ('done','cancelled') THEN j.finished_at END DESC, j.position, j.id LIMIT 1000`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// GetJob returns one job.
func (s *Store) GetJob(ctx context.Context, uid, id int64) (*Job, error) {
	j, err := scanJob(s.DB.QueryRowContext(ctx, `SELECT `+jobCols+jobFrom+` WHERE j.user_id = ? AND j.id = ?`, uid, id))
	return j, notFound(err)
}

// CreateJob adds a job at the end of the queue.
func (s *Store) CreateJob(ctx context.Context, j *Job) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO jobs (user_id, title, model_id, file_id, printer_id, filament_id, material, quantity, done_count, status, position, due_on,
		for_whom, contact_ref, price, notes, invoice_ref, task_ref, source, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?, (SELECT COALESCE(MAX(position),0)+1 FROM jobs WHERE user_id = ?), ?,?,?,?,?,?,?,?,?)`,
		j.UserID, j.Title, nullI(j.ModelID), nullI(j.FileID), nullI(j.PrinterID), nullI(j.FilamentID), j.Material, j.Quantity, j.DoneCount, j.Status, j.UserID, j.DueOn,
		j.ForWhom, j.ContactRef, j.Price, j.Notes, j.InvoiceRef, j.TaskRef, j.Source, j.FinishedAt)
	if err != nil {
		return err
	}
	j.ID, _ = res.LastInsertId()
	return nil
}

// UpdateJob saves a job.
func (s *Store) UpdateJob(ctx context.Context, j *Job) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE jobs SET title=?, model_id=?, file_id=?, printer_id=?, filament_id=?, material=?, quantity=?, done_count=?, status=?, due_on=?,
		for_whom=?, contact_ref=?, price=?, notes=?, invoice_ref=?, task_ref=?, finished_at=?, updated_at=? WHERE user_id = ? AND id = ?`,
		j.Title, nullI(j.ModelID), nullI(j.FileID), nullI(j.PrinterID), nullI(j.FilamentID), j.Material, j.Quantity, j.DoneCount, j.Status, j.DueOn,
		j.ForWhom, j.ContactRef, j.Price, j.Notes, j.InvoiceRef, j.TaskRef, j.FinishedAt, Now(), j.UserID, j.ID)
	return affected(res, err)
}

// DeleteJob removes a job (its prints stay).
func (s *Store) DeleteJob(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM jobs WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// ReorderJobs sets the queue order.
func (s *Store) ReorderJobs(ctx context.Context, uid int64, ids []int64) error {
	return s.reorder(ctx, "jobs", uid, ids)
}

// ---------- prints ----------

const printCols = `p.id, p.user_id, p.printer_id, p.job_id, p.model_id, p.file_id, p.spool_id, p.title, p.file_name, p.status, p.started_at, p.ended_at, p.seconds,
 p.est_seconds, p.progress, p.grams, p.est_grams, p.copies, p.fail_reason, p.fail_note, p.rating, p.notes, p.settings, p.keep, p.photo,
 p.filament_cost, p.power_cost, p.wear_cost, p.external_id, p.inventory_ref, p.calendar_ref, p.source, p.created_at, p.updated_at,
 COALESCE(pr.name, ''), COALESCE(TRIM(f.brand || ' ' || f.material || ' ' || f.name), ''), COALESCE(f.color, ''), COALESCE(f.material, ''), COALESCE(m.cover, '') != ''`

const printFrom = ` FROM prints p LEFT JOIN printers pr ON pr.id = p.printer_id LEFT JOIN spools s ON s.id = p.spool_id
 LEFT JOIN filaments f ON f.id = s.filament_id LEFT JOIN models m ON m.id = p.model_id`

func scanPrint(r scanner) (*Print, error) {
	var p Print
	var printer, job, model, file, spool sql.NullInt64
	var ended, upd sql.NullString
	var settings string
	var keep int
	if err := r.Scan(&p.ID, &p.UserID, &printer, &job, &model, &file, &spool, &p.Title, &p.FileName, &p.Status, &p.StartedAt, &ended, &p.Seconds,
		&p.EstSeconds, &p.Progress, &p.Grams, &p.EstGrams, &p.Copies, &p.FailReason, &p.FailNote, &p.Rating, &p.Notes, &settings, &keep, &p.Photo,
		&p.FilamentCost, &p.PowerCost, &p.WearCost, &p.ExternalID, &p.InventoryRef, &p.CalendarRef, &p.Source, &p.CreatedAt, &upd,
		&p.Printer, &p.Spool, &p.Color, &p.Material, &p.HasCover); err != nil {
		return nil, err
	}
	p.PrinterID, p.JobID, p.ModelID, p.FileID, p.SpoolID = np(printer), np(job), np(model), np(file), np(spool)
	p.EndedAt, p.UpdatedAt, p.Settings, p.Keep, p.HasPhoto = ns(ended), ns(upd), rawJSON(settings), keep == 1, p.Photo != ""
	p.Cost = p.FilamentCost + p.PowerCost + p.WearCost
	return &p, nil
}

// PrintFilter narrows ListPrints.
type PrintFilter struct {
	PrinterID int64
	ModelID   int64
	JobID     int64
	SpoolID   int64
	Status    []string
	From, To  string // started_at, UTC ISO
	Q         string
	Keep      bool
	Limit     int
	Offset    int
}

// ListPrints lists a person's prints, newest first.
func (s *Store) ListPrints(ctx context.Context, uid int64, f PrintFilter) ([]Print, int, error) {
	where := ` WHERE p.user_id = ?`
	args := []any{uid}
	add := func(cond string, v ...any) { where += ` AND ` + cond; args = append(args, v...) }
	if f.PrinterID > 0 {
		add(`p.printer_id = ?`, f.PrinterID)
	}
	if f.ModelID > 0 {
		add(`p.model_id = ?`, f.ModelID)
	}
	if f.JobID > 0 {
		add(`p.job_id = ?`, f.JobID)
	}
	if f.SpoolID > 0 {
		add(`p.spool_id = ?`, f.SpoolID)
	}
	if len(f.Status) > 0 {
		in := []any{}
		for _, st := range f.Status {
			in = append(in, st)
		}
		add(`p.status IN (`+strings.TrimSuffix(strings.Repeat("?,", len(in)), ",")+`)`, in...)
	}
	if f.From != "" {
		add(`p.started_at >= ?`, f.From)
	}
	if f.To != "" {
		add(`p.started_at < ?`, f.To)
	}
	if f.Keep {
		add(`p.keep = 1`)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + q + "%"
		add(`(p.title LIKE ? OR p.file_name LIKE ? OR p.notes LIKE ? OR p.fail_note LIKE ?)`, like, like, like, like)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM prints p`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+printCols+printFrom+where+` ORDER BY p.started_at DESC, p.id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Print{}
	for rows.Next() {
		p, err := scanPrint(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}

// GetPrint returns one print.
func (s *Store) GetPrint(ctx context.Context, uid, id int64) (*Print, error) {
	p, err := scanPrint(s.DB.QueryRowContext(ctx, `SELECT `+printCols+printFrom+` WHERE p.user_id = ? AND p.id = ?`, uid, id))
	return p, notFound(err)
}

// OpenPrint is the print on a printer now (printing or paused), if any.
func (s *Store) OpenPrint(ctx context.Context, uid, printerID int64) (*Print, error) {
	p, err := scanPrint(s.DB.QueryRowContext(ctx, `SELECT `+printCols+printFrom+` WHERE p.user_id = ? AND p.printer_id = ? AND p.status IN ('printing','paused') ORDER BY p.started_at DESC LIMIT 1`, uid, printerID))
	return p, notFound(err)
}

// CreatePrint adds a print.
func (s *Store) CreatePrint(ctx context.Context, p *Print) error {
	if p.StartedAt == "" {
		p.StartedAt = Now()
	}
	if p.Copies <= 0 {
		p.Copies = 1
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO prints (user_id, printer_id, job_id, model_id, file_id, spool_id, title, file_name, status, started_at, ended_at, seconds,
		est_seconds, progress, grams, est_grams, copies, fail_reason, fail_note, rating, notes, settings, keep, photo, filament_cost, power_cost, wear_cost, external_id,
		inventory_ref, calendar_ref, source) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.UserID, nullI(p.PrinterID), nullI(p.JobID), nullI(p.ModelID), nullI(p.FileID), nullI(p.SpoolID), p.Title, p.FileName, p.Status, p.StartedAt, p.EndedAt, p.Seconds,
		p.EstSeconds, p.Progress, p.Grams, p.EstGrams, p.Copies, p.FailReason, p.FailNote, p.Rating, p.Notes, jsonText(p.Settings), boolInt(p.Keep), p.Photo,
		p.FilamentCost, p.PowerCost, p.WearCost, p.ExternalID, p.InventoryRef, p.CalendarRef, p.Source)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdatePrint saves a print.
func (s *Store) UpdatePrint(ctx context.Context, p *Print) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE prints SET printer_id=?, job_id=?, model_id=?, file_id=?, spool_id=?, title=?, file_name=?, status=?, started_at=?, ended_at=?, seconds=?,
		est_seconds=?, progress=?, grams=?, est_grams=?, copies=?, fail_reason=?, fail_note=?, rating=?, notes=?, settings=?, keep=?, photo=?, filament_cost=?, power_cost=?, wear_cost=?,
		external_id=?, inventory_ref=?, calendar_ref=?, updated_at=? WHERE user_id = ? AND id = ?`,
		nullI(p.PrinterID), nullI(p.JobID), nullI(p.ModelID), nullI(p.FileID), nullI(p.SpoolID), p.Title, p.FileName, p.Status, p.StartedAt, p.EndedAt, p.Seconds,
		p.EstSeconds, p.Progress, p.Grams, p.EstGrams, p.Copies, p.FailReason, p.FailNote, p.Rating, p.Notes, jsonText(p.Settings), boolInt(p.Keep), p.Photo,
		p.FilamentCost, p.PowerCost, p.WearCost, p.ExternalID, p.InventoryRef, p.CalendarRef, Now(), p.UserID, p.ID)
	return affected(res, err)
}

// DeletePrint removes a print (its grams go back on the spool).
func (s *Store) DeletePrint(ctx context.Context, uid, id int64) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT spool_id, SUM(grams) FROM spool_log WHERE user_id = ? AND print_id = ? AND kind = 'print' GROUP BY spool_id`, uid, id)
	if err != nil {
		return err
	}
	type back struct {
		spool int64
		g     float64
	}
	var backs []back
	for rows.Next() {
		var b back
		if err := rows.Scan(&b.spool, &b.g); err == nil {
			backs = append(backs, b)
		}
	}
	rows.Close()
	res, err := s.DB.ExecContext(ctx, `DELETE FROM prints WHERE user_id = ? AND id = ?`, uid, id)
	if err := affected(res, err); err != nil {
		return err
	}
	for _, b := range backs {
		_, _ = s.DB.ExecContext(ctx, `UPDATE spools SET used_g = MAX(used_g - ?, 0) WHERE user_id = ? AND id = ?`, b.g, uid, b.spool)
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM spool_log WHERE user_id = ? AND print_id = ? AND spool_id = ?`, uid, id, b.spool)
	}
	return nil
}

// ---------- upkeep ----------

const upkeepCols = `u.id, u.user_id, u.printer_id, u.title, u.every_hours, u.every_days, u.last_done_on, u.last_hours, u.notes, u.task_ref, u.told_at, u.created_at, COALESCE(p.name, '')`

func scanUpkeep(r scanner) (*Upkeep, error) {
	var u Upkeep
	if err := r.Scan(&u.ID, &u.UserID, &u.PrinterID, &u.Title, &u.EveryHours, &u.EveryDays, &u.LastDoneOn, &u.LastHours, &u.Notes, &u.TaskRef, &u.ToldAt, &u.CreatedAt, &u.Printer); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUpkeep lists a person's upkeep (one printer's when printerID > 0).
func (s *Store) ListUpkeep(ctx context.Context, uid, printerID int64) ([]Upkeep, error) {
	q := `SELECT ` + upkeepCols + ` FROM upkeep u JOIN printers p ON p.id = u.printer_id WHERE u.user_id = ? AND p.archived = 0`
	args := []any{uid}
	if printerID > 0 {
		q += ` AND u.printer_id = ?`
		args = append(args, printerID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY p.position, u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upkeep{}
	for rows.Next() {
		u, err := scanUpkeep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// GetUpkeep returns one upkeep item.
func (s *Store) GetUpkeep(ctx context.Context, uid, id int64) (*Upkeep, error) {
	u, err := scanUpkeep(s.DB.QueryRowContext(ctx, `SELECT `+upkeepCols+` FROM upkeep u JOIN printers p ON p.id = u.printer_id WHERE u.user_id = ? AND u.id = ?`, uid, id))
	return u, notFound(err)
}

// CreateUpkeep adds an upkeep item.
func (s *Store) CreateUpkeep(ctx context.Context, u *Upkeep) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO upkeep (user_id, printer_id, title, every_hours, every_days, last_done_on, last_hours, notes, task_ref) VALUES (?,?,?,?,?,?,?,?,?)`,
		u.UserID, u.PrinterID, u.Title, u.EveryHours, u.EveryDays, u.LastDoneOn, u.LastHours, u.Notes, u.TaskRef)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

// UpdateUpkeep saves an upkeep item.
func (s *Store) UpdateUpkeep(ctx context.Context, u *Upkeep) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE upkeep SET title=?, every_hours=?, every_days=?, last_done_on=?, last_hours=?, notes=?, task_ref=?, told_at=? WHERE user_id = ? AND id = ?`,
		u.Title, u.EveryHours, u.EveryDays, u.LastDoneOn, u.LastHours, u.Notes, u.TaskRef, u.ToldAt, u.UserID, u.ID)
	return affected(res, err)
}

// DeleteUpkeep removes an upkeep item (its log stays).
func (s *Store) DeleteUpkeep(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM upkeep WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

// UpkeepUsers lists the people who have upkeep (the daily check).
func (s *Store) UpkeepUsers(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT user_id FROM upkeep`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListUpkeepLog is a printer's log book (all printers when printerID = 0), newest first.
func (s *Store) ListUpkeepLog(ctx context.Context, uid, printerID int64, limit int) ([]UpkeepLog, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT l.id, l.printer_id, l.upkeep_id, l.title, l.done_on, l.hours, l.cost, l.note, l.created_at, COALESCE(p.name, '')
		FROM upkeep_log l LEFT JOIN printers p ON p.id = l.printer_id WHERE l.user_id = ?`
	args := []any{uid}
	if printerID > 0 {
		q += ` AND l.printer_id = ?`
		args = append(args, printerID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY l.done_on DESC, l.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UpkeepLog{}
	for rows.Next() {
		var l UpkeepLog
		var uk sql.NullInt64
		if err := rows.Scan(&l.ID, &l.PrinterID, &uk, &l.Title, &l.DoneOn, &l.Hours, &l.Cost, &l.Note, &l.CreatedAt, &l.Printer); err != nil {
			return nil, err
		}
		l.UpkeepID = np(uk)
		out = append(out, l)
	}
	return out, rows.Err()
}

// CreateUpkeepLog writes upkeep or a repair into a printer's log book.
func (s *Store) CreateUpkeepLog(ctx context.Context, uid int64, l *UpkeepLog) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO upkeep_log (user_id, printer_id, upkeep_id, title, done_on, hours, cost, note) VALUES (?,?,?,?,?,?,?,?)`,
		uid, l.PrinterID, nullI(l.UpkeepID), l.Title, l.DoneOn, l.Hours, l.Cost, l.Note)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	return nil
}

// DeleteUpkeepLog removes a log entry.
func (s *Store) DeleteUpkeepLog(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM upkeep_log WHERE user_id = ? AND id = ?`, uid, id)
	return affected(res, err)
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
