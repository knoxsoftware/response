# FreeSWITCH Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate `respond` from Twilio to FreeSWITCH + VoIP.ms, adding an SMS decision-tree interface for customers.

**Architecture:** FreeSWITCH on a standalone VPS handles voice via `mod_xml_curl`, calling back to the Go app with FreeSWITCH XML instead of TwiML. VoIP.ms delivers inbound SMS directly to the Go app via HTTP webhook; outbound SMS uses the VoIP.ms REST API. All business logic, session state, and DB are unchanged.

**Tech Stack:** Go, FreeSWITCH `mod_xml_curl`, VoIP.ms SIP trunk + REST API, PostgreSQL, Kubernetes, YAML config

---

## Task 1: Replace `internal/twiml` with `internal/fsxml`

FreeSWITCH's `mod_xml_curl` expects a different XML format than TwiML. This task creates the new XML generation package.

**Files:**
- Create: `internal/fsxml/fsxml.go`
- Create: `internal/fsxml/fsxml_test.go`

**Background — FreeSWITCH XML dialplan via mod_xml_curl:**

When `mod_xml_curl` calls your app, it POSTs form fields similar to Twilio (caller, call UUID, DTMF digits). Your app responds with a FreeSWITCH XML document. Key differences from TwiML:

- Root element is `<document type="freeswitch/xml">`
- Contains `<section name="dialplan">` → `<context>` → `<extension>` → `<condition>` → `<action>` elements
- Text-to-speech uses `<action application="speak" data="text"/>` (requires `mod_flite` or `mod_tts_commandline`)
- DTMF gathering uses `<action application="play_and_get_digits" data="min max tries timeout file terminator varname regexp digit_timeout invalid_file"/>`
- Simultaneous dial uses `<action application="bridge" data="sofia/gateway/voipms/+1XXXXXXXXXX,sofia/gateway/voipms/+1YYYYYYYYYY"/>` with `{ignore_early_media=true}` prefix
- Hanging up uses `<action application="hangup"/>`

**Step 1: Write the failing tests**

Create `internal/fsxml/fsxml_test.go`:

```go
package fsxml_test

import (
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/fsxml"
)

func TestSay(t *testing.T) {
	out := fsxml.Say("Hello world")
	if !strings.Contains(out, `application="speak"`) {
		t.Errorf("Say() missing speak action: %s", out)
	}
	if !strings.Contains(out, "Hello world") {
		t.Errorf("Say() missing message text: %s", out)
	}
	if !strings.Contains(out, `application="hangup"`) {
		t.Errorf("Say() missing hangup: %s", out)
	}
}

func TestGather(t *testing.T) {
	out := fsxml.Gather("Press 1 or 2", "myvar", "/fs/gather", 1)
	if !strings.Contains(out, `application="play_and_get_digits"`) {
		t.Errorf("Gather() missing play_and_get_digits: %s", out)
	}
	if !strings.Contains(out, "myvar") {
		t.Errorf("Gather() missing var name: %s", out)
	}
	if !strings.Contains(out, "Press 1 or 2") {
		t.Errorf("Gather() missing prompt: %s", out)
	}
}

func TestGatherNoLimit(t *testing.T) {
	out := fsxml.Gather("Enter PIN", "pin_var", "/fs/gather", 0)
	// numDigits=0 means use # as terminator, no digit limit
	if !strings.Contains(out, "#") {
		t.Errorf("Gather() with numDigits=0 should use # terminator: %s", out)
	}
}

func TestDial(t *testing.T) {
	out := fsxml.Dial([]string{"+13035551234", "+13035555678"})
	if !strings.Contains(out, `application="bridge"`) {
		t.Errorf("Dial() missing bridge action: %s", out)
	}
	if !strings.Contains(out, "13035551234") {
		t.Errorf("Dial() missing first number: %s", out)
	}
	if !strings.Contains(out, "13035555678") {
		t.Errorf("Dial() missing second number: %s", out)
	}
}

func TestDialEmpty(t *testing.T) {
	out := fsxml.Dial([]string{})
	if !strings.Contains(out, `application="speak"`) {
		t.Errorf("Dial() with empty numbers should speak error: %s", out)
	}
	if !strings.Contains(out, "no available responders") {
		t.Errorf("Dial() with empty should say no responders: %s", out)
	}
}

func TestXMLEscape(t *testing.T) {
	out := fsxml.Say(`<script>alert("xss")</script>`)
	if strings.Contains(out, "<script>") {
		t.Errorf("Say() did not escape XML: %s", out)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/fsxml/...
```
Expected: `cannot find package` or compile error.

**Step 3: Implement `internal/fsxml/fsxml.go`**

