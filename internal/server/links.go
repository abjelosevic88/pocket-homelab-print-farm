package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Print Farm as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "printfarm:read", Name: "See your 3D printing", Description: "Read your printers and how they are doing now (printing, time left, done), the queue, your models, filament spools and what is left on them, the prints you made with how they went and what they cost, upkeep and stats: for your journal, search and Pocket AI."},
	{Scope: "printfarm:write", Name: "Add to your 3D printing", Description: "Add a print job to the queue (\"print two of the phone stand in black PETG\"), add a model from a file on the web, write down a print made, a spool bought or a spool weighed. Changes or removes only what that app added; never starts, pauses or stops a printer."},
}

// linkRoute is one request a Pocket Core link token may make.
type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(home|farm|summary|search|stats|printers|filaments|spools|models|jobs|prints|upkeep|day/\d{4}-\d{2}-\d{2})$`), "printfarm:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(printers|filaments|spools|models|jobs|prints)/\d+$`), "printfarm:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(models/\d+/cover|prints/\d+/photo|files/\d+/thumb)$`), "printfarm:read"},
	{"POST", regexp.MustCompile(`^/api/v1/(jobs|prints|spools|models/link)$`), "printfarm:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/(jobs|prints|spools)/\d+$`), "printfarm:write"},
	{"POST", regexp.MustCompile(`^/api/v1/spools/\d+/weigh$`), "printfarm:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "printfarm" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}

// withLinkApp marks a context as acting for another app (tests, and the auth middleware).
func withLinkApp(ctx context.Context, slug string) context.Context {
	return context.WithValue(ctx, linkAppKey, slug)
}
