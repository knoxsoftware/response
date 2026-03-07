# Respond Service Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go/Twilio service that dispatches calls to available responders, lets responders toggle availability, and lets admins manage the list via a phone menu.

**Architecture:** Single Go HTTP service with three inbound call flows (dispatch, responder self-service, admin menu) routed by caller ID lookup. Session state for multi-step DTMF flows is stored in PostgreSQL keyed by Twilio CallSid. A Helm chart handles Kubernetes deployment with CloudNativePG for the database.

**Tech Stack:** Go 1.22+, `github.com/twilio/twilio-go`, `github.com/jackc/pgx/v5`, `golang.org/x/crypto/bcrypt`, CloudNativePG, Helm 3

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `go.sum` (generated)
- Create: `cmd/respond/main.go`
- Create: `internal/config/config.go`
- Create: `.gitignore`

**Step 1: Initialize Go module**

```bash
go mod init github.com/mattventura/respond
```

**Step 2: Create `.gitignore`**

```
respond
*.env
.env
```

**Step 3: Create `internal/config/config.go`**

```go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL    string
	TwilioAuthToken string
	Port           string
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		TwilioAuthToken: os.Getenv("TWILIO_AUTH_TOKEN"),
		Port:            os.Getenv("PORT"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.TwilioAuthToken == "" {
		return nil, fmt.Errorf("TWILIO_AUTH_TOKEN is required")
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	return c, nil
}
```

**Step 4: Create `cmd/respond/main.go`**

```go
package main

import (
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	_ = cfg
	mux := http.NewServeMux()
	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

**Step 5: Build to verify it compiles**

```bash
go build ./...
```
Expected: no errors.

**Step 6: Commit**

```bash
git add go.mod cmd/ internal/ .gitignore
git commit -m "feat: initial project scaffold"
```

---

## Task 2: Database Schema & Migrations

**Files:**
- Create: `internal/db/db.go`
- Create: `migrations/001_initial.sql`
- Create: `cmd/migrate/main.go`

**Step 1: Write `migrations/001_initial.sql`**

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS responders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    available BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admins (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    pin_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS call_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    call_sid TEXT NOT NULL UNIQUE,
    caller TEXT NOT NULL,
    session_state JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Step 2: Add pgx dependency**

```bash
go get github.com/jackc/pgx/v5
```

**Step 3: Create `internal/db/db.go`**

```go
package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		filename TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		var applied bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename=$1)`, f,
		).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", f, err)
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile(filepath.Join(migrationsDir, f))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply migration %s: %w", f, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, f,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", f, err)
		}
	}
	return nil
}
```

**Step 4: Create `cmd/migrate/main.go`**

```go
package main

import (
	"context"
	"log"

	"github.com/mattventura/respond/internal/config"
	"github.com/mattventura/respond/internal/db"
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
	if err := db.Migrate(ctx, pool, "migrations"); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied")
}
```

**Step 5: Build to verify**

```bash
go build ./...
```

**Step 6: Commit**

```bash
git add migrations/ internal/db/ cmd/migrate/ go.mod go.sum
git commit -m "feat: database schema and migration runner"
```

---

## Task 3: Repository Layer (Responders & Admins)

**Files:**
- Create: `internal/store/responder.go`
- Create: `internal/store/admin.go`
- Create: `internal/store/session.go`

**Step 1: Add bcrypt dependency**

```bash
go get golang.org/x/crypto
```

**Step 2: Create `internal/store/responder.go`**

