package server

import (
	"context"
	"html"
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

const notifyTestBody = "This is how Pocket Print Farm tells you a print is done or failed, a spool is running low or a printer needs upkeep."

func htmlEsc(v string) string { return html.EscapeString(v) }

// push sends a phone message through the person's own notification URL.
func (s *Server) push(ctx context.Context, uid int64, st store.Settings, title, body, path string) bool {
	if st.NotifyURL == "" {
		return false
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"printer"}, Link: s.link(path)}); err != nil {
		s.log.Debug("notify failed", "user", uid, "err", err)
		return false
	}
	return true
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := s.settingsOf(ctx, actorID(ctx))
	if st.NotifyURL == "" {
		writeErr(w, http.StatusBadRequest, "add a notification URL first")
		return
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Print Farm", Body: notifyTestBody, Priority: "info", Tags: []string{"printer"}}); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
