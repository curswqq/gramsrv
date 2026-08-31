package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Panel session authorisation and CSRF.
//
// The panel authenticates with a cookie, which is what makes it a CSRF target:
// a request forged by any other origin arrives with the operator's session
// attached. Two independent checks close that.
//
// 1. Double-submit token. At login the server mints a random token, publishes it
//    in a readable cookie (telesrv_admin_csrf) and requires the same value in the
//    X-CSRF-Token header of every mutating request. A cross-origin page can make
//    the browser *send* the cookie but cannot read it, so it cannot produce the
//    header. Double-submit is the right shape here specifically because this
//    process keeps no server-side session store: the session lives entirely in a
//    signed cookie, so there is nowhere to park a per-session token, and the
//    stateless variant is the one that survives a restart and a second replica.
//    The token is additionally bound into the signed session claims, so a
//    cookie-writing neighbour (a sibling subdomain) cannot supply a matching
//    cookie/header pair of its own choosing either.
//
// 2. Origin agreement. When the browser states an Origin, it must be this host.
//    That catches a forged request from a page that somehow does hold a token.
//
// Both comparisons are constant time, for the same reason the session MAC is.

// Panel permission names. They match the strings an operator configures in
// TELESRV_ADMIN_UI_PERMISSIONS and the ones the admin API enforces.
const (
	permissionAll                = "*"
	permissionPremiumManage      = "premium.manage"
	permissionBotTokenRead       = "bots.token.read"
	permissionVerificationReview = "verification.review"
	permissionVerificationRevoke = "verification.revoke"
	// Third-party bot verification. Deliberately not implied by the official
	// verification rights above: the two are separate mechanisms over separate
	// tables, so a session trusted with one queue is not thereby trusted with the
	// other. review reads and decides applications; manage appoints verifiers,
	// curates the icon catalogue and strips granted marks.
	permissionBotVerificationReview = "botverification.review"
	permissionBotVerificationManage = "botverification.manage"
	// Managing panel administrator accounts: creating accounts, disabling them,
	// rotating somebody else's password, revoking somebody else's session.
	permissionAdminsManage = "admins.manage"
)

type permissionsKey struct{}

type authKey struct{}

func authFromContext(ctx context.Context) *panelAuth {
	if auth, ok := ctx.Value(authKey{}).(*panelAuth); ok {
		return auth
	}
	return &panelAuth{}
}