```go
package fsxml

import (
	"fmt"
	"strings"
)

// Say returns a FreeSWITCH XML document that speaks msg then hangs up.
func Say(msg string) string {
	return wrap(
		action("speak", xmlEscape(msg)),
		action("hangup", ""),
	)
}

// Gather returns a FreeSWITCH XML document that plays msg and collects DTMF into varName,
// then POSTs to actionURL. numDigits=0 means collect until # (no length limit).
func Gather(msg, varName, actionURL string, numDigits int) string {
	// play_and_get_digits args: min max tries timeout prompt_file terminator varname regexp digit_timeout invalid_file
	// We use say: prefix for TTS. terminator is # when numDigits=0, else empty.
	min := "1"
	max := "128"
	terminator := "#"
	if numDigits > 0 {
		max = fmt.Sprintf("%d", numDigits)
		terminator = "none"
	}
	data := fmt.Sprintf("%s %s 3 10000 say:%s %s %s \\d+ 5000 say:Invalid input",
		min, max, xmlEscape(msg), terminator, varName)
	// After gathering, transfer to the action URL handler via the session
	// FreeSWITCH will POST the collected digits in a variable named varName
	return wrap(
		action("play_and_get_digits", data),
		// Signal the dialplan to continue — the XML curl binding handles routing
		action("transfer", xmlEscape(actionURL)),
	)
}

// Dial returns a FreeSWITCH XML document that bridges to all numbers simultaneously.
func Dial(numbers []string) string {
	if len(numbers) == 0 {
		return Say("There are no available responders at this time. Please try again later.")
	}
	var legs []string
	for _, n := range numbers {
		// Remove + for sofia gateway format, FreeSWITCH gateway adds country code
		legs = append(legs, fmt.Sprintf("sofia/gateway/voipms/%s", xmlEscape(n)))
	}
	// {ignore_early_media=true} allows simultaneous ring
	data := "{ignore_early_media=true}" + strings.Join(legs, ",")
	return wrap(action("bridge", data))
}

// wrap builds a complete FreeSWITCH XML dialplan document with the given actions.
func wrap(actions ...string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<document type="freeswitch/xml">`)
	sb.WriteString(`<section name="dialplan">`)
	sb.WriteString(`<context name="default">`)
	sb.WriteString(`<extension name="respond">`)
	sb.WriteString(`<condition field="destination_number" expression=".*">`)
	for _, a := range actions {
		sb.WriteString(a)
	}
	sb.WriteString(`</condition>`)
	sb.WriteString(`</extension>`)
	sb.WriteString(`</context>`)
	sb.WriteString(`</section>`)
	sb.WriteString(`</document>`)
	return sb.String()
}

func action(app, data string) string {
	if data == "" {
		return fmt.Sprintf(`<action application="%s"/>`, app)
	}
	return fmt.Sprintf(`<action application="%s" data="%s"/>`, app, data)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/fsxml/... -v
```
Expected: all tests PASS.

**Step 5: Commit**

```bash
git add internal/fsxml/
git commit -m "feat: add internal/fsxml package replacing twiml"
```

---

## Task 2: Replace Twilio auth middleware with FreeSWITCH shared secret

**Files:**
- Create: `internal/middleware/fs_auth.go`
- Create: `internal/middleware/fs_auth_test.go`
- Keep: `internal/middleware/twilio_auth.go` (delete at end)

**Step 1: Write the failing test**

Create `internal/middleware/fs_auth_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattventura/respond/internal/middleware"
)

