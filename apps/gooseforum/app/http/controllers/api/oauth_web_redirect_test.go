package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/sessionstore"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/userOAuth"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/users"
	"github.com/gin-gonic/gin"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/google"
)

// webRedirectRouter wires the real login/callback handlers with a provider whose
// upstream identity fetch is faked; Goth still stores and checks OAuth state.
func webRedirectRouter(t *testing.T, provider string, user goth.User) func(string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var upstream goth.Provider = github.New("test", "test", "https://forum.example/api/auth/github/callback")
	if provider == "google" {
		upstream = google.New("test", "test", "https://forum.example/api/auth/google/callback")
	}
	old, oldErr := goth.GetProvider(provider)
	oldStore := gothic.Store
	gothic.Store = sessionstore.GetSession()
	goth.UseProviders(resumeProvider{Provider: upstream, user: user})
	t.Cleanup(func() {
		gothic.Store = oldStore
		if oldErr == nil {
			goth.UseProviders(old)
		} else {
			delete(goth.GetProviders(), provider)
		}
	})
	router := gin.New()
	router.GET("/api/auth/:provider", ProviderLogin)
	router.GET("/api/auth/:provider/callback", ProviderCallback)
	cookies := map[string]*http.Cookie{}
	return func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		for _, c := range rec.Result().Cookies() {
			if c.MaxAge < 0 {
				delete(cookies, c.Name)
			} else {
				cookies[c.Name] = c
			}
		}
		return rec
	}
}

// socialLogin follows /api/auth/<provider>?redirect=... to the upstream
// authorization URL and back through the callback, returning the callback.
func socialLogin(t *testing.T, get func(string) *httptest.ResponseRecorder, provider, redirect string) *httptest.ResponseRecorder {
	t.Helper()
	start := get("/api/auth/" + provider + "?redirect=" + url.QueryEscape(redirect))
	if start.Code != http.StatusTemporaryRedirect && start.Code != http.StatusFound {
		t.Fatalf("login start redirect=%q: status=%d body=%s", redirect, start.Code, start.Body.String())
	}
	authorization, err := url.Parse(start.Header().Get("Location"))
	if err != nil || authorization.Host == "" || authorization.Query().Get("state") == "" {
		t.Fatalf("upstream authorization: %q", start.Header().Get("Location"))
	}
	return get("/api/auth/" + provider + "/callback?state=" + url.QueryEscape(authorization.Query().Get("state")) + "&code=upstream-code")
}

// Post page -> /login?redirect=/p/post/123 -> GitHub/Google used to fail with
// 400 "Invalid OAuth continuation". It must log in and return to the post.
func TestSocialLoginReturnsToSafeWebRedirect(t *testing.T) {
	const post = "/p/post/123?page=2"
	for _, provider := range []string{"github", "google"} {
		t.Run(provider, func(t *testing.T) {
			setupOAuthCallbackTestDB(t)
			user := &users.EntityComplete{Username: "webredirect" + provider, Email: "webredirect" + provider + "@example.com", IsActivated: users.ActivationSuccess}
			if err := users.Create(user); err != nil {
				t.Fatal(err)
			}
			if err := userOAuth.Create(&userOAuth.Entity{UserId: user.Id, Provider: provider, ProviderUid: "web-" + provider}); err != nil {
				t.Fatal(err)
			}
			get := webRedirectRouter(t, provider, goth.User{Provider: provider, UserID: "web-" + provider})

			rec := socialLogin(t, get, provider, post)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != post || !hasAccessTokenCookie(rec) {
				t.Fatalf("callback status=%d location=%q session=%v body=%s", rec.Code, rec.Header().Get("Location"), hasAccessTokenCookie(rec), rec.Body.String())
			}

			// Unsafe destinations never 400 and never leave the site: land on "/".
			for _, unsafe := range []string{"//evil.com", "https://evil.com", `/\evil.com`, "/%2F%2Fevil.com", "%2F%2Fevil.com", "/%5Cevil.com"} {
				rec := socialLogin(t, get, provider, unsafe)
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
					t.Fatalf("unsafe %q: status=%d location=%q", unsafe, rec.Code, rec.Header().Get("Location"))
				}
			}
		})
	}
}

// A first-time social user is sent to registration and keeps the post as the
// registration continuation.
func TestSocialLoginWithoutAccountKeepsWebRedirect(t *testing.T) {
	setupOAuthCallbackTestDB(t)
	get := webRedirectRouter(t, "github", goth.User{Provider: "github", UserID: "web-new", NickName: "webnew", Email: "webnew@example.com"})
	rec := socialLogin(t, get, "github", "/p/post/123")
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(loc, "/login?register=true&oauthNotice=1") || !strings.Contains(loc, "redirect=%2Fp%2Fpost%2F123") || hasAccessTokenCookie(rec) {
		t.Fatalf("register redirect: status=%d location=%q", rec.Code, loc)
	}
}
