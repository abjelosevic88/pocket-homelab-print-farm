package server

import (
	"context"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/slicer"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// The other Pocket apps. None of it is needed: when an app is not there,
// the page greys the button and says what to do.

func linkTo(sib *siblingConn, path string) string {
	if sib == nil || sib.public == "" {
		return ""
	}
	return strings.TrimRight(sib.public, "/") + path
}

func plainErr(a pocketApp, err error) string {
	if isNotConnected(err) {
		return err.Error()
	}
	if ue, ok := err.(*userErr); ok {
		return ue.msg
	}
	return a.name + ": " + err.Error()
}

func (s *Server) appFail(w http.ResponseWriter, a pocketApp, err error) {
	if isNotConnected(err) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if ue, ok := err.(*userErr); ok {
		writeErr(w, http.StatusBadRequest, ue.msg)
		return
	}
	writeErr(w, http.StatusBadGateway, plainErr(a, err))
}

// handleApps lists how the person reaches each app, in words they can act on.
func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := s.clients(ctx, actorID(ctx))
	out := []appState{}
	for _, a := range pocketApps {
		out = append(out, c.status(ctx, a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "core": s.core.Configured()})
}

func (s *Server) sib(w http.ResponseWriter, r *http.Request, a pocketApp, scopes ...string) (*siblingConn, bool) {
	ctx := r.Context()
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, a, scopes...)
	if err != nil {
		s.appFail(w, a, err)
		return nil, false
	}
	return sib, true
}

func (s *Server) sibFor(ctx context.Context, uid int64, a pocketApp, scopes ...string) (*siblingConn, error) {
	return s.clients(ctx, uid).conn(ctx, a, scopes...)
}

func ifs(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

// ---- Pocket Files: models in, copies and photos out ----

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, ok := s.sib(w, r, appFiles, scopeFilesRead)
	if !ok {
		return
	}
	space := firstNonEmpty(r.URL.Query().Get("space"), "home")
	path := firstNonEmpty(r.URL.Query().Get("path"), "/")
	v, err := getJSONFrom(ctx, sib, "/api/v1/fs/list", url.Values{"space": {space}, "path": {path}})
	if err != nil {
		s.appFail(w, appFiles, err)
		return
	}
	out := []map[string]any{}
	for _, e := range rows(v, "entries") {
		name := str(e, "name")
		if strings.HasPrefix(name, ".") {
			continue
		}
		kind := slicer.KindOf(name)
		if e["is_dir"] == true {
			kind = "folder"
		}
		if kind == "" {
			continue
		}
		out = append(out, map[string]any{"name": name, "path": str(e, "path"), "kind": kind, "size": num(e, "size"), "modified": str(e, "modified")})
	}
	spaces := []map[string]any{}
	if sv, err := getJSONFrom(ctx, sib, "/api/v1/spaces", nil); err == nil {
		for _, sp := range rows(sv, "spaces") {
			spaces = append(spaces, map[string]any{"id": str(sp, "id"), "name": str(sp, "name")})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "space": space, "path": path, "spaces": spaces})
}

// handleFromFiles brings model files in from Pocket Files.
func (s *Server) handleFromFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Space    string   `json:"space"`
		Paths    []string `json:"paths"`
		ModelID  int64    `json:"model_id"`
		Together bool     `json:"together"`
	}
	if err := decode(r, &in); err != nil || len(in.Paths) == 0 || len(in.Paths) > 50 {
		writeErr(w, http.StatusBadRequest, "pick one or more files")
		return
	}
	sib, ok := s.sib(w, r, appFiles, scopeFilesRead)
	if !ok {
		return
	}
	space := firstNonEmpty(in.Space, "home")
	dir := filepath.Join(s.cfg.DataDir, "tmp")
	_ = os.MkdirAll(dir, 0o755)
	type added struct {
		Model *store.Model     `json:"model"`
		File  *store.ModelFile `json:"file"`
		New   bool             `json:"new"`
	}
	out := []added{}
	var errs []string
	modelID := in.ModelID
	for _, p := range in.Paths {
		name := filepath.Base(p)
		if slicer.KindOf(name) == "" {
			continue
		}
		res, err := sib.client.Open(ctx, "/api/v1/fs/raw", url.Values{"space": {space}, "path": {p}}, "")
		if err != nil {
			errs = append(errs, name+": "+plainErr(appFiles, err))
			continue
		}
		tf, err := os.CreateTemp(dir, "files-*")
		if err != nil {
			res.Body.Close()
			s.fail(w, err, "files")
			return
		}
		_, err = io.Copy(tf, io.LimitReader(res.Body, s.cfg.MaxUploadBytes+1))
		res.Body.Close()
		tf.Close()
		if err != nil {
			_ = os.Remove(tf.Name())
			errs = append(errs, name+": could not be read")
			continue
		}
		m, f, isNew, err := s.addFile(ctx, uid, modelID, name, tf.Name(), "files", space+":"+p)
		_ = os.Remove(tf.Name())
		if err != nil {
			errs = append(errs, plainUserErr(err))
			continue
		}
		if in.Together && modelID == 0 {
			modelID = m.ID
		}
		out = append(out, added{m, f, isNew})
	}
	if len(out) == 0 {
		writeErr(w, http.StatusBadRequest, firstNonEmpty(strings.Join(errs, "; "), "none of those are model files"))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"added": out, "errors": errs, "text": fmt.Sprintf("%d %s from your files", len(out), plural(len(out), "file", "files"))})
}

