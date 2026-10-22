package oauthservice

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/sessionstore"
)

func TestOIDCResumeTarget(t *testing.T) {
	for _, raw := range []string{"https://evil.test/api/oauth/authorize/callback?id=x", "//evil.test/api/oauth/authorize/callback?id=x", "/settings?id=x", "/api/oauth/authorize/callback", "/api/oauth/authorize/callback?id=x&id=y", "/api/oauth/authorize/callback?id=x&redirect=//evil.test", "/api/oauth/authorize/callback?id=x#bad", "/api/oauth/authorize/callback?id=%0a", "/api/oauth/authorize/callback?id=%zz"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := oidcResumeTarget(raw); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
	if got, err := oidcResumeTarget("/api/oauth/authorize/callback?id=abc-123"); err != nil || got != "/api/oauth/authorize/callback?id=abc-123" {
		t.Fatalf("valid target: %q %v", got, err)
	}
}

func TestOIDCResumeCookie(t *testing.T) {
	const target = "/api/oauth/authorize/callback?id=abc-123"
	for _, tc := range []string{"success", "state", "provider", "expired", "tampered", "callbackRedirect", "missing"} {
		t.Run(tc, func(t *testing.T) {
			start := httptest.NewRequest(http.MethodGet, "/api/auth/github?provider=github&state=client-controlled&redirect="+url.QueryEscape(target), nil)
			rec := httptest.NewRecorder()
			if err := prepareOIDCResume(rec, start); err != nil {
				t.Fatal(err)
			}
			state := start.URL.Query().Get("state")
			if state == "" || state == "client-controlled" {
				t.Fatal("state must be server-generated")
			}
			cookie := rec.Result().Cookies()[0]
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 600 {
				t.Fatalf("cookie options: %v", cookie)
			}
			callback := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?provider=github&state="+url.QueryEscape(state), nil)
			if tc == "state" {
				callback.URL.RawQuery = "provider=github&state=wrong"
			}
			if tc == "provider" {
				callback.URL.RawQuery = "provider=google&state=" + url.QueryEscape(state)
			}
			if tc == "callbackRedirect" {
				callback.URL.RawQuery += "&redirect=https://evil.test"
			}
			if tc == "tampered" {
				cookie.Value = "tampered"
			}
			if tc == "expired" {
				session, _ := sessionstore.GetSession().New(start, resumeCookie)
				session.Values["state"] = state
				session.Values["provider"] = "github"
				session.Values["target"] = target
				session.Values["expires"] = time.Now().Add(-time.Minute).Unix()
				saved := httptest.NewRecorder()
				if err := session.Save(start, saved); err != nil {
					t.Fatal(err)
				}
				cookie = saved.Result().Cookies()[0]
			}
			if tc != "missing" {
				callback.AddCookie(cookie)
			}
			result := httptest.NewRecorder()
			got, err := consumeOIDCResume(result, callback)
			switch tc {
			case "success", "callbackRedirect":
				if err != nil || got != target {
					t.Fatalf("resume: %q %v", got, err)
				}
			case "missing":
				if err != nil || got != "" {
					t.Fatalf("ordinary login: %q %v", got, err)
				}
			default:
				if err == nil || got != "" {
					t.Fatal("invalid continuation accepted")
				}
			}
			if tc != "missing" && result.Result().Cookies()[0].MaxAge != -1 {
				t.Fatal("continuation not cleared")
			}
		})
	}
}

func TestOrdinaryOAuthClearsContinuation(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github?provider=github", nil)
	w := httptest.NewRecorder()
	if err := prepareOIDCResume(w, r); err != nil {
		t.Fatal(err)
	}
	if w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("ordinary login retained a stale continuation")
	}
	// Malformed OIDC bridge continuations still fail loudly.
	for _, target := range []string{"/api/oauth/authorize/callback", "/api/oauth/authorize/callback?id=x&id=y", "/api/oauth/authorize/%63allback?id=x&redirect=//evil.test"} {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/github?provider=github&redirect="+url.QueryEscape(target), nil)
		if err := prepareOIDCResume(httptest.NewRecorder(), req); err == nil {
			t.Fatalf("invalid OIDC continuation accepted: %q", target)
		}
	}
	// Unsafe ordinary redirects are dropped silently: no error, no continuation.
	for _, target := range unsafeWebRedirects {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/github?provider=github&state=client&redirect="+url.QueryEscape(target), nil)
		rec := httptest.NewRecorder()
		if err := prepareOIDCResume(rec, req); err != nil {
			t.Fatalf("unsafe web redirect %q: %v", target, err)
		}
		if rec.Result().Cookies()[0].MaxAge != -1 || req.URL.Query().Get("state") != "client" {
			t.Fatalf("unsafe web redirect %q retained a continuation", target)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/unknown?provider=unknown&redirect="+url.QueryEscape("/api/oauth/authorize/callback?id=x"), nil)
	if err := prepareOIDCResume(httptest.NewRecorder(), req); err == nil {
		t.Fatal("invalid provider accepted")
	}
}

var unsafeWebRedirects = []string{
	"//evil.com",
	"https://evil.com",
	"javascript:alert(1)",
	`/\evil.com`,
	`\evil.com`,
	"/%5Cevil.com",
	"/%5cevil.com",
	"%2F%2Fevil.com",
	"/%2F%2Fevil.com",
	"/%2f/evil.com",
	"/p/post/1\r\nLocation: https://evil.com",
	"/p/post/1%0d%0aSet-Cookie:x=1",
	"/p/%zz",
	"/" + strings.Repeat("a", maxWebRedirectLength),
}

func TestWebRedirectTarget(t *testing.T) {
	// The OIDC bridge is never a plain Web destination; it has its own validator.
	for _, raw := range append([]string{"", "/api/oauth/authorize/callback?id=x"}, unsafeWebRedirects...) {
		if got, ok := webRedirectTarget(raw); ok {
			t.Errorf("webRedirectTarget(%q) = %q, want rejected", raw, got)
		}
	}
	for _, raw := range []string{"/", "/p/post/123", "/p/post/93?page=2#post-5", "/search?q=a%2Fb", "/u/1?tab=topics"} {
		if got, ok := webRedirectTarget(raw); !ok || got != raw {
			t.Errorf("webRedirectTarget(%q) = %q,%v, want accepted unchanged", raw, got, ok)
		}
	}
}

// Ordinary Web login from a post page: /login?redirect=/p/post/123 builds
// /api/auth/<provider>?redirect=/p/post/123. It must start OAuth (no 400) and
// the callback must resume the same page; the callback query never overrides it.
func TestWebRedirectContinuationCookie(t *testing.T) {
	const target = "/p/post/123?page=2"
	for _, provider := range []string{ProviderGitHub, ProviderGoogle} {
		t.Run(provider, func(t *testing.T) {
			start := httptest.NewRequest(http.MethodGet, "/api/auth/"+provider+"?provider="+provider+"&redirect="+url.QueryEscape(target), nil)
			rec := httptest.NewRecorder()
			if err := prepareOIDCResume(rec, start); err != nil {
				t.Fatal(err)
			}
			state := start.URL.Query().Get("state")
			cookie := rec.Result().Cookies()[0]
			if state == "" || cookie.MaxAge != 600 || !cookie.HttpOnly {
				t.Fatalf("continuation not stored: state=%q cookie=%v", state, cookie)
			}
			callback := httptest.NewRequest(http.MethodGet, "/api/auth/"+provider+"/callback?provider="+provider+"&state="+url.QueryEscape(state)+"&redirect=https://evil.test", nil)
			callback.AddCookie(cookie)
			got, err := consumeOIDCResume(httptest.NewRecorder(), callback)
			if err != nil || got != target {
				t.Fatalf("resume: %q %v", got, err)
			}
		})
	}
	// A plain redirect with an unknown provider is ignored, not an error.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/unknown?provider=unknown&redirect="+url.QueryEscape(target), nil)
	rec := httptest.NewRecorder()
	if err := prepareOIDCResume(rec, req); err != nil || rec.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("unknown provider web redirect: %v", err)
	}
}