```go
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Responder struct {
	ID          string
	PhoneNumber string
	Name        string
	Available   bool
}

type ResponderStore struct{ db *pgxpool.Pool }

func NewResponderStore(db *pgxpool.Pool) *ResponderStore { return &ResponderStore{db: db} }

func (s *ResponderStore) FindByPhone(ctx context.Context, phone string) (*Responder, error) {
	r := &Responder{}
	err := s.db.QueryRow(ctx,
		`SELECT id, phone_number, name, available FROM responders WHERE phone_number=$1`, phone,
	).Scan(&r.ID, &r.PhoneNumber, &r.Name, &r.Available)
	if err != nil {
		return nil, fmt.Errorf("find responder: %w", err)
	}
	return r, nil
}

func (s *ResponderStore) ListAvailable(ctx context.Context) ([]Responder, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, phone_number, name, available FROM responders WHERE available=TRUE ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list available: %w", err)
	}
	defer rows.Close()
	var out []Responder
	for rows.Next() {
		var r Responder
		if err := rows.Scan(&r.ID, &r.PhoneNumber, &r.Name, &r.Available); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ResponderStore) ListAll(ctx context.Context) ([]Responder, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, phone_number, name, available FROM responders ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all: %w", err)
	}
	defer rows.Close()
	var out []Responder
	for rows.Next() {
		var r Responder
		if err := rows.Scan(&r.ID, &r.PhoneNumber, &r.Name, &r.Available); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ResponderStore) Create(ctx context.Context, phone, name string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO responders (phone_number, name) VALUES ($1, $2)`, phone, name,
	)
	return err
}

func (s *ResponderStore) Delete(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM responders WHERE phone_number=$1`, phone)
	return err
}

func (s *ResponderStore) SetAvailable(ctx context.Context, phone string, available bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE responders SET available=$1 WHERE phone_number=$2`, available, phone,
	)
	return err
}

func (s *ResponderStore) ToggleAvailable(ctx context.Context, phone string) (bool, error) {
	var newState bool
	err := s.db.QueryRow(ctx,
		`UPDATE responders SET available=NOT available WHERE phone_number=$1 RETURNING available`, phone,
	).Scan(&newState)
	return newState, err
}
```

**Step 3: Create `internal/store/admin.go`**

```go
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Admin struct {
	ID          string
	PhoneNumber string
	Name        string
	PinHash     string
}

type AdminStore struct{ db *pgxpool.Pool }

func NewAdminStore(db *pgxpool.Pool) *AdminStore { return &AdminStore{db: db} }

func (s *AdminStore) FindByPhone(ctx context.Context, phone string) (*Admin, error) {
	a := &Admin{}
	err := s.db.QueryRow(ctx,
		`SELECT id, phone_number, name, pin_hash FROM admins WHERE phone_number=$1`, phone,
	).Scan(&a.ID, &a.PhoneNumber, &a.Name, &a.PinHash)
	if err != nil {
		return nil, fmt.Errorf("find admin: %w", err)
	}
	return a, nil
}

func (s *AdminStore) Create(ctx context.Context, phone, name, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO admins (phone_number, name, pin_hash) VALUES ($1, $2, $3)`,
		phone, name, string(hash),
	)
	return err
}

func (a *Admin) VerifyPIN(pin string) bool {
	return bcrypt.CompareHashAndPassword([]byte(a.PinHash), []byte(pin)) == nil
}
```

**Step 4: Create `internal/store/session.go`**

```go
package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionState struct {
	Step    string         `json:"step"`
	Pending map[string]string `json:"pending,omitempty"`
}

type Session struct {
	CallSid string
	Caller  string
	State   SessionState
}

type SessionStore struct{ db *pgxpool.Pool }

func NewSessionStore(db *pgxpool.Pool) *SessionStore { return &SessionStore{db: db} }

func (s *SessionStore) Get(ctx context.Context, callSid string) (*Session, error) {
	var raw []byte
	sess := &Session{CallSid: callSid}
	err := s.db.QueryRow(ctx,
		`SELECT caller, session_state FROM call_sessions WHERE call_sid=$1`, callSid,
	).Scan(&sess.Caller, &raw)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if err := json.Unmarshal(raw, &sess.State); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return sess, nil
}

func (s *SessionStore) Upsert(ctx context.Context, sess *Session) error {
	raw, err := json.Marshal(sess.State)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO call_sessions (call_sid, caller, session_state)
		VALUES ($1, $2, $3)
		ON CONFLICT (call_sid) DO UPDATE
		SET session_state=$3
	`, sess.CallSid, sess.Caller, raw)
	return err
}

