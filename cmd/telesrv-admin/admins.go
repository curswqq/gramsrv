package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// Panel administrator accounts and their server-side sessions.
//
// The panel started with a single shared secret from the environment and a
// stateless signed cookie. That shape cannot answer "who else is logged in"
// or "change my password", so this file adds real accounts (admin_users) and
// server-side sessions (admin_sessions) on top:
//
//   - Login resolves an account, checks a bcrypt hash and inserts a session
//     row; the cookie carries only the opaque session id.
//   - Every authenticated request re-reads the session row, so revocation,
//     expiry and deactivation take effect immediately.
//   - Both tables are created idempotently at startup, because this process
//     starts before the main server runs golang-migrate; the canonical
//     migration lives in deploy/migrations/0183_admin_accounts.
//
// Compatibility: while no accounts exist (or the schema is not there yet),
// login and session verification fall back to the previous single-secret
// behaviour, so an operator is never locked out by this change.

const (
	bootstrapAdminUsername = "root"
	adminUsernameMinLen    = 2
	adminUsernameMaxLen    = 32
	adminPasswordMinLen    = 8
	adminPasswordMaxLen    = 128
)

var adminUsernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

var errAdminNotFound = errors.New("admin user not found")

type adminUserRow struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	Permissions []string  `json:"permissions"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Sessions    int       `json:"active_sessions"`
}

type adminSessionRow struct {
	ID         string    `json:"id"`
	AdminID    int64     `json:"admin_id"`
	Username   string    `json:"username"`
	IPAddr     string    `json:"ip_addr"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// panelAuth is the resolved identity attached to every authenticated request.
// Legacy signed-cookie sessions carry admin_id=0 and session_id="".
type panelAuth struct {
	adminID     int64
	username    string
	permissions panelPermissions
	csrf        string
	sessionID   string
}

func newSessionID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// ---------------------------------------------------------------------------
// Schema and bootstrap

func (s *server) ensureAdminSchema(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS public.admin_users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    permissions text[] NOT NULL DEFAULT '{}'::text[],
    active boolean NOT NULL DEFAULT true,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE IF NOT EXISTS public.admin_sessions (
    id text PRIMARY KEY,
    admin_id bigint NOT NULL REFERENCES public.admin_users(id) ON DELETE CASCADE,
    csrf_token text NOT NULL,
    ip_addr text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS admin_sessions_admin_active_idx ON public.admin_sessions (admin_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS admin_sessions_expiry_idx ON public.admin_sessions (expires_at);
`)
	return err
}

// EnsureBootstrapAdmin seeds the first account from the environment secret so
// the upgrade is invisible to an existing operator: they keep signing in as
// "root" with the same TELESRV_ADMIN_UI_PASSWORD value.
func (s *server) EnsureBootstrapAdmin(ctx context.Context) {
	if err := s.ensureAdminSchema(ctx); err != nil {
		log.Printf("admin accounts schema unavailable: %v", err)
		return
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM public.admin_users").Scan(&count); err != nil {
		log.Printf("admin accounts bootstrap count failed: %v", err)
		return
	}
	if count > 0 {
		return
	}
	password := s.cfg.Password
	if strings.TrimSpace(password) == "" {
		log.Print("admin accounts: no TELESRV_ADMIN_UI_PASSWORD configured; skipping root seed")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("admin accounts bootstrap hash failed: %v", err)
		return
	}
	permissions := s.cfg.Permissions
	if len(permissions) == 0 {
		permissions = []string{permissionAll}
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO public.admin_users (username, password_hash, permissions)
		 VALUES ($1, $2, $3) ON CONFLICT (username) DO NOTHING`,
		bootstrapAdminUsername, string(hash), permissions,
	); err != nil {
		log.Printf("admin accounts bootstrap insert failed: %v", err)
		return
	}
	log.Printf("admin accounts: seeded initial %q account from environment credentials", bootstrapAdminUsername)
}

// ---------------------------------------------------------------------------
// Store queries

func (s *server) adminByUsername(ctx context.Context, username string) (adminUserRow, []byte, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, permissions, active, created_at, updated_at
		 FROM public.admin_users WHERE username = $1`, username)
	var admin adminUserRow
	var hash string
	var permissions []string
	if err := row.Scan(&admin.ID, &admin.Username, &hash, &permissions, &admin.Active, &admin.CreatedAt, &admin.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return adminUserRow{}, nil, errAdminNotFound
		}
		return adminUserRow{}, nil, err
	}
	admin.Permissions = permissions
	return admin, []byte(hash), nil
}

func (s *server) adminCount(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, "SELECT count(*) FROM public.admin_users").Scan(&count)
	return count, err
}

func (s *server) listAdminUsers(ctx context.Context) ([]adminUserRow, error) {
	rows, err := s.pool.Query(ctx, `
SELECT u.id, u.username, u.permissions, u.active, u.created_at, u.updated_at,
       (SELECT count(*) FROM public.admin_sessions s
         WHERE s.admin_id = u.id AND s.revoked_at IS NULL AND s.expires_at > now()) AS active_sessions
FROM public.admin_users u ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adminUserRow{}
	for rows.Next() {
		var admin adminUserRow
		if err := rows.Scan(&admin.ID, &admin.Username, &admin.Permissions, &admin.Active, &admin.CreatedAt, &admin.UpdatedAt, &admin.Sessions); err != nil {
			return nil, err
		}
		out = append(out, admin)
	}
	return out, rows.Err()
}

func (s *server) createAdminUser(ctx context.Context, username, passwordHash string, permissions []string) (adminUserRow, error) {
	var admin adminUserRow
	err := s.pool.QueryRow(ctx,
		`INSERT INTO public.admin_users (username, password_hash, permissions)
		 VALUES ($1, $2, $3)
		 RETURNING id, username, permissions, active, created_at, updated_at`,
		username, passwordHash, permissions,
	).Scan(&admin.ID, &admin.Username, &admin.Permissions, &admin.Active, &admin.CreatedAt, &admin.UpdatedAt)
	return admin, err
}

func (s *server) updateAdminPassword(ctx context.Context, adminID int64, passwordHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE public.admin_users SET password_hash = $2, updated_at = now() WHERE id = $1`,
		adminID, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAdminNotFound
	}
	return nil
}

func (s *server) updateAdminActive(ctx context.Context, adminID int64, active bool) (bool, error) {
	var updated bool
	err := s.pool.QueryRow(ctx,
		`UPDATE public.admin_users SET active = $2, updated_at = now() WHERE id = $1 RETURNING active`,
		adminID, active).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errAdminNotFound
	}
	return updated, err
}

// revokeOtherAdminSessions terminates every live session of the account except
// keepSessionID (the operator's own browser, when they rotate their password).
func (s *server) revokeOtherAdminSessions(ctx context.Context, adminID int64, keepSessionID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE public.admin_sessions SET revoked_at = now()
		 WHERE admin_id = $1 AND revoked_at IS NULL AND id <> $2`,
		adminID, keepSessionID)
	return err
}