// requireAuthAPI is the gate on every authenticated API route: a valid session,
// and -- for a mutating request -- a valid CSRF token.
func (s *server) requireAuthAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeAPIError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		auth := s.resolveSession(r, cookie.Value)
		if auth == nil {
			clearSessionCookie(w)
			writeAPIError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		if !checkMutationSafety(w, r, *auth) {
			return
		}
		ctx := context.WithValue(r.Context(), actorKey{}, auth.username)
		ctx = context.WithValue(ctx, authKey{}, auth)
		ctx = context.WithValue(ctx, permissionsKey{}, auth.permissions)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveSession prefers a server-side session row (account logins write an
// opaque id). A signed legacy cookie -- the single-secret sessions issued
// before accounts existed, or by the fallback path while no accounts are
// seeded yet -- is still honoured, so the upgrade never logs an operator out.
// A dotted value can only be a legacy cookie: opaque ids are plain hex.
func (s *server) resolveSession(r *http.Request, value string) *panelAuth {
	if s.pool != nil && !strings.Contains(value, ".") {
		auth, err := s.lookupLiveSession(r.Context(), value)
		if err != nil {
			return nil
		}
		return auth
	}
	claims, ok := verifySession(s.cfg.SessionKey, value, time.Now())
	if !ok {
		return nil
	}
	return &panelAuth{
		username:    claims.Actor,
		permissions: newPanelPermissions(claims.Permissions),
		csrf:        claims.CSRF,
	}
}

// requirePermission refuses a session that was not granted the right, before the
// request ever reaches the admin API. The panel is the only caller that can be
// driven by a browser, so the check belongs here as well as upstream: a 403 from
// this process costs no round trip and cannot be confused with a domain failure.
func (s *server) requirePermission(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !permissionsFromContext(r.Context()).Has(permission) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":      "permission " + permission + " is required",
				"code":       "FORBIDDEN",
				"permission": permission,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkMutationSafety enforces the CSRF contract on a mutating request. The
// token is bound to the session -- inside the signed claims for a legacy
// cookie, in the admin_sessions row for an account login.
func checkMutationSafety(w http.ResponseWriter, r *http.Request, auth panelAuth) bool {
	if !mutatingMethod(r.Method) {
		return true
	}
	if !sameOriginRequest(r) {
		writeAPIError(w, http.StatusForbidden, "origin is not allowed")
		return false
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		writeAPIError(w, http.StatusForbidden, "missing "+csrfCookieName+" cookie; sign in again")
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if header == "" {
		writeAPIError(w, http.StatusForbidden, "missing "+csrfHeaderName+" header")
		return false
	}
	if subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		writeAPIError(w, http.StatusForbidden, csrfHeaderName+" does not match the "+csrfCookieName+" cookie")
		return false
	}
	// The session is the third leg: it pins the pair to the session this server
	// issued. A session minted before the token existed carries no CSRF claim and
	// is refused, which forces one re-login rather than leaving a half-protected
	// session running.
	if auth.csrf == "" || subtle.ConstantTimeCompare([]byte(header), []byte(auth.csrf)) != 1 {
		writeAPIError(w, http.StatusForbidden, "csrf token is not bound to this session; sign in again")
		return false
	}
	return true
}

// mutatingMethod reports whether the method changes state. GET/HEAD/OPTIONS are
// the safe ones; everything else has to carry a token.
func mutatingMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// sameOriginRequest checks the Origin header against the request host.
//
// An absent Origin is accepted: browsers omit it on same-origin requests and
// non-browser callers (curl, tests) never send it, so requiring it would break
// the panel without adding protection the token does not already give. A present
// Origin must be this host -- including the literal "null" a sandboxed or
// privacy-stripped context sends, which is by definition not this host.
//
// This compares against r.Host, so a reverse proxy in front of the panel has to
// preserve it (nginx: proxy_set_header Host $host).
func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// panelPermissions is a resolved session permission set.
type panelPermissions struct {
	all   bool
	names map[string]struct{}
	list  []string
}

func newPanelPermissions(permissions []string) panelPermissions {
	set := panelPermissions{names: make(map[string]struct{}, len(permissions))}
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		if _, dup := set.names[permission]; dup {
			continue
		}
		if permission == permissionAll {
			set.all = true
		}
		set.names[permission] = struct{}{}
		set.list = append(set.list, permission)
	}
	return set
}

// Has reports whether the session was granted the permission.
func (p panelPermissions) Has(permission string) bool {
	if p.all {
		return true
	}
	_, ok := p.names[permission]
	return ok
}

// List is what the panel is told about itself, so the UI can hide a section the
// session may not use instead of rendering it into a 403.
func (p panelPermissions) List() []string {
	if p.list == nil {
		return []string{}
	}
	return p.list
}

func permissionsFromContext(ctx context.Context) panelPermissions {
	if permissions, ok := ctx.Value(permissionsKey{}).(panelPermissions); ok {
		return permissions
	}
	return panelPermissions{}
}

func isRequestSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	return proto == "https"
}

// securityHeadersMiddleware adds protective HTTP security headers to all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none';")
		next.ServeHTTP(w, r)
	})
}

// loginRateLimiter tracks and limits failed login attempts per remote IP to prevent brute-force attacks.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*ipLoginState
}

type ipLoginState struct {
	failedCount int
	firstFailed time.Time
	lockoutTime time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		attempts: make(map[string]*ipLoginState),
	}
}

func (l *loginRateLimiter) allow(ip string, now time.Time) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.attempts[ip]
	if !ok {
		return true, 0
	}
	if now.Before(state.lockoutTime) {
		return false, state.lockoutTime.Sub(now)
	}
	if now.Sub(state.firstFailed) > 5*time.Minute {
		delete(l.attempts, ip)
		return true, 0
	}
	return true, 0
}

func (l *loginRateLimiter) recordFailure(ip string, now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.attempts[ip]
	if !ok || now.Sub(state.firstFailed) > 5*time.Minute {
		l.attempts[ip] = &ipLoginState{
			failedCount: 1,
			firstFailed: now,
		}
		return
	}
	state.failedCount++
	if state.failedCount >= 5 {
		// Lock out for 1 minute after 5 consecutive failures in 5 minutes
		state.lockoutTime = now.Add(1 * time.Minute)
	}
}

func (l *loginRateLimiter) recordSuccess(ip string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