func (s *SessionStore) Delete(ctx context.Context, callSid string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM call_sessions WHERE call_sid=$1`, callSid)
	return err
}
```

**Step 5: Build to verify**

```bash
go build ./...
```

**Step 6: Commit**

```bash
git add internal/store/ go.mod go.sum
git commit -m "feat: store layer for responders, admins, and sessions"
```

---

## Task 4: TwiML Helpers

**Files:**
- Create: `internal/twiml/twiml.go`

Twilio's Go SDK includes TwiML builders. We'll use it directly rather than building raw XML.

**Step 1: Add Twilio SDK dependency**

```bash
go get github.com/twilio/twilio-go
```

**Step 2: Create `internal/twiml/twiml.go`**

```go
package twiml

import (
	"fmt"
	"strings"
)

// Say returns a TwiML <Response><Say> block.
func Say(msg string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Response><Say>%s</Say></Response>`, xmlEscape(msg))
}

// Gather returns a TwiML response prompting for DTMF input that POSTs to action.
func Gather(msg, action string, numDigits int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Response><Gather numDigits="%d" action="%s" method="POST"><Say>%s</Say></Gather></Response>`,
		numDigits, xmlEscape(action), xmlEscape(msg))
}

// Dial returns a TwiML response dialing all numbers simultaneously.
func Dial(numbers []string) string {
	if len(numbers) == 0 {
		return Say("There are no available responders at this time. Please try again later.")
	}
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response><Dial>`)
	for _, n := range numbers {
		sb.WriteString(fmt.Sprintf(`<Number>%s</Number>`, xmlEscape(n)))
	}
	sb.WriteString(`</Dial></Response>`)
	return sb.String()
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
```

**Step 3: Build to verify**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add internal/twiml/ go.mod go.sum
git commit -m "feat: TwiML helper functions"
```

---

## Task 5: Twilio Signature Validation Middleware

**Files:**
- Create: `internal/middleware/twilio_auth.go`

**Step 1: Create `internal/middleware/twilio_auth.go`**

Twilio signs requests with HMAC-SHA1. We validate using the auth token.

```go
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
// authToken is your Twilio Auth Token.
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

	// Build the string to sign: URL + sorted POST params
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
```

**Step 2: Build to verify**

```bash
go build ./...
```

**Step 3: Commit**

```bash
git add internal/middleware/
git commit -m "feat: Twilio signature validation middleware"
```

---

## Task 6: Call Handler — Dispatch Flow

**Files:**
- Create: `internal/handler/voice.go`

This is the entry point for all inbound calls. This task implements the routing logic and the dispatch (unknown caller) flow.

**Step 1: Create `internal/handler/voice.go`**

```go
package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/mattventura/respond/internal/store"
	"github.com/mattventura/respond/internal/twiml"
)

type VoiceHandler struct {
	Responders *store.ResponderStore
	Admins     *store.AdminStore
	Sessions   *store.SessionStore
	BaseURL    string // e.g. https://respond.example.com
}

func (h *VoiceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	from := r.FormValue("From")
	callSid := r.FormValue("CallSid")
	ctx := r.Context()

	w.Header().Set("Content-Type", "application/xml")

	// Lookup caller
	admin, adminErr := h.Admins.FindByPhone(ctx, from)
	responder, responderErr := h.Responders.FindByPhone(ctx, from)

	switch {
	case adminErr == nil && admin != nil:
		h.startAdminFlow(w, r, ctx, admin, callSid)
	case responderErr == nil && responder != nil:
		h.startResponderFlow(w, r, ctx, responder, callSid)
	default:
		h.dispatchFlow(w, ctx)
	}
}

func (h *VoiceHandler) dispatchFlow(w http.ResponseWriter, ctx context.Context) {
	available, err := h.Responders.ListAvailable(ctx)
	if err != nil {
		log.Printf("list available: %v", err)
		w.Write([]byte(twiml.Say("System error. Please try again.")))
		return
	}
	numbers := make([]string, len(available))
	for i, r := range available {
		numbers[i] = r.PhoneNumber
	}
	w.Write([]byte(twiml.Dial(numbers)))
}

func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	status := "unavailable"
	if resp.Available {
		status = "available"
	}
	msg := "You are currently " + status + ". Press 1 to toggle your availability, or press 2 to keep it as is."
	sess := &store.Session{
		CallSid: callSid,
		Caller:  resp.PhoneNumber,
		State:   store.SessionState{Step: "responder_toggle"},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather(msg, h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *VoiceHandler) startAdminFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, admin *store.Admin, callSid string) {
	sess := &store.Session{
		CallSid: callSid,
		Caller:  admin.PhoneNumber,
		State:   store.SessionState{Step: "admin_pin", Pending: map[string]string{}},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather("Welcome, "+admin.Name+". Please enter your PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
}
```

