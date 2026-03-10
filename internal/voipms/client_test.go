package voipms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattventura/respond/internal/voipms"
)

func TestSendSMS(t *testing.T) {
	var gotMethod, gotDst string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		r.ParseForm()
		gotDst = r.FormValue("dst")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	c := voipms.NewClient("user", "pass", "5005550000", srv.URL)
	err := c.SendSMS(context.Background(), "+15005551234", "Hello")
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("expected GET, got %s", gotMethod)
	}
	if gotDst != "15005551234" {
		t.Errorf("expected dst=15005551234, got %s", gotDst)
	}
}
