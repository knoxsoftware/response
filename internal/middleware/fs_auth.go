package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// FSAuth returns middleware that validates a shared secret header from FreeSWITCH.
func FSAuth(secret string, next http.Handler) http.Handler {
	secretHash := sha256.Sum256([]byte(secret))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-FS-Secret")
		gotHash := sha256.Sum256([]byte(got))
		if subtle.ConstantTimeCompare(gotHash[:], secretHash[:]) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