**Step 2: Wire handler into `cmd/respond/main.go`**

Replace the stub main.go:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

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

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		log.Fatal("BASE_URL is required")
	}

	responders := store.NewResponderStore(pool)
	admins := store.NewAdminStore(pool)
	sessions := store.NewSessionStore(pool)

	voiceHandler := &handler.VoiceHandler{
		Responders: responders,
		Admins:     admins,
		Sessions:   sessions,
		BaseURL:    baseURL,
	}

	gatherHandler := &handler.GatherHandler{
		Responders: responders,
		Admins:     admins,
		Sessions:   sessions,
		BaseURL:    baseURL,
	}

	statusHandler := &handler.StatusHandler{Sessions: sessions}

	mux := http.NewServeMux()
	twilioMW := func(h http.Handler) http.Handler {
		return middleware.TwilioAuth(cfg.TwilioAuthToken, h)
	}
	mux.Handle("/twilio/voice", twilioMW(voiceHandler))
	mux.Handle("/twilio/voice/gather", twilioMW(gatherHandler))
	mux.Handle("/twilio/status", twilioMW(statusHandler))

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

Also add `BASE_URL` to `internal/config/config.go`:

```go
// Add to Config struct:
BaseURL string

// Add to Load():
c.BaseURL = os.Getenv("BASE_URL")
if c.BaseURL == "" {
    return nil, fmt.Errorf("BASE_URL is required")
}
```

Remove the `os.Getenv("BASE_URL")` call from main.go after adding to config. Pass `cfg.BaseURL` to handlers.

**Step 3: Build (will fail until GatherHandler and StatusHandler stubs exist)**

Create stubs in `internal/handler/gather.go` and `internal/handler/status.go`:

`internal/handler/gather.go`:
```go
package handler

import (
	"net/http"

	"github.com/mattventura/respond/internal/store"
)

type GatherHandler struct {
	Responders *store.ResponderStore
	Admins     *store.AdminStore
	Sessions   *store.SessionStore
	BaseURL    string
}

func (h *GatherHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Say>Not implemented.</Say></Response>`))
}
```

`internal/handler/status.go`:
```go
package handler

import (
	"net/http"

	"github.com/mattventura/respond/internal/store"
)

