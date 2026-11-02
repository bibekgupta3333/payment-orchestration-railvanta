package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	// CSRFCookieName is the readable (not HttpOnly) double-submit companion of
	// the session cookie; the dashboard echoes it as X-CSRF-Token.
	CSRFCookieName = "railvanta_csrf"
	// VisitorEmailSuffix marks throwaway public-sandbox visitor users. Cleanup
	// only ever touches rows whose email ends with it.
	VisitorEmailSuffix = "@sandbox.invalid"
	// VisitorSessionTTL is the lifetime of a public-sandbox visitor session.
	VisitorSessionTTL = 12 * time.Hour
)

// RandomToken returns 32 bytes of crypto/rand as 64 hex characters.
func RandomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// CSRFCookie is readable by page script (the client echoes it as a header) but
// still Secure and SameSite=Strict, with the same expiry as the session.
func CSRFCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: CSRFCookieName, Value: token, Path: "/", Secure: true, HttpOnly: false, SameSite: http.SameSiteStrictMode, Expires: expires}
}

// ClearedCookies expire both cookies immediately.
func ClearedCookies() []*http.Cookie {
	session, csrf := SessionCookie("", time.Unix(0, 0)), CSRFCookie("", time.Unix(0, 0))
	session.MaxAge, csrf.MaxAge = -1, -1
	return []*http.Cookie{session, csrf}
}
