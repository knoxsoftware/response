package middleware

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"
)

// TwilioAuth returns middleware that validates Twilio request signatures.
func TwilioAuth(authToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validateSignature(authToken, r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validateSignature(authToken string, r *http.Request) bool {
	sig := r.Header.Get("X-Twilio-Signature")
	if sig == "" {
		return false
	}
	if err := r.ParseForm(); err != nil {
		return false
	}

	url := "https://" + r.Host + r.URL.RequestURI()
	var params []string
	for k, vs := range r.PostForm {
		for _, v := range vs {
			params = append(params, k+v)
		}
	}
	sort.Strings(params)
	s := url + strings.Join(params, "")

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(s))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}