type StatusHandler struct {
	Sessions *store.SessionStore
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 4: Build**

```bash
go build ./...
```
Expected: success.

**Step 5: Commit**

```bash
git add internal/handler/ cmd/respond/main.go internal/config/config.go
git commit -m "feat: voice entry handler with dispatch, responder, and admin routing"
```

---

## Task 7: Gather Handler — Responder Toggle & Admin PIN + Menu

**Files:**
- Modify: `internal/handler/gather.go`

**Step 1: Replace gather.go stub with full implementation**

```go
package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/mattventura/respond/internal/store"
	"github.com/mattventura/respond/internal/twiml"
)

type GatherHandler struct {
	Responders *store.ResponderStore
	Admins     *store.AdminStore
	Sessions   *store.SessionStore
	BaseURL    string
}

func (h *GatherHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	callSid := r.FormValue("CallSid")
	digits := r.FormValue("Digits")
	ctx := r.Context()

	w.Header().Set("Content-Type", "application/xml")

	sess, err := h.Sessions.Get(ctx, callSid)
	if err != nil {
		log.Printf("get session %s: %v", callSid, err)
		w.Write([]byte(twiml.Say("Session not found. Goodbye.")))
		return
	}

	switch sess.State.Step {
	case "responder_toggle":
		h.handleResponderToggle(w, r, ctx, sess, digits)
	case "admin_pin":
		h.handleAdminPIN(w, r, ctx, sess, digits)
	case "admin_menu":
		h.handleAdminMenu(w, r, ctx, sess, digits)
	case "admin_add_number":
		h.handleAdminAddNumber(w, r, ctx, sess, digits)
	case "admin_add_name":
		h.handleAdminAddName(w, r, ctx, sess, digits)
	case "admin_remove_number":
		h.handleAdminRemoveNumber(w, r, ctx, sess, digits)
	case "admin_change_number":
		h.handleAdminChangeAvailNumber(w, r, ctx, sess, digits)
	default:
		w.Write([]byte(twiml.Say("Unknown state. Goodbye.")))
	}
}

func (h *GatherHandler) handleResponderToggle(w http.ResponseWriter, r *http.Request, ctx interface{ Done() <-chan struct{} }, sess *store.Session, digits string) {
	// ctx is context.Context
	c := r.Context()
	if digits == "1" {
		newState, err := h.Responders.ToggleAvailable(c, sess.Caller)
		if err != nil {
			log.Printf("toggle: %v", err)
			w.Write([]byte(twiml.Say("Error updating availability. Goodbye.")))
			return
		}
		status := "unavailable"
		if newState {
			status = "available"
		}
		w.Write([]byte(twiml.Say("Your availability is now set to " + status + ". Goodbye.")))
	} else {
		w.Write([]byte(twiml.Say("No changes made. Goodbye.")))
	}
}

func (h *GatherHandler) handleAdminPIN(w http.ResponseWriter, r *http.Request, _ interface{}, sess *store.Session, digits string) {
	ctx := r.Context()
	admin, err := h.Admins.FindByPhone(ctx, sess.Caller)
	if err != nil || !admin.VerifyPIN(digits) {
		w.Write([]byte(twiml.Say("Incorrect PIN. Goodbye.")))
		return
	}
	sess.State.Step = "admin_menu"
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather(adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func adminMenuPrompt() string {
	return "Admin menu. Press 1 to add a responder. Press 2 to remove a responder. Press 3 to list all responders. Press 4 to change a responder's availability."
}

func (h *GatherHandler) handleAdminMenu(w http.ResponseWriter, r *http.Request, _ interface{}, sess *store.Session, digits string) {
	ctx := r.Context()
	switch digits {
	case "1":
		sess.State.Step = "admin_add_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the new responder, followed by pound.", h.BaseURL+"/twilio/voice/gather", 10)))
	case "2":
		sess.State.Step = "admin_remove_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the responder to remove, followed by pound.", h.BaseURL+"/twilio/voice/gather", 10)))
	case "3":
		responders, err := h.Responders.ListAll(ctx)
		if err != nil {
			w.Write([]byte(twiml.Say("Error retrieving list. Goodbye.")))
			return
		}
		if len(responders) == 0 {
			w.Write([]byte(twiml.Gather("No responders configured. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
			return
		}
		var parts []string
		for _, resp := range responders {
			status := "unavailable"
			if resp.Available {
				status = "available"
			}
			parts = append(parts, fmt.Sprintf("%s is %s", resp.Name, status))
		}
		msg := strings.Join(parts, ". ") + ". " + adminMenuPrompt()
		w.Write([]byte(twiml.Gather(msg, h.BaseURL+"/twilio/voice/gather", 1)))
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
	case "4":
		sess.State.Step = "admin_change_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the responder to update, followed by pound.", h.BaseURL+"/twilio/voice/gather", 10)))
	default:
		w.Write([]byte(twiml.Gather("Invalid selection. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
	}
}

func (h *GatherHandler) handleAdminAddNumber(w http.ResponseWriter, r *http.Request, _ interface{}, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	sess.State.Step = "admin_add_name"
	sess.State.Pending["phone"] = phone
	h.Sessions.Upsert(ctx, sess)
	// For name, use speech input via <Gather input="speech"> — simplified here with DTMF digits spelling
	// We'll use a 30-digit gather as a proxy; in practice, switch to speech input
	w.Write([]byte(twiml.Gather("Got it. This feature requires you to say the responder's name. For now, press any key followed by pound to confirm adding "+phone+" with a placeholder name, or hang up to cancel.", h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminAddName(w http.ResponseWriter, r *http.Request, _ interface{}, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := sess.State.Pending["phone"]
	name := "Responder " + phone[len(phone)-4:] // placeholder name from last 4 digits
	if err := h.Responders.Create(ctx, phone, name); err != nil {
		log.Printf("create responder: %v", err)
		w.Write([]byte(twiml.Gather("Error adding responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		return
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Responder "+phone+" added. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminRemoveNumber(w http.ResponseWriter, r *http.Request, _ interface{}, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	if err := h.Responders.Delete(ctx, phone); err != nil {
		log.Printf("delete responder: %v", err)
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Responder "+phone+" removed. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminChangeAvailNumber(w http.ResponseWriter, r *http.Request, _ interface{}, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	newState, err := h.Responders.ToggleAvailable(ctx, phone)
	if err != nil {
		log.Printf("toggle avail: %v", err)
		w.Write([]byte(twiml.Gather("Error updating responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		return
	}
	status := "unavailable"
	if newState {
		status = "available"
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(phone+" is now "+status+". "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

// normalizePhone converts a 10-digit DTMF string to E.164 (+1XXXXXXXXXX).
func normalizePhone(digits string) string {
	digits = strings.TrimSpace(digits)
	if strings.HasPrefix(digits, "+") {
		return digits
	}
	if len(digits) == 10 {
		return "+1" + digits
	}
	return "+" + digits
}
```

**Note on `handleResponderToggle` signature:** Fix the interface mismatch — use `r *http.Request` for context and remove the unused interface parameter. The ctx should come from `r.Context()`.

**Step 2: Build**

```bash
go build ./...
```
Fix any compile errors (unused imports, interface mismatches).

**Step 3: Commit**

```bash
git add internal/handler/gather.go
git commit -m "feat: gather handler for responder toggle and admin menu flows"
```

---

## Task 8: Status Callback Handler

**Files:**
- Modify: `internal/handler/status.go`

**Step 1: Replace status.go stub**

```go
package handler

import (
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/store"
)

type StatusHandler struct {
	Sessions *store.SessionStore
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	callSid := r.FormValue("CallSid")
	if callSid != "" {
		if err := h.Sessions.Delete(r.Context(), callSid); err != nil {
			log.Printf("delete session %s: %v", callSid, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 2: Build**

```bash
go build ./...
```

**Step 3: Commit**

```bash
git add internal/handler/status.go
git commit -m "feat: status callback handler cleans up call sessions"
```

---

## Task 9: Dockerfile

**Files:**
- Create: `Dockerfile`

**Step 1: Create `Dockerfile`**

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o respond ./cmd/respond

FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/respond /respond
COPY --from=builder /app/migrations /migrations
ENTRYPOINT ["/respond"]
```

**Step 2: Add `.dockerignore`**

```
.git
*.md
docs/
charts/
```

**Step 3: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "feat: multi-stage distroless Dockerfile"
```

---

## Task 10: Helm Chart

**Files:**
- Create: `charts/respond/Chart.yaml`
- Create: `charts/respond/values.yaml`
- Create: `charts/respond/templates/deployment.yaml`
- Create: `charts/respond/templates/service.yaml`
- Create: `charts/respond/templates/ingress.yaml`
- Create: `charts/respond/templates/secret.yaml`
- Create: `charts/respond/templates/serviceaccount.yaml`
- Create: `charts/respond/templates/migrate-job.yaml`
- Create: `charts/respond/templates/cnpg-cluster.yaml`
- Create: `charts/respond/templates/_helpers.tpl`

**Step 1: Create `charts/respond/Chart.yaml`**

```yaml
apiVersion: v2
name: respond
description: Twilio-based on-call responder service
version: 0.1.0
appVersion: "0.1.0"
```

**Step 2: Create `charts/respond/values.yaml`**

```yaml
replicaCount: 2

image:
  repository: ghcr.io/your-org/respond
  pullPolicy: IfNotPresent
  tag: ""

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: true
  className: nginx
  host: respond.example.com
  tls: []

config:
  baseUrl: "https://respond.example.com"
  port: "8080"

secrets:
  databaseUrl: ""
  twilioAuthToken: ""

serviceAccount:
  create: true
  name: ""

database:
  enabled: true
  instances: 2
  storage:
    size: 1Gi
    storageClass: ""
```

**Step 3: Create `charts/respond/templates/_helpers.tpl`**

```
{{- define "respond.name" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "respond.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "respond.name" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
```

**Step 4: Create `charts/respond/templates/secret.yaml`**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "respond.name" . }}
  labels:
    app: {{ include "respond.name" . }}
type: Opaque
stringData:
  DATABASE_URL: {{ .Values.secrets.databaseUrl | quote }}
  TWILIO_AUTH_TOKEN: {{ .Values.secrets.twilioAuthToken | quote }}
```

**Step 5: Create `charts/respond/templates/serviceaccount.yaml`**

```yaml
{{- if .Values.serviceAccount.create }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "respond.serviceAccountName" . }}
{{- end }}
```

**Step 6: Create `charts/respond/templates/cnpg-cluster.yaml`**

```yaml
{{- if .Values.database.enabled }}
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: {{ include "respond.name" . }}-db
spec:
  instances: {{ .Values.database.instances }}
  storage:
    size: {{ .Values.database.storage.size }}
    {{- if .Values.database.storage.storageClass }}
    storageClass: {{ .Values.database.storage.storageClass }}
    {{- end }}
{{- end }}
```

**Step 7: Create `charts/respond/templates/migrate-job.yaml`**

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "respond.name" . }}-migrate-{{ .Release.Revision }}
  annotations:
    "helm.sh/hook": pre-upgrade,pre-install
    "helm.sh/hook-delete-policy": before-hook-creation
spec:
  template:
    spec:
      serviceAccountName: {{ include "respond.serviceAccountName" . }}
      restartPolicy: OnFailure
      containers:
        - name: migrate
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          command: ["/migrate"]
          envFrom:
            - secretRef:
                name: {{ include "respond.name" . }}
          env:
            - name: BASE_URL
              value: {{ .Values.config.baseUrl | quote }}
            - name: PORT
              value: {{ .Values.config.port | quote }}
```

Note: The migrate binary needs a separate `cmd/migrate/main.go` entrypoint (already created in Task 2). The Dockerfile runs `respond`; add a second build step for `migrate` or package both binaries.

Update `Dockerfile` to also build the migrate binary:
```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -o respond ./cmd/respond && \
    CGO_ENABLED=0 GOOS=linux go build -o migrate ./cmd/migrate
```
And copy it:
```dockerfile
COPY --from=builder /app/migrate /migrate
```

**Step 8: Create `charts/respond/templates/deployment.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "respond.name" . }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: {{ include "respond.name" . }}
  template:
    metadata:
      labels:
        app: {{ include "respond.name" . }}
    spec:
      serviceAccountName: {{ include "respond.serviceAccountName" . }}
      containers:
        - name: respond
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          ports:
            - containerPort: {{ .Values.service.port }}
          envFrom:
            - secretRef:
                name: {{ include "respond.name" . }}
          env:
            - name: BASE_URL
              value: {{ .Values.config.baseUrl | quote }}
            - name: PORT
              value: {{ .Values.config.port | quote }}
          readinessProbe:
            httpGet:
              path: /healthz
              port: {{ .Values.service.port }}
            initialDelaySeconds: 5
            periodSeconds: 10