func (s *server) insertAdminSession(ctx context.Context, sessionID string, adminID int64, csrf, ipAddr, userAgent string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO public.admin_sessions (id, admin_id, csrf_token, ip_addr, user_agent, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID, adminID, csrf, ipAddr, userAgent, expiresAt)
	return err
}

func (s *server) lookupLiveSession(ctx context.Context, sessionID string) (*panelAuth, error) {
	var (
		adminID     int64
		username    string
		csrf        string
		active      bool
		permissions []string
	)
	err := s.pool.QueryRow(ctx, `
SELECT s.id, s.admin_id, u.username, u.active, u.permissions, s.csrf_token
FROM public.admin_sessions s JOIN public.admin_users u ON u.id = s.admin_id
WHERE s.id = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`, sessionID).
		Scan(&sessionID, &adminID, &username, &active, &permissions, &csrf)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, errAdminNotFound
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE public.admin_sessions SET last_seen_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		sessionID); err != nil {
		return nil, err
	}
	return &panelAuth{
		adminID:     adminID,
		username:    username,
		permissions: newPanelPermissions(permissions),
		csrf:        csrf,
		sessionID:   sessionID,
	}, nil
}

func (s *server) revokeAdminSession(ctx context.Context, sessionID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE public.admin_sessions SET revoked_at = now()
		 WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errAdminNotFound
	}
	return nil
}

