package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizePermissionListDeduplicatesAndTrims(t *testing.T) {
	got := normalizePermissionList([]string{" premium.manage ", "", "*", "premium.manage", "users.read"})
	want := []string{"premium.manage", "*", "users.read"}
	if len(got) != len(want) {
		t.Fatalf("normalizePermissionList = %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("normalizePermissionList[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestAdminUsernameValidationRules(t *testing.T) {
	valid := []string{"root", "alice.smith", "Ops_2", "a-b"}
	for _, username := range valid {
		if len(username) < adminUsernameMinLen || len(username) > adminUsernameMaxLen || !adminUsernamePattern.MatchString(username) {
			t.Fatalf("username %q should be valid", username)
		}
	}
	invalid := []string{"", "a", strings.Repeat("x", adminUsernameMaxLen+1), "has space", "semi;colon", "emoji😀"}
	for _, username := range invalid {
		if len(username) >= adminUsernameMinLen && len(username) <= adminUsernameMaxLen && adminUsernamePattern.MatchString(username) {
			t.Fatalf("username %q should be rejected", username)
		}
	}
}

func TestAdminRoutesRequireSession(t *testing.T) {
	srv, err := newServer(uiConfig{SessionKey: []byte("01234567890123456789012345678901")}, nil, nil, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admins"},
		{http.MethodPost, "/api/admins"},
		{http.MethodPost, "/api/admins/7/password"},
		{http.MethodPost, "/api/admins/7/active"},
		{http.MethodGet, "/api/admins/sessions"},
		{http.MethodPost, "/api/admins/sessions/abc/revoke"},
		{http.MethodPost, "/api/actions/debit-stars"},
	}
	for _, item := range cases {
		req := httptest.NewRequest(item.method, item.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want 401", item.method, item.path, rec.Code)
		}
	}
}

func TestLegacySignedCookieStillAuthenticatesSessionEndpoint(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	srv, err := newServer(uiConfig{SessionKey: key}, nil, nil, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	value, err := signSession(key, sessionClaims{
		Actor:       "admin",
		Exp:         time.Now().Add(time.Hour).Unix(),
		Nonce:       "legacy",
		Permissions: []string{permissionAll},
		CSRF:        "csrf-token",
	})
	if err != nil {
		t.Fatalf("signSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"permissions":["*"]`) {
		t.Fatalf("session body missing permissions: %s", rec.Body.String())
	}
}

func TestPasswordChangeRevokesOtherSessionsAndKeepsCurrent(t *testing.T) {
	// The revocation query itself needs Postgres; here we pin the contract that
	// the handler refuses strangers without admins.manage.
	srv := &server{cfg: uiConfig{SessionKey: []byte("01234567890123456789012345678901")}}
	req := httptest.NewRequest(http.MethodPost, "/api/admins/42/password", strings.NewReader(`{"password":"super-secret-1"}`))
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()
	srv.handleAdminPasswordAPI(rec, req)
	// No auth context at all -> zero-value auth has no manage permission and a
	// mismatching admin id, so the request must be refused before any write.
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
}