```

**Step 9: Create `charts/respond/templates/service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "respond.name" . }}
spec:
  type: {{ .Values.service.type }}
  selector:
    app: {{ include "respond.name" . }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: {{ .Values.service.port }}
```

**Step 10: Create `charts/respond/templates/ingress.yaml`**

```yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "respond.name" . }}
spec:
  ingressClassName: {{ .Values.ingress.className }}
  {{- if .Values.ingress.tls }}
  tls:
    {{- toYaml .Values.ingress.tls | nindent 4 }}
  {{- end }}
  rules:
    - host: {{ .Values.ingress.host }}
      http:
        paths:
          - path: /twilio
            pathType: Prefix
            backend:
              service:
                name: {{ include "respond.name" . }}
                port:
                  number: {{ .Values.service.port }}
{{- end }}
```

**Step 11: Add `/healthz` endpoint to main.go**

```go
mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
})
```

**Step 12: Build and lint the chart**

```bash
go build ./...
helm lint charts/respond/
```
Expected: `1 chart(s) linted, 0 chart(s) failed`

**Step 13: Commit**

```bash
git add charts/ Dockerfile
git commit -m "feat: Helm chart with CloudNativePG, migration job, and ingress"
```

---

## Task 11: Final Review & README

**Files:**
- Create: `README.md`

**Step 1: Create `README.md`**

```markdown
# respond