func (s *server) listAdminSessions(ctx context.Context, adminID int64, all bool) ([]adminSessionRow, error) {
	query := `
SELECT s.id, s.admin_id, u.username, s.ip_addr, s.user_agent, s.created_at, s.last_seen_at, s.expires_at
FROM public.admin_sessions s JOIN public.admin_users u ON u.id = s.admin_id
WHERE s.revoked_at IS NULL AND s.expires_at > now()`
	args := []any{}
	if !all {
		query += " AND s.admin_id = $1"
		args = append(args, adminID)
	}
	query += " ORDER BY s.last_seen_at DESC LIMIT 200"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adminSessionRow{}
	for rows.Next() {
		var item adminSessionRow
		if err := rows.Scan(&item.ID, &item.AdminID, &item.Username, &item.IPAddr, &item.UserAgent, &item.CreatedAt, &item.LastSeenAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// HTTP handlers

func (s *server) adminsManage(handler http.HandlerFunc) http.Handler {
	return s.requireAuthAPI(s.requirePermission(permissionAdminsManage, handler))
}

type createAdminAPIRequest struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Permissions []string `json:"permissions"`
}

func (s *server) handleAdminsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		admins, err := s.listAdminUsers(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admins": admins})
	case http.MethodPost:
		var body createAdminAPIRequest
		if !decodeAction(w, r, &body) {
			return
		}
		username := strings.TrimSpace(body.Username)
		if len(username) < adminUsernameMinLen || len(username) > adminUsernameMaxLen || !adminUsernamePattern.MatchString(username) {
			writeAPIError(w, http.StatusBadRequest, "invalid username")
			return
		}
		if len(body.Password) < adminPasswordMinLen || len(body.Password) > adminPasswordMaxLen {
			writeAPIError(w, http.StatusBadRequest, "password length must be between 8 and 128 characters")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		admin, err := s.createAdminUser(r.Context(), username, string(hash), normalizePermissionList(body.Permissions))
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
				writeAPIError(w, http.StatusConflict, "username already exists")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": admin})
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type adminPasswordAPIRequest struct {
	Password string `json:"password"`
}

func (s *server) handleAdminPasswordAPI(w http.ResponseWriter, r *http.Request) {
	// The right to touch this account is checked before anything else: a stranger
	// probing ids gets a uniform 403 whether or not the account exists.
	auth := authFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid admin id")
		return
	}
	if auth.adminID != id && !auth.permissions.Has(permissionAdminsManage) {
		writeAPIError(w, http.StatusForbidden, "permission "+permissionAdminsManage+" is required")
		return
	}
	var body adminPasswordAPIRequest
	if !decodeAction(w, r, &body) {
		return
	}
	if len(body.Password) < adminPasswordMinLen || len(body.Password) > adminPasswordMaxLen {
		writeAPIError(w, http.StatusBadRequest, "password length must be between 8 and 128 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.updateAdminPassword(r.Context(), id, string(hash)); err != nil {
		if errors.Is(err, errAdminNotFound) {
			writeAPIError(w, http.StatusNotFound, "admin user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A rotated password strands every other browser that still holds a cookie.
	if err := s.revokeOtherAdminSessions(r.Context(), id, auth.sessionID); err != nil {
		log.Printf("revoke sessions after password change failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type adminActiveAPIRequest struct {
	Active bool `json:"active"`
}

func (s *server) handleAdminActiveAPI(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid admin id")
		return
	}
	auth := authFromContext(r.Context())
	if auth.adminID == id && !r.URL.Query().Has("force") {
		writeAPIError(w, http.StatusBadRequest, "cannot disable your own account")
		return
	}
	var body adminActiveAPIRequest
	if !decodeAction(w, r, &body) {
		return
	}
	updated, err := s.updateAdminActive(r.Context(), id, body.Active)
	if err != nil {
		if errors.Is(err, errAdminNotFound) {
			writeAPIError(w, http.StatusNotFound, "admin user not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !body.Active {
		if err := s.revokeOtherAdminSessions(r.Context(), id, ""); err != nil {
			log.Printf("revoke sessions after disable failed: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": updated})
}

func (s *server) handleAdminSessionsAPI(w http.ResponseWriter, r *http.Request) {
	auth := authFromContext(r.Context())
	all := auth.permissions.Has(permissionAdminsManage)
	sessions, err := s.listAdminSessions(r.Context(), auth.adminID, all)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *server) handleAdminSessionRevokeAPI(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	auth := authFromContext(r.Context())
	if auth.sessionID != sessionID && !auth.permissions.Has(permissionAdminsManage) {
		writeAPIError(w, http.StatusForbidden, "permission "+permissionAdminsManage+" is required")
		return
	}
	if auth.sessionID == sessionID {
		// Revoking the current session is what logout already does better.
		writeAPIError(w, http.StatusBadRequest, "use logout to end the current session")
		return
	}
	if err := s.revokeAdminSession(r.Context(), sessionID); err != nil {
		if errors.Is(err, errAdminNotFound) {
			writeAPIError(w, http.StatusNotFound, "session not found or already closed")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func normalizePermissionList(values []string) []string {
	out := []string{}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// handleAccountLogin authenticates against admin_users and opens a server-side
// session. It is reached only when at least one account exists (see the login
// route), so a missing row is simply a bad credential.
func (s *server) handleAccountLogin(w http.ResponseWriter, r *http.Request, username, secret string) {
	admin, hash, err := s.adminByUsername(r.Context(), username)
	if err != nil || !admin.Active {
		writeAPIError(w, http.StatusUnauthorized, "invalid credential")
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(secret)) != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid credential")
		return
	}
	sessionID, err := newSessionID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	csrfToken, err := newCSRFToken()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	expires := time.Now().Add(sessionTTL)
	if err := s.insertAdminSession(r.Context(), sessionID, admin.ID, csrfToken, clientIP(r), r.UserAgent(), expires); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setSessionCookie(w, sessionID, sessionTTL)
	setCSRFCookie(w, csrfToken, sessionTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"actor":       admin.Username,
		"username":    admin.Username,
		"admin_id":    admin.ID,
		"permissions": newPanelPermissions(admin.Permissions).List(),
		"csrf_token":  csrfToken,
	})
}

// clientIP prefers the first X-Forwarded-For hop: Caddy sits in front of this
// process on production and sets it for the public host.
func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found || first != "" {
			return strings.TrimSpace(first)
		}
	}
	host := r.RemoteAddr
	if _, port, found := strings.Cut(host, ":"); found {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return host
}
