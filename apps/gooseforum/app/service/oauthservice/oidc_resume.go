package oauthservice

import (
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/sessionstore"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/service/urlconfig"
	"github.com/markbates/goth/gothic"
)

const resumeCookie = "yourtj_oauth_resume"
const resumeLifetime = 10 * time.Minute

// oidcBridgePath is the built-in OIDC authorization bridge. A redirect aimed at
// it is an OIDC continuation and must pass oidcResumeTarget exactly.
const oidcBridgePath = "/api/oauth/authorize/callback"

// maxWebRedirectLength keeps the signed continuation cookie well under browser
// cookie limits; longer ordinary destinations fall back to "/".
const maxWebRedirectLength = 1024

// resumeKindWeb marks an ordinary same-site Web destination. Cookies without a
// kind (including ones issued before this field existed) are OIDC continuations.
const resumeKindWeb = "web"

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Only resume the fixed local authorization bridge. Its own browser-binding,
// expiry and single-use checks remain authoritative for the authorization ID.
func oidcResumeTarget(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid OAuth continuation")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Fragment != "" || u.Path != oidcBridgePath || len(q) != 1 || len(q["id"]) != 1 || !requestIDPattern.MatchString(q.Get("id")) {
		return "", errors.New("invalid OAuth continuation")
	}
	return oidcBridgePath + "?id=" + url.QueryEscape(q.Get("id")), nil
}

// targetsOIDCBridge reports whether raw addresses the OIDC bridge path (after
// percent-decoding). Such values keep the strict continuation contract: a
// malformed one is rejected instead of being downgraded to a Web redirect.
func targetsOIDCBridge(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Path == oidcBridgePath
}

// webRedirectTarget accepts an ordinary same-site destination such as the post
// a visitor was reading before choosing social login (/login?redirect=...).
// It reuses urlconfig.IsSafeRedirect on the raw value and again on the decoded
// path, so %2F%2Fhost and %5Chost cannot become //host or \host, and rejects
// control characters, any scheme/host/userinfo and over-long values.
func webRedirectTarget(raw string) (string, bool) {
	if len(raw) > maxWebRedirectLength || !urlconfig.IsSafeRedirect(raw) || hasControlChar(raw) {
		return "", false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !urlconfig.IsSafeRedirect(decoded) || hasControlChar(decoded) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || !urlconfig.IsSafeRedirect(u.Path) || u.Path == oidcBridgePath {
		return "", false
	}
	return raw, true
}

func hasControlChar(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// The continuation has a separate signed, HttpOnly cookie: Gothic replaces its
// own provider-session cookie at authorization start and deletes it on callback.
// Bind to a fresh server-generated OAuth state, provider and short expiry; never
// trust redirect/state supplied on the returning callback.
func prepareOIDCResume(w http.ResponseWriter, r *http.Request) error {
	session, _ := sessionstore.GetSession().New(r, resumeCookie)
	options := *session.Options
	session.Options = &options
	session.Values = make(map[interface{}]interface{})
	session.Options.MaxAge = -1
	if raw := r.URL.Query().Get("redirect"); raw != "" {
		provider := r.URL.Query().Get("provider")
		validProvider := provider == ProviderGitHub || provider == ProviderGoogle
		kind := ""
		target, err := oidcResumeTarget(raw)
		if err != nil {
			// An OIDC bridge continuation must be exact: the mobile client is
			// waiting for an authorization code, so a malformed one fails loudly.
			if targetsOIDCBridge(raw) {
				return err
			}
			// Ordinary Web login (/login?redirect=<post>): keep a safe same-site
			// destination; silently drop anything else and land on "/".
			webTarget, ok := webRedirectTarget(raw)
			if !ok || !validProvider {
				return session.Save(r, w)
			}
			target, kind = webTarget, resumeKindWeb
		} else if !validProvider {
			return errors.New("invalid OAuth provider")
		}
		state := rand.Text()
		q := r.URL.Query()
		q.Set("state", state)
		r.URL.RawQuery = q.Encode()
		session.Options.MaxAge = int(resumeLifetime / time.Second)
		session.Values["state"] = state
		session.Values["provider"] = provider
		session.Values["target"] = target
		session.Values["kind"] = kind
		session.Values["expires"] = time.Now().Add(resumeLifetime).Unix()
	}
	return session.Save(r, w)
}

func consumeOIDCResume(w http.ResponseWriter, r *http.Request) (string, error) {
	if _, err := r.Cookie(resumeCookie); errors.Is(err, http.ErrNoCookie) {
		return "", nil
	}
	session, err := sessionstore.GetSession().New(r, resumeCookie)
	options := *session.Options
	session.Options = &options
	session.Options.MaxAge = -1
	if saveErr := session.Save(r, w); saveErr != nil {
		return "", saveErr
	}
	if err != nil || session.IsNew {
		return "", errors.New("invalid OAuth continuation cookie")
	}
	state, _ := session.Values["state"].(string)
	provider, _ := session.Values["provider"].(string)
	expires, _ := session.Values["expires"].(int64)
	if state == "" || state != gothic.GetState(r) || provider != r.URL.Query().Get("provider") || expires <= time.Now().Unix() {
		return "", errors.New("OAuth continuation expired or mismatched")
	}
	target, _ := session.Values["target"].(string)
	if kind, _ := session.Values["kind"].(string); kind == resumeKindWeb {
		if webTarget, ok := webRedirectTarget(target); ok {
			return webTarget, nil
		}
		return "", errors.New("invalid OAuth continuation")
	}
	return oidcResumeTarget(target)
}
