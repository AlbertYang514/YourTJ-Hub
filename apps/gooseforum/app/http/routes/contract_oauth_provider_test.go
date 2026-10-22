package routes

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/sessionstore"
	"github.com/gin-gonic/gin"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
)

func preserveOAuthProviders(t *testing.T) {
	t.Helper()
	providers := make([]goth.Provider, 0, len(goth.GetProviders()))
	for _, provider := range goth.GetProviders() {
		providers = append(providers, provider)
	}
	store := gothic.Store
	goth.ClearProviders()
	t.Cleanup(func() {
		goth.ClearProviders()
		goth.UseProviders(providers...)
		gothic.Store = store
	})
}

func TestOAuthProviderUnavailableRouteUsesSiteErrorContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	preserveOAuthProviders(t)
	router := gin.New()
	apiRoute(router)

	for _, provider := range []string{"github", "google"} {
		t.Run(provider+" browser request", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/"+provider+"?lang=zh", nil))

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
			}
			if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("Content-Type = %q, want an HTML site error page", contentType)
			}
			if strings.Contains(recorder.Body.String(), "no provider for "+provider+" exists") {
				t.Fatalf("response leaked Goth's internal provider error: %s", recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `href="/"`) || !strings.Contains(recorder.Body.String(), "此登录方式暂不可用") {
				t.Fatalf("localized site error page or navigation link missing: %s", recorder.Body.String())
			}
		})

		t.Run(provider+" page request", func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/auth/"+provider, nil)
			request.Header.Set("X-Goose-Page", "true")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
			}
			if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("Content-Type = %q, want JSON page payload", contentType)
			}
			if !strings.Contains(recorder.Body.String(), `"messageCode":"oauth.provider.unavailable"`) {
				t.Fatalf("response lacks the localized provider-unavailable message code: %s", recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "no provider for "+provider+" exists") {
				t.Fatalf("response leaked Goth's internal provider error: %s", recorder.Body.String())
			}
		})
	}
}

func TestConfiguredOAuthProviderRouteStillStartsAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	preserveOAuthProviders(t)
	gothic.Store = sessionstore.GetSession()
	goth.UseProviders(github.New("test-client-id", "test-client-secret", "https://forum.example/api/auth/github/callback"))
	router := gin.New()
	apiRoute(router)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/github", nil))
	if recorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusTemporaryRedirect, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse OAuth redirect: %v", err)
	}
	if location.Host != "github.com" || location.Path != "/login/oauth/authorize" || location.Query().Get("state") == "" {
		t.Fatalf("OAuth redirect = %q, want GitHub authorization URL with state", location)
	}
}