Twilio-based on-call responder service. When an unknown caller dials your Twilio number, it simultaneously rings all available responders. Responders can toggle their own availability by calling in. Admins (verified by caller ID + PIN) can manage the responder list via a phone menu.

## Configuration

| Env Var | Description |
|---------|-------------|
| `DATABASE_URL` | PostgreSQL connection string |
| `TWILIO_AUTH_TOKEN` | Twilio Auth Token (for signature validation) |
| `BASE_URL` | Public HTTPS URL of this service (e.g. `https://respond.example.com`) |
| `PORT` | HTTP listen port (default: `8080`) |

## Twilio Setup

1. Point your Twilio number's Voice webhook to `POST https://respond.example.com/twilio/voice`
2. Set the Status Callback to `POST https://respond.example.com/twilio/status`

## Deployment

```bash
helm upgrade --install respond charts/respond/ \
  --set secrets.databaseUrl="postgresql://..." \
  --set secrets.twilioAuthToken="..." \
  --set ingress.host="respond.example.com" \
  --set config.baseUrl="https://respond.example.com"
```

## Seeding Initial Data

Connect to the database and insert your first admin directly:

```sql
INSERT INTO admins (phone_number, name, pin_hash)
VALUES ('+15551234567', 'Alice', crypt('your-pin', gen_salt('bf')));
```

Or use a one-off Go CLI tool (not included — add as needed).
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README with setup and deployment instructions"
```

---

## Notes & Known Simplifications

- **Admin name input via phone:** Collecting a name by voice/DTMF from a phone is tricky. The current implementation uses the last 4 digits of the phone number as a placeholder name. A production enhancement would use Twilio's `<Gather input="speech">` with a speech recognition callback to collect a spoken name.
- **Phone number format:** The `normalizePhone` helper assumes US numbers (10 digits → `+1XXXXXXXXXX`). Adjust for international use.
- **PIN input:** Admin PIN is gathered with `numDigits=6`. Adjust as needed. The `#` terminator is mentioned in prompts but Twilio's `numDigits` attribute stops at exactly N digits — use `finishOnKey="#"` attribute if you want variable-length PINs (update `twiml.Gather` accordingly).
- **Seeding:** There's no bootstrap CLI. The first admin must be inserted directly into the database.