// putInFiles keeps bytes in the person's Print Farm folder in Pocket Files.
func (s *Server) putInFiles(ctx context.Context, uid int64, sub, name string, data []byte, ctype string) (string, error) {
	sib, err := s.sibFor(ctx, uid, appFiles, scopeFilesWrite)
	if err != nil {
		return "", err
	}
	dir := strings.Trim(firstNonEmpty(s.settingsOf(ctx, uid).FilesFolder, "3D prints"), "/")
	if sub != "" {
		dir += "/" + sub
	}
	var res map[string]any
	q := url.Values{"space": {"home"}, "path": {"/"}, "name": {dir + "/" + fileSafe(name)}, "conflict": {"rename"}, "mtime": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	if err := sib.client.PutRaw(ctx, "/api/v1/fs/upload", q, data, ctype, &res); err != nil {
		return "", err
	}
	p := firstNonEmpty(str(res, "path"), "/"+dir+"/"+name)
	folder := p
	if i := strings.LastIndex(p, "/"); i > 0 {
		folder = p[:i]
	}
	return linkTo(sib, "/f/home"+folder), nil
}

func fileSafe(v string) string {
	v = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, v)
	return clip(strings.TrimSpace(v), 150)
}

// handleFileToFiles keeps a copy of a model file in Pocket Files.
func (s *Server) handleFileToFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, ok := s.fileOf(w, r)
	if !ok {
		return
	}
	if f.Size > 512<<20 {
		writeErr(w, http.StatusBadRequest, "that file is too big to copy this way")
		return
	}
	data, err := os.ReadFile(s.mediaPath(f.Path))
	if err != nil {
		writeErr(w, http.StatusNotFound, "the file is gone")
		return
	}
	sub := ""
	if m, err := s.store.GetModel(ctx, f.UserID, f.ModelID); err == nil {
		sub = fileSafe(m.Name)
	}
	link, err := s.putInFiles(ctx, f.UserID, sub, f.Name, data, mimeByExt(f.Name))
	if err != nil {
		s.appFail(w, appFiles, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": link, "text": "Copied to Pocket Files"})
}

// handlePrintPhotoToFiles keeps a print's photo in Pocket Files.
func (s *Server) handlePrintPhotoToFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	if p.Photo == "" {
		writeErr(w, http.StatusBadRequest, "this print has no photo yet")
		return
	}
	data, err := os.ReadFile(s.mediaPath(p.Photo))
	if err != nil {
		writeErr(w, http.StatusNotFound, "the photo is gone")
		return
	}
	name := fmt.Sprintf("%s - %s%s", localDay(p.StartedAt), p.Title, filepath.Ext(p.Photo))
	link, err := s.putInFiles(ctx, p.UserID, "Photos", name, data, mimeByExt(p.Photo))
	if err != nil {
		s.appFail(w, appFiles, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": link, "text": "The photo is in Pocket Files"})
}

// ---- Pocket Inventory: finished prints on the shelf ----

func (s *Server) handleInventoryLocations(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appInventory, scopeItemsRead)
	if !ok {
		return
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/locations", nil)
	if err != nil {
		s.appFail(w, appInventory, err)
		return
	}
	out := []map[string]any{}
	for _, l := range rows(v, "locations") {
		out = append(out, map[string]any{"id": num(l, "id"), "name": str(l, "name"), "count": num(l, "item_count")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": out})
}

// handlePrintToInventory puts a finished print's pieces on the inventory:
// added to the item made from the same model before, else a new item.
func (s *Server) handlePrintToInventory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	if p.Status != "done" {
		writeErr(w, http.StatusBadRequest, "only a finished print goes on the inventory")
		return
	}
	var in struct {
		Location string `json:"location"`
		Quantity int    `json:"quantity"`
		Name     string `json:"name"`
	}
	_ = decode(r, &in)
	sib, ok := s.sib(w, r, appInventory, scopeItemsRead, scopeItemsWrite, scopeItemsAdjust)
	if !ok {
		return
	}
	st := s.settingsOf(ctx, p.UserID)
	qty := firstPos(in.Quantity, p.Copies, 1)
	name := firstNonEmpty(cleanText(in.Name, maxName), p.Title)
	modelURL := ""
	if p.ModelID != nil {
		modelURL = s.link(fmt.Sprintf("/models/%d", *p.ModelID))
	}
	var itemID string
	if modelURL != "" {
		if v, err := getJSONFrom(ctx, sib, "/api/v1/items", url.Values{"q": {name}, "limit": {"20"}}); err == nil {
			for _, it := range rows(v, "items") {
				if str(it, "url") == modelURL {
					itemID = idOf(it)
					break
				}
			}
		}
	}
	var res map[string]any
	var err error
	if itemID != "" {
		err = sib.client.Do(ctx, http.MethodPost, "/api/v1/items/"+url.PathEscape(itemID)+"/adjust", nil, map[string]any{"delta": qty, "note": "Printed: " + p.Title}, &res)
	} else {
		body := map[string]any{"name": name, "quantity": qty, "tags": "3d printed", "url": modelURL, "note": "Printed in Pocket Print Farm",
			"notes": strings.TrimSpace(fmt.Sprintf("Printed %s%s.", localDay(p.StartedAt), ifs(p.Material != "", " in "+p.Material, "")))}
		if loc := firstNonEmpty(strings.TrimSpace(in.Location), st.InventoryLocation); loc != "" {
			body["location"] = loc
		}
		if p.Cost > 0 && qty > 0 {
			body["price"] = math.Round(float64(p.Cost)/float64(qty)) / 100
		}
		err = sib.client.Do(ctx, http.MethodPost, "/api/v1/items", nil, body, &res)
		itemID = idOf(res)
	}
	if err != nil {
		s.appFail(w, appInventory, err)
		return
	}
	link := linkTo(sib, "/items/"+itemID)
	p.InventoryRef = link
	_ = s.store.UpdatePrint(ctx, p)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": link, "text": fmt.Sprintf("%d on your inventory", qty)})
}

// ---- Pocket Money: a spool bought ----

func (s *Server) handleMoneyAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, ok := s.sib(w, r, appMoney, scopeAccountsRead)
	if !ok {
		return
	}
	v, err := getJSONFrom(ctx, sib, "/api/v1/accounts", nil)
	if err != nil {
		s.appFail(w, appMoney, err)
		return
	}
	accts := []map[string]any{}
	for _, a := range rows(v, "accounts") {
		if a["archived"] == true || str(a, "role") == "viewer" {
			continue
		}
		accts = append(accts, map[string]any{"id": idOf(a), "name": str(a, "name"), "currency": str(a, "currency")})
	}
	cats := []map[string]any{}
	if cv, err := getJSONFrom(ctx, sib, "/api/v1/categories", nil); err == nil {
		for _, c := range rows(cv, "categories") {
			if c["archived"] == true || (str(c, "kind") != "" && str(c, "kind") != "expense") {
				continue
			}
			cats = append(cats, map[string]any{"id": idOf(c), "name": firstNonEmpty(str(c, "full_name"), str(c, "name"))})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accts, "categories": cats})
}

// spoolExpense writes a spool bought (n of them) into Pocket Money.
func (s *Server) spoolExpense(ctx context.Context, uid int64, x *store.Spool, n int) (string, error) {
	sib, err := s.sibFor(ctx, uid, appMoney, scopeAccountsRead, scopeTxWrite)
	if err != nil {
		return "", err
	}
	st := s.settingsOf(ctx, uid)
	acct := st.MoneyAccount
	if acct == "" {
		v, err := getJSONFrom(ctx, sib, "/api/v1/accounts", nil)
		if err != nil {
			return "", err
		}
		for _, a := range rows(v, "accounts") {
			if a["archived"] != true && str(a, "role") != "viewer" {
				acct = idOf(a)
				break
			}
		}
	}
	if acct == "" {
		return "", &userErr{"there is no account in Pocket Money to write it in"}
	}
	price := firstPos(x.Price, x.FilamentPrice) * max(1, n)
	if price <= 0 {
		return "", &userErr{"the spool has no price"}
	}
	aid, _ := strconv.ParseInt(acct, 10, 64)
	desc := x.Label()
	if n > 1 {
		desc = fmt.Sprintf("%d × %s", n, desc)
	}
	body := map[string]any{"kind": "expense", "account_id": aid, "amount": price, "currency": st.Currency, "date": firstNonEmpty(x.BoughtOn, store.Today()),
		"description": "Filament: " + desc, "payee": x.Brand, "tags": []string{"3d printing"}, "external_id": fmt.Sprintf("spool-%d", x.ID)}
	if c := s.moneyCategory(ctx, sib, st.MoneyCategory); c > 0 {
		body["category_id"] = c
	}
	res, err := postJSONTo(ctx, sib, "/api/v1/transactions", body)
	if err != nil {
		return "", err
	}
	return linkTo(sib, "/transactions/"+idOf(res)), nil
}

// moneyCategory finds the category by name (or its id typed in settings).
func (s *Server) moneyCategory(ctx context.Context, sib *siblingConn, want string) int64 {
	want = strings.TrimSpace(want)
	if want == "" {
		return 0
	}
	if n, err := strconv.ParseInt(want, 10, 64); err == nil {
		return n
	}
	v, err := getJSONFrom(ctx, sib, "/api/v1/categories", nil)
	if err != nil {
		return 0
	}
	for _, c := range rows(v, "categories") {
		if strings.EqualFold(str(c, "name"), want) || strings.EqualFold(str(c, "full_name"), want) {
			n, _ := strconv.ParseInt(idOf(c), 10, 64)
			return n
		}
	}
	return 0
}

func (s *Server) handleSpoolToMoney(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, ok := s.spoolOf(w, r)
	if !ok {
		return
	}
	link, err := s.spoolExpense(ctx, x.UserID, x, 1)
	if err != nil {
		s.appFail(w, appMoney, err)
		return
	}
	x.MoneyRef = link
	_ = s.store.UpdateSpool(ctx, x)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": link, "text": "Written down in Pocket Money"})
}

// ---- Pocket Invoicing: prints sold ----

func (s *Server) handleInvoiceClients(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appInvoicing, scopeInvoicesWrite)
	if !ok {
		return
	}
	q := url.Values{}
	if t := strings.TrimSpace(r.URL.Query().Get("q")); t != "" {
		q.Set("q", t)
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/clients", q)
	if err != nil {
		s.appFail(w, appInvoicing, err)
		return
	}
	out := []map[string]any{}
	for _, c := range rows(v, "clients") {
		if c["archived"] == true {
			continue
		}
		out = append(out, map[string]any{"id": idOf(c), "name": str(c, "name"), "currency": str(c, "currency")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": out})
}

// handleJobInvoice makes a draft invoice for a job: its quantity at its price.
func (s *Server) handleJobInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, ok := s.jobOf(w, r)
	if !ok {
		return
	}
	var in struct {
		ClientID int64 `json:"client_id"`
		Price    int   `json:"price"`
	}
	if err := decode(r, &in); err != nil || in.ClientID <= 0 {
		writeErr(w, http.StatusBadRequest, "choose who the invoice is for")
		return
	}
	price := firstPos(in.Price, j.Price)
	if price <= 0 {
		writeErr(w, http.StatusBadRequest, "give the job a price first")
		return
	}
	sib, ok := s.sib(w, r, appInvoicing, scopeInvoicesWrite)
	if !ok {
		return
	}
	qty := firstPos(j.DoneCount, j.Quantity)
	desc := j.Title + ifs(j.Material != "", " (3D printed, "+j.Material+")", " (3D printed)")
	st := s.settingsOf(ctx, j.UserID)
	body := map[string]any{"client_id": in.ClientID, "currency": st.Currency, "notes": "Print job: " + j.Title,
		"items": []map[string]any{{"description": desc, "unit": "piece", "quantity": qty, "unit_price": float64(price) / 100}}}
	res, err := postJSONTo(ctx, sib, "/api/v1/invoices", body)
	if err != nil {
		s.appFail(w, appInvoicing, err)
		return
	}
	link := linkTo(sib, "/invoices/"+idOf(res))
	j.InvoiceRef = link
	if in.Price > 0 {
		j.Price = in.Price
	}
	_ = s.store.UpdateJob(ctx, j)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": link, "text": "A draft invoice is in Pocket Invoicing"})
}

// ---- Pocket Contacts ----

func (s *Server) handleContacts(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appContacts, scopeContactsRead)
	if !ok {
		return
	}
	q := url.Values{"limit": {"40"}}
	if t := strings.TrimSpace(r.URL.Query().Get("q")); t != "" {
		q.Set("q", t)
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/contacts", q)
	if err != nil {
		s.appFail(w, appContacts, err)
		return
	}
	out := []map[string]any{}
	for _, m := range rows(v, "contacts") {
		name := firstNonEmpty(str(m, "fn"), str(m, "name"), joinNonEmpty(" ", str(m, "given"), str(m, "family")))
		if name == "" {
			continue
		}
		out = append(out, map[string]any{"id": idOf(m), "name": name, "org": str(m, "org")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": out, "url": linkTo(sib, "/")})
}

// ---- Pocket Tasks ----

func (s *Server) handleTaskLists(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appTasks, scopeTasksRead)
	if !ok {
		return
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/lists", nil)
	if err != nil {
		s.appFail(w, appTasks, err)
		return
	}
	out := []map[string]any{}
	for _, l := range rows(v, "lists") {
		if l["archived"] == true {
			continue
		}
		out = append(out, map[string]any{"id": idOf(l), "name": str(l, "name"), "inbox": l["is_inbox"] == true})
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": out})
}

func (s *Server) newTask(ctx context.Context, uid int64, title, notes, due, link string) (string, *siblingConn, error) {
	sib, err := s.sibFor(ctx, uid, appTasks, scopeTasksWrite)
	if err != nil {
		return "", nil, err
	}
	st := s.settingsOf(ctx, uid)
	body := map[string]any{"title": clip(title, 200), "notes": clip(notes, 4000), "url": link, "tags": []string{"3d printing"}}
	if due != "" {
		body["due"] = due
	}
	if n, err := strconv.ParseInt(st.TasksList, 10, 64); err == nil && n > 0 {
		body["list_id"] = n
	}
	res, err := postJSONTo(ctx, sib, "/api/v1/tasks", body)
	if err != nil {
		return "", nil, err
	}
	return idOf(res), sib, nil
}

// upkeepTask makes a task for upkeep that is due; it returns the task's id.
func (s *Server) upkeepTask(ctx context.Context, uid int64, u *store.Upkeep) (string, error) {
	notes := "Due on " + u.Printer
	if u.DueHours != nil && *u.DueHours <= 0 {
		notes += fmt.Sprintf(" (%.0f print hours since last time)", u.EveryHours-*u.DueHours)
	}
	if u.Notes != "" {
		notes += "\n\n" + u.Notes
	}
	id, _, err := s.newTask(ctx, uid, u.Printer+": "+u.Title, notes, store.Today(), s.link(fmt.Sprintf("/printers/%d", u.PrinterID)))
	return id, err
}

// completeTask ticks off the task made for upkeep, now done.
func (s *Server) completeTask(ctx context.Context, uid int64, id string) {
	sib, err := s.sibFor(ctx, uid, appTasks, scopeTasksWrite)
	if err != nil {
		return
	}
	var res map[string]any
	_ = sib.client.Do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/complete", nil, map[string]any{}, &res)
}

func (s *Server) handleJobTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, ok := s.jobOf(w, r)
	if !ok {
		return
	}
	notes := fmt.Sprintf("%d to print", j.Quantity)
	if j.ForWhom != "" {
		notes += " for " + j.ForWhom
	}
	if j.Notes != "" {
		notes += "\n\n" + j.Notes
	}
	id, sib, err := s.newTask(ctx, j.UserID, "Print: "+j.Title, notes, j.DueOn, s.link("/queue"))
	if err != nil {
		s.appFail(w, appTasks, err)
		return
	}
	j.TaskRef = id
	_ = s.store.UpdateJob(ctx, j)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": linkTo(sib, "/?task="+url.QueryEscape(id)), "text": "A task is in Pocket Tasks"})
}

// ---- Pocket Calendar ----

// printToCalendar puts the time a long print will be done in the calendar.
func (s *Server) printToCalendar(ctx context.Context, uid int64, pr *store.Print, p *store.Printer) (string, error) {
	sib, err := s.sibFor(ctx, uid, appCalendar, scopeCalendarWrite)
	if err != nil {
		return "", err
	}
	if pr.EstSeconds <= 0 {
		return "", &userErr{"how long it takes is not known"}
	}
	end := parseTime(pr.StartedAt).Add(time.Duration(pr.EstSeconds) * time.Second)
	title := "🖨️ " + pr.Title + " done"
	if p != nil {
		title += " on " + p.Name
	}
	body := map[string]any{"title": clip(title, 200), "description": fmt.Sprintf("Started at %s, about %s.", localClock(pr.StartedAt), hm(pr.EstSeconds)),
		"url": s.link(fmt.Sprintf("/prints/%d", pr.ID)), "start": end.Add(-15 * time.Minute).Format(time.RFC3339), "end": end.Format(time.RFC3339),
		"categories": []string{"3D printing"}, "busy": false}
	res, err := postJSONTo(ctx, sib, "/api/v1/events", body)
	if err != nil {
		return "", err
	}
	return linkTo(sib, "/?event="+url.QueryEscape(firstNonEmpty(str(res, "event_id"), idOf(res)))), nil
}

func (s *Server) handlePrintCalendar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pr, ok := s.printOf(w, r)
	if !ok {
		return
	}
	var p *store.Printer
	if pr.PrinterID != nil {
		p, _ = s.store.GetPrinter(ctx, pr.UserID, *pr.PrinterID)
	}
	link, err := s.printToCalendar(ctx, pr.UserID, pr, p)
	if err != nil {
		s.appFail(w, appCalendar, err)
		return
	}
	pr.CalendarRef = link
	_ = s.store.UpdatePrint(ctx, pr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": link, "text": "In Pocket Calendar"})
}

// ---- Pocket Chat ----

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appChat, scopeChatRead)
	if !ok {
		return
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/conversations", nil)
	if err != nil {
		s.appFail(w, appChat, err)
		return
	}
	out := []map[string]any{}
	for _, m := range rows(v, "conversations") {
		if m["archived"] == true {
			continue
		}
		out = append(out, map[string]any{"id": idOf(m), "name": firstNonEmpty(str(m, "name"), str(m, "title")), "kind": str(m, "kind"), "emoji": str(m, "emoji")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

// handlePrintChat shares a print (with its photo) to a chat.
func (s *Server) handlePrintChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pr, ok := s.printOf(w, r)
	if !ok {
		return
	}
	var in struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	_ = decode(r, &in)
	sib, ok := s.sib(w, r, appChat, scopeChatRead, scopeChatWrite)
	if !ok {
		return
	}
	st := s.settingsOf(ctx, pr.UserID)
	to := firstNonEmpty(strings.TrimSpace(in.To), st.ChatTo, "me")
	lines := []string{}
	if pr.Printer != "" {
		lines = append(lines, "🖨️ "+pr.Printer)
	}
	if pr.Seconds > 0 {
		lines = append(lines, "⏱️ "+hm(pr.Seconds))
	}
	if pr.Grams > 0 {
		lines = append(lines, strings.TrimSpace(fmt.Sprintf("🧵 %.0f g %s", pr.Grams, pr.Material)))
	}
	if pr.Rating > 0 {
		lines = append(lines, strings.Repeat("★", pr.Rating)+strings.Repeat("☆", 5-pr.Rating))
	}
	icon, sub := "✅", "Printed"
	if pr.Status == "failed" {
		icon, sub = "❌", fmt.Sprintf("Failed at %d%%", int(pr.Progress*100))
	}
	text := firstNonEmpty(strings.TrimSpace(in.Text), icon+" "+pr.Title)
	body := map[string]any{"to": to, "text": clip(text, 2000), "client_id": fmt.Sprintf("printfarm-%d-%d-%d", pr.UserID, pr.ID, time.Now().Unix()/30),
		"card": map[string]any{"title": pr.Title, "subtitle": sub, "lines": lines, "icon": icon, "url": s.link(fmt.Sprintf("/prints/%d", pr.ID)), "app": "printfarm", "kind": "card"}}
	// the photo goes along when the chat is known by its id
	if pr.Photo != "" {
		if cid := s.chatID(ctx, sib, to); cid != "" {
			if att, err := s.chatUpload(ctx, sib, cid, pr); err == nil && att != "" {
				if n, err := strconv.ParseInt(att, 10, 64); err == nil {
					body["attachments"] = []int64{n}
					body["to"] = cid
				}
			}
		}
	}
	res, err := postJSONTo(ctx, sib, "/api/v1/send", body)
	if err != nil {
		s.appFail(w, appChat, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": linkTo(sib, "/c/"+str(res, "conversation_id")), "text": "Shared in Pocket Chat"})
}

// chatID is a conversation's id from what the person chose ("me" = their own notes).
func (s *Server) chatID(ctx context.Context, sib *siblingConn, to string) string {
	if _, err := strconv.ParseInt(to, 10, 64); err == nil {
		return to
	}
	v, err := getJSONFrom(ctx, sib, "/api/v1/conversations", nil)
	if err != nil {
		return ""
	}
	for _, c := range rows(v, "conversations") {
		if (to == "me" || to == "") && str(c, "kind") == "self" {
			return idOf(c)
		}
		if strings.EqualFold(str(c, "name"), to) {
			return idOf(c)
		}
	}
	return ""
}

func (s *Server) chatUpload(ctx context.Context, sib *siblingConn, cid string, pr *store.Print) (string, error) {
	data, err := os.ReadFile(s.mediaPath(pr.Photo))
	if err != nil {
		return "", err
	}
	rd, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		fw, err := mw.CreateFormFile("file", "print"+filepath.Ext(pr.Photo))
		if err == nil {
			_, err = fw.Write(data)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	var res map[string]any
	if err := sib.client.PostRaw(ctx, "/api/v1/conversations/"+url.PathEscape(cid)+"/upload", nil, rd, mw.FormDataContentType(), &res); err != nil {
		return "", err
	}
	return idOf(res), nil
}

// ---- Pocket Parcels: filament on the way ----

var filamentWords = []string{"filament", "pla", "petg", "abs", "asa", "tpu", "spool", "prusament", "polymaker", "esun", "sunlu", "elegoo", "bambu", "creality", "resin", "3d"}

func looksLikeFilament(t string) bool {
	t = " " + strings.ToLower(t) + " "
	for _, w := range filamentWords {
		if strings.Contains(t, " "+w+" ") || (len(w) > 4 && strings.Contains(t, w)) {
			return true
		}
	}
	return false
}

func (s *Server) handleParcels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, ok := s.sib(w, r, appParcels, scopeParcelsRead)
	if !ok {
		return
	}
	v, err := getJSONFrom(ctx, sib, "/api/v1/parcels", url.Values{"view": {"active"}, "limit": {"200"}})
	if err != nil {
		s.appFail(w, appParcels, err)
		return
	}
	all := queryBool(r, "all")
	out := []map[string]any{}
	for _, p := range rows(v, "parcels") {
		if !all && !looksLikeFilament(str(p, "title")+" "+str(p, "shop")) {
			continue
		}
		out = append(out, map[string]any{"id": idOf(p), "title": str(p, "title"), "shop": str(p, "shop"), "status": str(p, "status_label"), "expected_on": str(p, "expected_on"),
			"arrives_in": p["arrives_in"], "price": p["price"], "currency": str(p, "currency"), "url": firstNonEmpty(str(p, "url"), linkTo(sib, "/parcels/"+idOf(p)))})
	}
	writeJSON(w, http.StatusOK, map[string]any{"parcels": out})
}

// ---- Pocket Projects ----

func (s *Server) handlePrintIssue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pr, ok := s.printOf(w, r)
	if !ok {
		return
	}
	var in struct {
		Title string `json:"title"`
		Team  string `json:"team"`
	}
	_ = decode(r, &in)
	sib, ok := s.sib(w, r, appProjects, scopeProjectsWrite)
	if !ok {
		return
	}
	title := firstNonEmpty(cleanText(in.Title, 200), ifs(pr.Status == "failed", "Print failed: "+pr.Title, "Printed: "+pr.Title))
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s", map[string]string{"done": "Printed", "failed": "Failed", "cancelled": "Stopped", "printing": "Printing", "paused": "Paused"}[pr.Status], firstNonEmpty(pr.Printer, "a printer"))
	if pr.Status != "done" {
		fmt.Fprintf(&b, " at %d%%", int(pr.Progress*100))
	}
	if pr.FailReason != "" {
		fmt.Fprintf(&b, " — %s", strings.ReplaceAll(pr.FailReason, "_", " "))
	}
	if pr.FailNote != "" {
		b.WriteString(": " + pr.FailNote)
	}
	b.WriteString(".\n\n")
	if pr.Spool != "" {
		fmt.Fprintf(&b, "- Filament: %s\n", pr.Spool)
	}
	if pr.Seconds > 0 {
		fmt.Fprintf(&b, "- Time: %s\n", hm(pr.Seconds))
	}
	if pr.Notes != "" {
		b.WriteString("\n" + pr.Notes + "\n")
	}
	link := s.link(fmt.Sprintf("/prints/%d", pr.ID))
	body := map[string]any{"title": title, "description": b.String(), "source_ref": fmt.Sprintf("printfarm:print:%d", pr.ID), "source_url": link, "labels": []string{"3d printing"}}
	if t := strings.TrimSpace(in.Team); t != "" {
		body["team"] = t
	}
	var res map[string]any
	if err := sib.client.Do(ctx, http.MethodPost, "/api/v1/issues", nil, body, &res); err != nil {
		s.appFail(w, appProjects, err)
		return
	}
	issue := obj(res, "issue")
	if len(issue) == 0 {
		issue = res
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": firstNonEmpty(str(issue, "url"), linkTo(sib, "/issue/"+firstNonEmpty(str(issue, "identifier"), idOf(issue)))), "text": "An issue is in Pocket Projects"})
}

// ---- Pocket Home: the printer as an appliance ----

func (s *Server) handleHomeThings(w http.ResponseWriter, r *http.Request) {
	sib, ok := s.sib(w, r, appHome, scopeHomeRead)
	if !ok {
		return
	}
	q := url.Values{}
	if t := strings.TrimSpace(r.URL.Query().Get("q")); t != "" {
		q.Set("q", t)
	}
	v, err := getJSONFrom(r.Context(), sib, "/api/v1/things", q)
	if err != nil {
		s.appFail(w, appHome, err)
		return
	}
	out := []map[string]any{}
	for _, t := range rows(v, "things") {
		out = append(out, map[string]any{"id": idOf(t), "home_id": num(t, "home_id"), "name": str(t, "name"), "room": str(t, "room"), "brand": str(t, "brand"), "model": str(t, "model")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"things": out, "url": linkTo(sib, "/")})
}

// upkeepToHome writes upkeep done into the appliance's log in Pocket Home.
func (s *Server) upkeepToHome(ctx context.Context, uid int64, p *store.Printer, l *store.UpkeepLog) (string, error) {
	sib, err := s.sibFor(ctx, uid, appHome, scopeHomeRead, scopeHomeWrite)
	if err != nil {
		return "", err
	}
	thing, err := getJSONFrom(ctx, sib, "/api/v1/things/"+url.PathEscape(p.HomeRef), nil)
	if err != nil {
		return "", err
	}
	tm, _ := thing.(map[string]any)
	if t := obj(tm, "thing"); len(t) > 0 {
		tm = t
	}
	tid, _ := strconv.ParseInt(p.HomeRef, 10, 64)
	body := map[string]any{"thing_id": tid, "home_id": int64(num(tm, "home_id")), "date": l.DoneOn, "kind": "maintenance", "title": l.Title,
		"notes": strings.TrimSpace(fmt.Sprintf("%s\nAt %.0f print hours (Pocket Print Farm).", l.Note, l.Hours)), "external_id": fmt.Sprintf("upkeep-%d", l.ID)}
	if l.Cost > 0 {
		body["cost"], body["currency"] = l.Cost, s.settingsOf(ctx, uid).Currency
	}
	if _, err := postJSONTo(ctx, sib, "/api/v1/records", body); err != nil {
		return "", err
	}
	return linkTo(sib, "/things/"+url.PathEscape(p.HomeRef)), nil
}

// ---- Pocket Alerts ----

func (s *Server) alert(ctx context.Context, uid int64, title, body, path, priority string) bool {
	sib, err := s.sibFor(ctx, uid, appAlerts, scopeAlertsWrite)
	if err != nil {
		return false
	}
	_, err = postJSONTo(ctx, sib, "/api/v1/alerts", map[string]any{"title": clip(title, 200), "body": clip(body, 2000), "priority": priority, "tags": []string{"printer"}, "click": s.link(path)})
	if err != nil {
		s.log.Debug("pocket alerts: send failed", "err", err)
		return false
	}
	return true
}