func TestFSAuth_ValidSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.FSAuth("mysecret", next)

	req := httptest.NewRequest("POST", "/fs/voice", nil)
	req.Header.Set("X-FS-Secret", "mysecret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler was not called with valid secret")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestFSAuth_InvalidSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.FSAuth("mysecret", next)

	req := httptest.NewRequest("POST", "/fs/voice", nil)
	req.Header.Set("X-FS-Secret", "wrongsecret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should not be called with invalid secret")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestFSAuth_MissingSecret(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.FSAuth("mysecret", next)

	req := httptest.NewRequest("POST", "/fs/voice", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("next handler should not be called with missing secret")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/middleware/... -run TestFSAuth
```
Expected: compile error — `FSAuth` not defined.

**Step 3: Implement `internal/middleware/fs_auth.go`**

```go
package middleware

import (
	"crypto/subtle"
	"net/http"
)

// FSAuth returns middleware that validates a shared secret header from FreeSWITCH.
func FSAuth(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-FS-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

**Step 4: Run tests**

```bash
go test ./internal/middleware/... -v
```
Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/middleware/fs_auth.go internal/middleware/fs_auth_test.go
git commit -m "feat: add FreeSWITCH shared secret auth middleware"
```

---

## Task 3: Update config — swap Twilio vars for FreeSWITCH + VoIP.ms vars

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Update `config.go`**

Replace the `TwilioAuthToken` field and env var with the new fields:

```go
package config

import (
	"fmt"
	"os"
	"strings"
)

type BootstrapAdmin struct {
	Phone string
	PIN   string
}

type Config struct {
	DatabaseURL     string
	FSSharedSecret  string
	VoIPMSUsername  string
	VoIPMSPassword  string
	VoIPMSDID       string
	SMSTreePath     string
	Port            string
	BaseURL         string
	BootstrapAdmins []BootstrapAdmin
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		FSSharedSecret: os.Getenv("FS_SHARED_SECRET"),
		VoIPMSUsername: os.Getenv("VOIPMS_USERNAME"),
		VoIPMSPassword: os.Getenv("VOIPMS_PASSWORD"),
		VoIPMSDID:      os.Getenv("VOIPMS_DID"),
		SMSTreePath:    os.Getenv("SMS_TREE_PATH"),
		Port:           os.Getenv("PORT"),
		BaseURL:        os.Getenv("BASE_URL"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.FSSharedSecret == "" {
		return nil, fmt.Errorf("FS_SHARED_SECRET is required")
	}
	if c.VoIPMSUsername == "" || c.VoIPMSPassword == "" || c.VoIPMSDID == "" {
		return nil, fmt.Errorf("VOIPMS_USERNAME, VOIPMS_PASSWORD, and VOIPMS_DID are required")
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("BASE_URL is required")
	}
	if c.SMSTreePath == "" {
		c.SMSTreePath = "/config/sms-tree.yaml"
	}
	if raw := os.Getenv("BOOTSTRAP_ADMINS"); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("BOOTSTRAP_ADMINS: invalid entry %q, expected phone:pin", entry)
			}
			c.BootstrapAdmins = append(c.BootstrapAdmins, BootstrapAdmin{
				Phone: parts[0],
				PIN:   parts[1],
			})
		}
	}
	return c, nil
}
```

**Step 2: Verify it compiles**

```bash
go build ./...
```
Expected: compile errors in `main.go` referencing `TwilioAuthToken` — that's fine, fixed in next task.

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: update config for FreeSWITCH and VoIP.ms vars"
```

---

## Task 4: Update handlers to use `fsxml` and wire new routes in `main.go`

The handlers (`VoiceHandler`, `GatherHandler`, `StatusHandler`) call `twiml.*` functions. Swap those for `fsxml.*`. Then update `main.go` to use the new middleware and route prefix.

**Files:**
- Modify: `internal/handler/voice.go`
- Modify: `internal/handler/gather.go`
- Modify: `internal/handler/status.go` (check if it uses twiml)
- Modify: `cmd/respond/main.go`
- Delete: `internal/middleware/twilio_auth.go`
- Delete: `internal/twiml/twiml.go`

**Step 1: Update `internal/handler/voice.go`**

Change the import from `twiml` to `fsxml` and all `twiml.` calls to `fsxml.`:

```go
package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/fsxml"
	"github.com/mattventura/respond/internal/store"
)

type VoiceHandler struct {
	Responders *store.ResponderStore
	Sessions   *store.SessionStore
	BaseURL    string
}

func (h *VoiceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// FreeSWITCH sends caller as Caller-ID-Number, call ID as Unique-ID
	from := r.FormValue("Caller-ID-Number")
	callSid := r.FormValue("Unique-ID")
	ctx := r.Context()

	w.Header().Set("Content-Type", "application/xml")

	responder, err := h.Responders.FindByPhone(ctx, from)

	switch {
	case err == nil:
		h.startResponderFlow(w, r, ctx, responder, callSid)
	default:
		h.dispatchFlow(w, ctx)
	}
}

func (h *VoiceHandler) dispatchFlow(w http.ResponseWriter, ctx context.Context) {
	available, err := h.Responders.ListAvailable(ctx)
	if err != nil {
		log.Printf("list available: %v", err)
		w.Write([]byte(fsxml.Say("System error. Please try again.")))
		return
	}
	numbers := make([]string, len(available))
	for i, r := range available {
		numbers[i] = r.PhoneNumber
	}
	w.Write([]byte(fsxml.Dial(numbers)))
}

func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	if !resp.IsValidated {
		sess := &store.Session{
			CallSid: callSid,
			Caller:  resp.PhoneNumber,
			State:   store.SessionState{Step: "responder_set_pin", Pending: map[string]string{}},
		}
		if err := h.Sessions.Upsert(ctx, sess); err != nil {
			log.Printf("upsert session: %v", err)
		}
		w.Write([]byte(fsxml.Gather("Welcome. Please enter a PIN to secure your account, followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
		return
	}

	sess := &store.Session{
		CallSid: callSid,
		Caller:  resp.PhoneNumber,
		State:   store.SessionState{Step: "responder_pin"},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(fsxml.Gather("Please enter your PIN followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
}
```

**Step 2: Update `internal/handler/gather.go`**

Change import and all `twiml.` references to `fsxml.`, and update the gather action URL prefix from `/twilio/` to `/fs/`, and the DTMF variable from `Digits` to `pin_input` (the varName passed to `fsxml.Gather`):

- Import: `"github.com/mattventura/respond/internal/fsxml"`
- Replace all `twiml.Say(` → `fsxml.Say(`
- Replace all `twiml.Gather(` → `fsxml.Gather(` — note signature change: add `"menu_input"` or `"pin_input"` as second arg (varName), and adjust numDigits position
- Replace all `"/twilio/voice/gather"` → `h.BaseURL+"/fs/gather"` (already uses BaseURL, just update path segment)
- In `ServeHTTP`, change `digits := r.FormValue("Digits")` → `digits := r.FormValue("pin_input")` — FreeSWITCH POSTs the variable by name

The `fsxml.Gather` signature is: `Gather(msg, varName, actionURL string, numDigits int)`. All existing calls use either `0` (open-ended, uses #) or `1` (single digit for menus). Use `"pin_input"` for PIN flows and `"menu_input"` for menu flows.

In `ServeHTTP`, read digits as:
```go
digits := r.FormValue("pin_input")
if digits == "" {
    digits = r.FormValue("menu_input")
}
```

**Step 3: Check `internal/handler/status.go`**

Read the file — if it imports twiml, update to fsxml. The status handler cleans up sessions and likely just returns 200 OK, so it may not use twiml at all.

**Step 4: Update `cmd/respond/main.go`**

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/config"
	"github.com/mattventura/respond/internal/db"
	"github.com/mattventura/respond/internal/handler"
	"github.com/mattventura/respond/internal/middleware"
	"github.com/mattventura/respond/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	responders := store.NewResponderStore(pool)
	sessions := store.NewSessionStore(pool)

	if len(cfg.BootstrapAdmins) > 0 {
		count, err := responders.CountAdmins(ctx)
		if err != nil {
			log.Fatalf("bootstrap: count admins: %v", err)
		}
		if count == 0 {
			for _, a := range cfg.BootstrapAdmins {
				if err := responders.CreateAdmin(ctx, a.Phone, a.PIN); err != nil {
					log.Fatalf("bootstrap: create admin %s: %v", a.Phone, err)
				}
				log.Printf("bootstrap: created admin %s", a.Phone)
			}
		}
	}

	voiceHandler := &handler.VoiceHandler{
		Responders: responders,
		Sessions:   sessions,
		BaseURL:    cfg.BaseURL,
	}
	gatherHandler := &handler.GatherHandler{
		Responders: responders,
		Sessions:   sessions,
		BaseURL:    cfg.BaseURL,
	}
	statusHandler := &handler.StatusHandler{Sessions: sessions}

	fsMW := func(h http.Handler) http.Handler {
		return middleware.FSAuth(cfg.FSSharedSecret, h)
	}

	mux := http.NewServeMux()
	mux.Handle("/fs/voice", fsMW(voiceHandler))
	mux.Handle("/fs/gather", fsMW(gatherHandler))
	mux.Handle("/fs/status", fsMW(statusHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

**Step 5: Delete old files**

```bash
rm internal/twiml/twiml.go
rm internal/middleware/twilio_auth.go
```

**Step 6: Verify build**

```bash
go build ./...
```
Expected: clean build, no errors.

**Step 7: Run all tests**

```bash
go test ./...
```
Expected: all PASS.

**Step 8: Commit**

```bash
git add -A
git commit -m "feat: migrate voice handlers from TwiML to FreeSWITCH XML"
```

---

## Task 5: Add SMS session store

**Files:**
- Create: `migrations/007_sms_sessions.sql`
- Create: `internal/store/sms_session.go`
- Create: `internal/store/sms_session_test.go`

**Step 1: Create migration**

Create `migrations/007_sms_sessions.sql`:

```sql
CREATE TABLE sms_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number TEXT NOT NULL UNIQUE,
    current_node TEXT NOT NULL DEFAULT 'root',
    last_activity TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Step 2: Write the failing tests**

Create `internal/store/sms_session_test.go`:

```go
package store_test

// Integration tests — require DATABASE_URL env var pointing to a test DB.
// Run with: DATABASE_URL=postgres://... go test ./internal/store/... -run TestSMSSession

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mattventura/respond/internal/store"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestSMSSessionUpsertAndGet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM sms_sessions WHERE phone_number = '+15005550001'")

	s := store.NewSMSSessionStore(pool)

	// Create
	if err := s.Upsert(ctx, "+15005550001", "node_a"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	node, err := s.GetNode(ctx, "+15005550001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if node != "node_a" {
		t.Errorf("expected node_a, got %s", node)
	}

	// Update
	if err := s.Upsert(ctx, "+15005550001", "node_b"); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	node, err = s.GetNode(ctx, "+15005550001")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if node != "node_b" {
		t.Errorf("expected node_b after update, got %s", node)
	}
}

func TestSMSSessionDelete(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM sms_sessions WHERE phone_number = '+15005550002'")

	s := store.NewSMSSessionStore(pool)
	s.Upsert(ctx, "+15005550002", "root")
	s.Delete(ctx, "+15005550002")

	_, err := s.GetNode(ctx, "+15005550002")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestSMSSessionDeleteExpired(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM sms_sessions WHERE phone_number IN ('+15005550003', '+15005550004')")

	s := store.NewSMSSessionStore(pool)
	s.Upsert(ctx, "+15005550003", "root")
	s.Upsert(ctx, "+15005550004", "root")

	// Backdate one session
	pool.Exec(ctx, "UPDATE sms_sessions SET last_activity = NOW() - INTERVAL '2 hours' WHERE phone_number = '+15005550003'")

	deleted, err := s.DeleteExpired(ctx, 30*time.Minute)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// +15005550004 should still exist
	node, err := s.GetNode(ctx, "+15005550004")
	if err != nil || node != "root" {
		t.Errorf("active session should remain: node=%s err=%v", node, err)
	}
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/store/... -run TestSMSSession -v
```
Expected: compile error — `SMSSessionStore` not defined.

**Step 4: Implement `internal/store/sms_session.go`**

```go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SMSSessionStore struct{ db *pgxpool.Pool }

func NewSMSSessionStore(db *pgxpool.Pool) *SMSSessionStore {
	return &SMSSessionStore{db: db}
}

func (s *SMSSessionStore) GetNode(ctx context.Context, phone string) (string, error) {
	var node string
	err := s.db.QueryRow(ctx,
		`SELECT current_node FROM sms_sessions WHERE phone_number=$1`, phone,
	).Scan(&node)
	if err != nil {
		return "", fmt.Errorf("get sms session: %w", err)
	}
	return node, nil
}

func (s *SMSSessionStore) Upsert(ctx context.Context, phone, node string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO sms_sessions (phone_number, current_node, last_activity)
		VALUES ($1, $2, NOW())
		ON CONFLICT (phone_number) DO UPDATE
		SET current_node=$2, last_activity=NOW()
	`, phone, node)
	return err
}

func (s *SMSSessionStore) Delete(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sms_sessions WHERE phone_number=$1`, phone)
	return err
}

// DeleteExpired removes sessions inactive for longer than timeout. Returns count deleted.
func (s *SMSSessionStore) DeleteExpired(ctx context.Context, timeout time.Duration) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM sms_sessions WHERE last_activity < NOW() - $1::interval`,
		fmt.Sprintf("%d seconds", int(timeout.Seconds())),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

**Step 5: Run tests**

```bash
DATABASE_URL=postgres://localhost/respond_test go test ./internal/store/... -run TestSMSSession -v
```
Expected: all PASS (requires test DB with migration 007 applied).

**Step 6: Commit**

```bash
git add migrations/007_sms_sessions.sql internal/store/sms_session.go internal/store/sms_session_test.go
git commit -m "feat: add SMS session store and migration"
```

---

## Task 6: SMS decision tree — config loader and engine

**Files:**
- Create: `internal/sms/tree.go`
- Create: `internal/sms/tree_test.go`
- Create: `internal/sms/engine.go`
- Create: `internal/sms/engine_test.go`

**Step 1: Write tree loader tests**

Create `internal/sms/tree_test.go`:

```go
package sms_test

import (
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/sms"
)

const validYAML = `
greeting: "Hello! Press 1 or 2."
timeout_minutes: 30
nodes:
  root:
    prompt: "Press 1 for A, 2 for B."
    options:
      "1": node_a
      "2": node_b
  node_a:
    response: "You chose A."
  node_b:
    prompt: "Urgent? Y or N."
    options:
      Y: node_b_yes
      N: node_b_no
  node_b_yes:
    response: "Connecting you now."
    action: notify_responders
  node_b_no:
    response: "Email us."
`

func TestLoadTree_Valid(t *testing.T) {
	tree, err := sms.LoadTree(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Greeting != "Hello! Press 1 or 2." {
		t.Errorf("wrong greeting: %s", tree.Greeting)
	}
	if tree.TimeoutMinutes != 30 {
		t.Errorf("wrong timeout: %d", tree.TimeoutMinutes)
	}
	root, ok := tree.Nodes["root"]
	if !ok {
		t.Fatal("root node missing")
	}
	if root.Prompt != "Press 1 for A, 2 for B." {
		t.Errorf("wrong root prompt: %s", root.Prompt)
	}
	if root.Options["1"] != "node_a" {
		t.Errorf("wrong option 1: %s", root.Options["1"])
	}
}

func TestLoadTree_MissingNode(t *testing.T) {
	bad := strings.ReplaceAll(validYAML, "node_a:", "node_x:")
	_, err := sms.LoadTree(strings.NewReader(bad))
	if err == nil {
		t.Error("expected error for missing node reference, got nil")
	}
}

func TestLoadTree_BranchNodeMissingOptions(t *testing.T) {
	bad := `
greeting: hi
timeout_minutes: 30
nodes:
  root:
    prompt: "Choose"
`
	_, err := sms.LoadTree(strings.NewReader(bad))
	if err == nil {
		t.Error("expected error for branch node with no options")
	}
}
```

**Step 2: Write engine tests**

Create `internal/sms/engine_test.go`:

```go
package sms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/sms"
)

type mockSMSStore struct {
	nodes map[string]string
}

func (m *mockSMSStore) GetNode(ctx context.Context, phone string) (string, error) {
	if n, ok := m.nodes[phone]; ok {
		return n, nil
	}
	return "", fmt.Errorf("not found")
}
func (m *mockSMSStore) Upsert(ctx context.Context, phone, node string) error {
	m.nodes[phone] = node
	return nil
}
func (m *mockSMSStore) Delete(ctx context.Context, phone string) error {
	delete(m.nodes, phone)
	return nil
}

func newTestEngine(t *testing.T) *sms.Engine {
	t.Helper()
	tree, err := sms.LoadTree(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	store := &mockSMSStore{nodes: map[string]string{}}
	return sms.NewEngine(tree, store)
}

func TestEngine_NewSession(t *testing.T) {
	e := newTestEngine(t)
	resp, err := e.Handle(context.Background(), "+15005550001", "hi")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	// New session — should return root prompt
	if !strings.Contains(resp.Message, "Press 1 for A") {
		t.Errorf("expected root prompt, got: %s", resp.Message)
	}
}

func TestEngine_ValidOption(t *testing.T) {
	e := newTestEngine(t)
	e.Handle(context.Background(), "+15005550001", "hi") // start session at root
	resp, err := e.Handle(context.Background(), "+15005550001", "1")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.Message != "You chose A." {
		t.Errorf("expected terminal response, got: %s", resp.Message)
	}
	if resp.Terminal != true {
		t.Error("expected terminal=true")
	}
}

func TestEngine_InvalidOption(t *testing.T) {
	e := newTestEngine(t)
	e.Handle(context.Background(), "+15005550001", "hi")
	resp, err := e.Handle(context.Background(), "+15005550001", "9")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	// Should repeat prompt with error prefix
	if !strings.Contains(resp.Message, "Press 1 for A") {
		t.Errorf("expected repeated prompt, got: %s", resp.Message)
	}
	if resp.Terminal {
		t.Error("expected terminal=false for invalid input")
	}
}

func TestEngine_NotifyAction(t *testing.T) {
	e := newTestEngine(t)
	e.Handle(context.Background(), "+15005550001", "hi")
	e.Handle(context.Background(), "+15005550001", "2") // → node_b prompt
	resp, _ := e.Handle(context.Background(), "+15005550001", "Y") // → node_b_yes
	if resp.Action != "notify_responders" {
		t.Errorf("expected notify_responders action, got: %s", resp.Action)
	}
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/sms/... -v
```
Expected: compile error — package not found.

**Step 4: Implement `internal/sms/tree.go`**

```go
package sms

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type Node struct {
	Prompt   string            `yaml:"prompt"`
	Options  map[string]string `yaml:"options"`
	Response string            `yaml:"response"`
	Action   string            `yaml:"action"`
}

func (n *Node) IsTerminal() bool {
	return n.Response != ""
}

type Tree struct {
	Greeting       string          `yaml:"greeting"`
	TimeoutMinutes int             `yaml:"timeout_minutes"`
	Nodes          map[string]Node `yaml:"nodes"`
}

func LoadTree(r io.Reader) (*Tree, error) {
	var tree Tree
	if err := yaml.NewDecoder(r).Decode(&tree); err != nil {
		return nil, fmt.Errorf("parse tree: %w", err)
	}
	if err := validate(&tree); err != nil {
		return nil, err
	}
	return &tree, nil
}

func validate(tree *Tree) error {
	for name, node := range tree.Nodes {
		if node.IsTerminal() {
			continue
		}
		// Branch node — must have options, all options must reference existing nodes
		if len(node.Options) == 0 {
			return fmt.Errorf("node %q has no response and no options", name)
		}
		for opt, target := range node.Options {
			if _, ok := tree.Nodes[target]; !ok {
				return fmt.Errorf("node %q option %q references unknown node %q", name, opt, target)
			}
		}
	}
	if _, ok := tree.Nodes["root"]; !ok {
		return fmt.Errorf("tree must have a 'root' node")
	}
	return nil
}
```

**Step 5: Implement `internal/sms/engine.go`**

```go
package sms

import (
	"context"
	"fmt"
	"strings"
)

type SMSStore interface {
	GetNode(ctx context.Context, phone string) (string, error)
	Upsert(ctx context.Context, phone, node string) error
	Delete(ctx context.Context, phone string) error
}

type Response struct {
	Message  string
	Terminal bool
	Action   string
}

type Engine struct {
	tree  *Tree
	store SMSStore
}

func NewEngine(tree *Tree, store SMSStore) *Engine {
	return &Engine{tree: tree, store: store}
}

// Handle processes an inbound SMS from phone with body text.
// Returns the response to send back.
func (e *Engine) Handle(ctx context.Context, phone, body string) (*Response, error) {
	currentNode, err := e.store.GetNode(ctx, phone)
	if err != nil {
		// No session — start at root, send greeting + root prompt
		if err := e.store.Upsert(ctx, phone, "root"); err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		root := e.tree.Nodes["root"]
		return &Response{Message: e.tree.Greeting + "\n" + root.Prompt}, nil
	}

	node, ok := e.tree.Nodes[currentNode]
	if !ok {
		// Corrupt state — reset
		e.store.Delete(ctx, phone)
		return &Response{Message: "Sorry, something went wrong. " + e.tree.Greeting}, nil
	}

	if node.IsTerminal() {
		// Already at terminal — reset and re-greet
		e.store.Delete(ctx, phone)
		root := e.tree.Nodes["root"]
		e.store.Upsert(ctx, phone, "root")
		return &Response{Message: e.tree.Greeting + "\n" + root.Prompt}, nil
	}

	input := strings.TrimSpace(strings.ToUpper(body))
	nextKey, ok := node.Options[input]
	if !ok {
		// Also try lowercase key matching
		for k, v := range node.Options {
			if strings.EqualFold(k, input) {
				nextKey = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return &Response{Message: "Sorry, I didn't understand that.\n" + node.Prompt}, nil
	}

	next := e.tree.Nodes[nextKey]
	if next.IsTerminal() {
		e.store.Delete(ctx, phone)
		return &Response{
			Message:  next.Response,
			Terminal: true,
			Action:   next.Action,
		}, nil
	}

	e.store.Upsert(ctx, phone, nextKey)
	return &Response{Message: next.Prompt}, nil
}
```

**Step 6: Add yaml.v3 dependency**

```bash
go get gopkg.in/yaml.v3
go mod tidy
```

**Step 7: Fix the mock in engine_test.go**

The mock needs the `fmt` import. Add `"fmt"` to the import list in `engine_test.go`.

**Step 8: Run all tests**

```bash
go test ./internal/sms/... -v
```
Expected: all PASS.

**Step 9: Commit**

```bash
git add internal/sms/ go.mod go.sum
git commit -m "feat: add SMS decision tree loader and conversation engine"
```

---

## Task 7: VoIP.ms SMS client and inbound handler

**Files:**
- Create: `internal/voipms/client.go`
- Create: `internal/voipms/client_test.go`
- Create: `internal/handler/sms.go`
- Modify: `cmd/respond/main.go`

**Step 1: Write client test**

Create `internal/voipms/client_test.go`:

```go
package voipms_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattventura/respond/internal/voipms"
)

func TestSendSMS(t *testing.T) {
	var gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		r.ParseForm()
		gotBody = r.FormValue("dst")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	c := voipms.NewClient("user", "pass", "5005550000", srv.URL)
	err := c.SendSMS(nil, "+15005551234", "Hello")
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("expected GET, got %s", gotMethod)
	}
	if gotBody != "15005551234" {
		t.Errorf("expected dst=15005551234, got %s", gotBody)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/voipms/... -v
```
Expected: compile error.

**Step 3: Implement `internal/voipms/client.go`**

VoIP.ms SMS API sends outbound SMS via a GET request with query params.

```go
package voipms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	username string
	password string
	did      string
	baseURL  string
}

func NewClient(username, password, did, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://voip.ms/api/v1/rest.php"
	}
	return &Client{username: username, password: password, did: did, baseURL: baseURL}
}

// SendSMS sends an outbound SMS via the VoIP.ms REST API.
func (c *Client) SendSMS(ctx context.Context, to, message string) error {
	// Strip leading + for VoIP.ms API
	dst := strings.TrimPrefix(to, "+")

	params := url.Values{
		"api_username": {c.username},
		"api_password": {c.password},
		"method":       {"sendSMS"},
		"did":          {c.did},
		"dst":          {dst},
		"message":      {message},
	}

	reqURL := c.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send sms: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("voipms api: status %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), `"status":"invalid`) {
		return fmt.Errorf("voipms api error: %s", body)
	}
	return nil
}
```

**Step 4: Implement `internal/handler/sms.go`**

```go
package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/sms"
	"github.com/mattventura/respond/internal/store"
)

type SMSSender interface {
	SendSMS(ctx context.Context, to, message string) error
}

type SMSHandler struct {
	Engine     *sms.Engine
	Sender     SMSSender
	Responders *store.ResponderStore
}

func (h *SMSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// VoIP.ms sends: from, to, message
	from := r.FormValue("from")
	body := r.FormValue("message")
	ctx := r.Context()

	log.Printf("[sms] from=%s body=%q", from, body)

	resp, err := h.Engine.Handle(ctx, from, body)
	if err != nil {
		log.Printf("[sms] engine error: %v", err)
		w.WriteHeader(http.StatusOK) // Always 200 to VoIP.ms
		return
	}

	if err := h.Sender.SendSMS(ctx, from, resp.Message); err != nil {
		log.Printf("[sms] send reply: %v", err)
	}

	if resp.Action == "notify_responders" {
		h.notifyResponders(ctx, from)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *SMSHandler) notifyResponders(ctx context.Context, customerPhone string) {
	available, err := h.Responders.ListAvailable(ctx)
	if err != nil {
		log.Printf("[sms] list available for notify: %v", err)
		return
	}
	msg := "On-call request from " + customerPhone + ". Please call them back."
	for _, r := range available {
		if err := h.Sender.SendSMS(ctx, r.PhoneNumber, msg); err != nil {
			log.Printf("[sms] notify responder %s: %v", r.PhoneNumber, err)
		}
	}
}
```

**Step 5: Wire SMS into `main.go`**

Add to `cmd/respond/main.go` after existing store setup:

```go
// SMS tree
treeFile, err := os.Open(cfg.SMSTreePath)
if err != nil {
    log.Fatalf("open sms tree: %v", err)
}
tree, err := sms.LoadTree(treeFile)
treeFile.Close()
if err != nil {
    log.Fatalf("load sms tree: %v", err)
}

smsStore := store.NewSMSSessionStore(pool)
voipmsClient := voipms.NewClient(cfg.VoIPMSUsername, cfg.VoIPMSPassword, cfg.VoIPMSDID, "")
smsEngine := sms.NewEngine(tree, smsStore)
smsHandler := &handler.SMSHandler{
    Engine:     smsEngine,
    Sender:     voipmsClient,
    Responders: responders,
}
```

Add import: `"os"`, `"github.com/mattventura/respond/internal/sms"`, `"github.com/mattventura/respond/internal/voipms"`

Add route (no auth — VoIP.ms webhook, validated by IP allowlist at infrastructure level):
```go
mux.Handle("/sms/inbound", smsHandler)
```

**Step 6: Build and test**

```bash
go build ./...
go test ./...
```
Expected: all pass.

**Step 7: Commit**

```bash
git add internal/voipms/ internal/handler/sms.go cmd/respond/main.go
git commit -m "feat: add VoIP.ms SMS client and inbound SMS handler"
```

---

## Task 8: FreeSWITCH configuration files

These are configuration files for the FreeSWITCH VPS — not Go code. Store them in the repo under `freeswitch/` for reference and deployment.

**Files:**
- Create: `freeswitch/sip_profiles/voipms.xml`
- Create: `freeswitch/dialplan/respond.xml`
- Create: `freeswitch/autoload_configs/xml_curl.conf.xml`
- Create: `freeswitch/README.md`

**Step 1: Create VoIP.ms SIP profile**

Create `freeswitch/sip_profiles/voipms.xml`:

```xml
<include>
  <gateway name="voipms">
    <param name="username" value="YOUR_VOIPMS_USERNAME"/>
    <param name="password" value="YOUR_VOIPMS_PASSWORD"/>
    <param name="proxy" value="denver.voip.ms"/>
    <param name="register" value="true"/>
    <param name="caller-id-in-from" value="true"/>
  </gateway>
</include>
```

Replace `denver.voip.ms` with your nearest VoIP.ms POP. Options: `atlanta`, `chicago`, `dallas`, `denver`, `losangeles`, `montreal`, `newyork`, `seattle`, `toronto`, `vancouver`.

**Step 2: Create mod_xml_curl config**

Create `freeswitch/autoload_configs/xml_curl.conf.xml`:

```xml
<configuration name="xml_curl.conf" description="XML Curl">
  <bindings>
    <binding name="respond-dialplan">
      <param name="gateway-url" value="https://YOUR_APP_URL/fs/voice"/>
      <param name="bindings" value="dialplan"/>
      <param name="method" value="POST"/>
      <param name="disable-100-continue" value="true"/>
      <param name="timeout" value="10000"/>
      <!-- Shared secret sent as custom header -->
      <param name="enable-cacert-check" value="true"/>
    </binding>
  </bindings>
</configuration>
```

Note: FreeSWITCH's `mod_xml_curl` does not natively support custom headers in its config. To send `X-FS-Secret`, use a `<param name="url-params">` workaround or configure a Lua/ESL event socket bridge. The simpler alternative: validate by source IP (FreeSWITCH server IP) in the Go middleware instead of a header secret. Update `FSAuth` middleware to also accept IP-based validation if needed.

**Step 3: Create dialplan**

Create `freeswitch/dialplan/respond.xml`:

```xml
<include>
  <context name="public">
    <extension name="inbound-respond">
      <condition field="destination_number" expression="^\+?1?(\d{10})$">
        <!-- mod_xml_curl will fetch dialplan from the app -->
        <!-- This extension is the catch-all for inbound calls via VoIP.ms trunk -->
        <action application="set" data="hangup_after_bridge=true"/>
        <action application="answer"/>
        <action application="xml_curl" data="https://YOUR_APP_URL/fs/voice"/>
      </condition>
    </extension>
  </context>
</include>
```

**Step 4: Create README**

Create `freeswitch/README.md` documenting:
- Which files go where on the FreeSWITCH VPS (`/etc/freeswitch/`)
- How to reload config: `fs_cli -x "reloadxml"`
- How to reload SIP profile: `fs_cli -x "sofia profile voipms rescan"`
- Module requirements: `mod_xml_curl`, `mod_flite` (TTS), `mod_dptools`
- VoIP.ms inbound DID configuration: point DID to SIP URI `sip:YOUR_DID@YOUR_VPS_IP`

**Step 5: Commit**

```bash
git add freeswitch/
git commit -m "docs: add FreeSWITCH configuration files for VoIP.ms trunk"
```

---

## Task 9: Final cleanup and verification

**Step 1: Remove all Twilio references**

```bash
grep -r "twilio\|Twilio\|twiml" --include="*.go" .
```
Expected: zero results (only comments in git history).

**Step 2: Run full test suite**

```bash
go test ./... -v
```
Expected: all PASS.

**Step 3: Build final binary**

```bash
go build ./cmd/respond/
```
Expected: clean build.

**Step 4: Update Kubernetes manifests**

In your Helm chart / K8s manifests:
- Remove `TWILIO_AUTH_TOKEN` from Secret
- Add `FS_SHARED_SECRET`, `VOIPMS_USERNAME`, `VOIPMS_PASSWORD`, `VOIPMS_DID`
- Add ConfigMap for `sms-tree.yaml` and mount at `/config/sms-tree.yaml`
- Update any references to `/twilio/*` routes in Ingress to `/fs/*` and `/sms/*`

**Step 5: Final commit**

```bash
git add -A
git commit -m "chore: final cleanup and K8s manifest updates for FreeSWITCH migration"
```
