# Hardening Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add handler tests, DTMF input sanitization, SMS session expiry background worker, and Helm chart updates.

**Architecture:** Tests use mock stores implementing the same interfaces already used by the handlers. Input sanitization adds a `sanitizeDigits` helper called before any digits are used. Session expiry is a background goroutine started in `main.go`. Helm chart drops Twilio references and adds FreeSWITCH/VoIP.ms secrets plus SMS ConfigMap.

**Tech Stack:** Go, net/http/httptest, testify-free stdlib testing, Helm

---

## Task 1: DTMF input sanitization

Add a `sanitizeDigits` function that strips non-digit characters and enforces a max length, called at the top of `GatherHandler.ServeHTTP` before `digits` is used by any handler.

**Files:**
- Modify: `internal/handler/gather.go`

**Background:**

`digits` comes from untrusted HTTP form input. Currently it's passed raw to PIN comparison, DB lookups, and phone number validation. Risks:
- Very long strings could cause bcrypt to be called with >72 bytes (bcrypt silently truncates — attacker can craft a 72-byte prefix that matches any longer PIN)
- Non-digit characters could slip through `isValidUSPhone` edge cases

`sanitizeDigits(s string, maxLen int) string` should: strip all non-digit characters, then truncate to `maxLen`.

**Step 1: Write the failing test**

Add to a new file `internal/handler/sanitize_test.go`:

```go
package handler

import (
	"testing"
)

func TestSanitizeDigits(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"1234", 10, "1234"},
		{"12 34", 10, "1234"},         // spaces stripped
		{"12-34", 10, "1234"},         // dashes stripped
		{"abc123", 10, "123"},         // letters stripped
		{"1234567890", 4, "1234"},     // truncated to maxLen
		{"", 10, ""},                  // empty
		{"!@#$%", 10, ""},             // all non-digit
		{"123456789012345", 10, "1234567890"}, // long input truncated
	}
	for _, tt := range tests {
		got := sanitizeDigits(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("sanitizeDigits(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /home/matt/src/respond && go test ./internal/handler/... -run TestSanitizeDigits -v
```
Expected: compile error — `sanitizeDigits` undefined.

**Step 3: Implement `sanitizeDigits` and wire it in**

Add to `internal/handler/gather.go` (add after the existing `sayPhone` function at the bottom):

```go
// sanitizeDigits strips all non-digit characters and truncates to maxLen.
// This prevents overly long or malformed input from reaching bcrypt or DB queries.
func sanitizeDigits(s string, maxLen int) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
			if b.Len() == maxLen {
				break
			}
		}
	}
	return b.String()
}
```

Then in `ServeHTTP`, replace:
```go
digits := r.FormValue("pin_input")
if digits == "" {
    digits = r.FormValue("menu_input")
}
```
with:
```go
digits := sanitizeDigits(r.FormValue("pin_input"), 128)
if digits == "" {
    digits = sanitizeDigits(r.FormValue("menu_input"), 1)
}
```

Note: `pin_input` max is 128 (generous for PINs), `menu_input` max is 1 (single digit menu selections only).

**Step 4: Run tests**

```bash
go test ./internal/handler/... -v
```
Expected: all pass.

**Step 5: Commit**

```bash
git add internal/handler/gather.go internal/handler/sanitize_test.go
git commit -m "feat: sanitize DTMF input before processing"
```

---

## Task 2: Mock stores for handler tests

The handlers depend on `*store.ResponderStore` and `*store.SessionStore`, which are concrete types backed by a real DB. To unit-test handlers, extract interfaces and create in-memory mocks.

**Files:**
- Create: `internal/handler/mocks_test.go`

**Background:**

`store.ResponderStore` and `store.SessionStore` are used directly in handler structs. Go interfaces are satisfied implicitly, so we can define interfaces in the test file matching just the methods the handlers call:

From `voice.go` and `gather.go`, the handler uses these `ResponderStore` methods:
- `FindByPhone(ctx, phone) (*store.Responder, error)`
- `ListAvailable(ctx) ([]*store.Responder, error)`
- `SetPIN(ctx, phone, pin) error`
- `SetValidated(ctx, phone) error`
- `ToggleAvailable(ctx, phone) (bool, error)`
- `UpdatePIN(ctx, phone, pin) error`
- `Create(ctx, phone) error`
- `Delete(ctx, phone) error`
- `ListAll(ctx) ([]*store.Responder, error)`
- `CountByAvailability(ctx) (int, int, error)`
- `SetAdmin(ctx, phone, isAdmin bool) error`

And these `SessionStore` methods:
- `Get(ctx, callSid) (*store.Session, error)`
- `Upsert(ctx, sess) error`
- `Delete(ctx, callSid) error`

**Step 1: Read `internal/store/responder.go` to confirm method signatures**

```bash
cat /home/matt/src/respond/internal/store/responder.go
```

Use the exact signatures found there.

**Step 2: Write `internal/handler/mocks_test.go`**

```go
package handler

import (
	"context"
	"fmt"

	"github.com/mattventura/respond/internal/store"
)

// mockResponderStore is an in-memory ResponderStore for handler tests.
type mockResponderStore struct {
	responders map[string]*store.Responder // keyed by phone
}

func newMockResponderStore(responders ...*store.Responder) *mockResponderStore {
	m := &mockResponderStore{responders: map[string]*store.Responder{}}
	for _, r := range responders {
		m.responders[r.PhoneNumber] = r
	}
	return m
}

func (m *mockResponderStore) FindByPhone(_ context.Context, phone string) (*store.Responder, error) {
	r, ok := m.responders[phone]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}

func (m *mockResponderStore) ListAvailable(_ context.Context) ([]*store.Responder, error) {
	var out []*store.Responder
	for _, r := range m.responders {
		if r.Available {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *mockResponderStore) ListAll(_ context.Context) ([]*store.Responder, error) {
	var out []*store.Responder
	for _, r := range m.responders {
		out = append(out, r)
	}
	return out, nil
}

func (m *mockResponderStore) SetPIN(_ context.Context, phone, pin string) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.PINHash = pin // store raw for test simplicity
	return nil
}

func (m *mockResponderStore) SetValidated(_ context.Context, phone string) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.IsValidated = true
	return nil
}

func (m *mockResponderStore) ToggleAvailable(_ context.Context, phone string) (bool, error) {
	r, ok := m.responders[phone]
	if !ok {
		return false, fmt.Errorf("not found")
	}
	r.Available = !r.Available
	return r.Available, nil
}

func (m *mockResponderStore) UpdatePIN(_ context.Context, phone, pin string) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.PINHash = pin
	return nil
}

func (m *mockResponderStore) Create(_ context.Context, phone string) error {
	if _, ok := m.responders[phone]; ok {
		return fmt.Errorf("already exists")
	}
	m.responders[phone] = &store.Responder{PhoneNumber: phone}
	return nil
}

func (m *mockResponderStore) Delete(_ context.Context, phone string) error {
	delete(m.responders, phone)
	return nil
}

func (m *mockResponderStore) CountByAvailability(_ context.Context) (int, int, error) {
	var active, inactive int
	for _, r := range m.responders {
		if r.Available {
			active++
		} else {
			inactive++
		}
	}
	return active, inactive, nil
}

func (m *mockResponderStore) SetAdmin(_ context.Context, phone string, isAdmin bool) error {
	r, ok := m.responders[phone]
	if !ok {
		return fmt.Errorf("not found")
	}
	r.IsAdmin = isAdmin
	return nil
}

// mockSessionStore is an in-memory SessionStore for handler tests.
type mockSessionStore struct {
	sessions map[string]*store.Session // keyed by CallSid
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{sessions: map[string]*store.Session{}}
}

func (m *mockSessionStore) Get(_ context.Context, callSid string) (*store.Session, error) {
	s, ok := m.sessions[callSid]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	// Return a copy so mutations don't affect the stored value until Upsert
	copy := *s
	stateCopy := s.State
	if stateCopy.Pending != nil {
		pendingCopy := make(map[string]string)
		for k, v := range stateCopy.Pending {
			pendingCopy[k] = v
		}
		stateCopy.Pending = pendingCopy
	}
	copy.State = stateCopy
	return &copy, nil
}

func (m *mockSessionStore) Upsert(_ context.Context, sess *store.Session) error {
	m.sessions[sess.CallSid] = sess
	return nil
}

func (m *mockSessionStore) Delete(_ context.Context, callSid string) error {
	delete(m.sessions, callSid)
	return nil
}
```

**Step 3: Verify it compiles**

The mock types need to satisfy interfaces used by the handlers. The handlers currently take `*store.ResponderStore` and `*store.SessionStore` directly — not interfaces. You will need to check the actual field types in `VoiceHandler` and `GatherHandler` and update them to interfaces if needed.

Read `internal/handler/voice.go` and `internal/handler/gather.go`. If `Responders` is `*store.ResponderStore` (concrete type), you'll need to introduce interfaces.

Add to `internal/handler/gather.go` (top of file, before the struct declarations):

```go
// responderStore defines the responder persistence operations used by handlers.
type responderStore interface {
	FindByPhone(ctx context.Context, phone string) (*store.Responder, error)
	ListAvailable(ctx context.Context) ([]*store.Responder, error)
	ListAll(ctx context.Context) ([]*store.Responder, error)
	SetPIN(ctx context.Context, phone, pin string) error
	SetValidated(ctx context.Context, phone string) error
	ToggleAvailable(ctx context.Context, phone string) (bool, error)
	UpdatePIN(ctx context.Context, phone, pin string) error
	Create(ctx context.Context, phone string) error
	Delete(ctx context.Context, phone string) error
	CountByAvailability(ctx context.Context) (int, int, error)
	SetAdmin(ctx context.Context, phone string, isAdmin bool) error
}

// sessionStore defines the session persistence operations used by handlers.
type sessionStore interface {
	Get(ctx context.Context, callSid string) (*store.Session, error)
	Upsert(ctx context.Context, sess *store.Session) error
	Delete(ctx context.Context, callSid string) error
}
```

Update `GatherHandler` struct fields from concrete to interface types:
```go
type GatherHandler struct {
	Responders responderStore
	Sessions   sessionStore
	BaseURL    string
}
```

Do the same for `VoiceHandler` in `voice.go`:
```go
type VoiceHandler struct {
	Responders responderStore
	Sessions   sessionStore
	BaseURL    string
}
```

Note: `responderStore` and `sessionStore` interfaces are defined in `gather.go` (package `handler`) so they're accessible to `voice.go` in the same package.

Also update `StatusHandler` in `status.go`:
```go
type StatusHandler struct {
	Sessions sessionStore
}
```

Verify `*store.ResponderStore` and `*store.SessionStore` satisfy these interfaces:
```bash
go build ./...
```
Expected: clean build (Go will verify interface satisfaction implicitly at the call sites in `main.go`).

**Step 4: Commit**

```bash
git add internal/handler/
git commit -m "refactor: introduce responderStore and sessionStore interfaces in handler package"
```

---

## Task 3: VoiceHandler tests

**Files:**
- Create: `internal/handler/voice_test.go`

**Step 1: Write the failing tests**

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/store"
)

func TestVoiceHandler_UnknownCaller_DialsAvailable(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: true, IsValidated: true},
		&store.Responder{PhoneNumber: "+15552222222", Available: false, IsValidated: true},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15559999999"}, "Unique-ID": {"call-1"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "bridge") {
		t.Errorf("expected bridge action for unknown caller, got: %s", body)
	}
	if !strings.Contains(body, "15551111111") {
		t.Errorf("expected available responder in dial, got: %s", body)
	}
	if strings.Contains(body, "15552222222") {
		t.Errorf("unavailable responder should not be dialled, got: %s", body)
	}
}

func TestVoiceHandler_NoAvailableResponders(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: false, IsValidated: true},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15559999999"}, "Unique-ID": {"call-1"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "no available responders") {
		t.Errorf("expected no-responders message, got: %s", body)
	}
}

func TestVoiceHandler_KnownResponder_UnvalidatedPromptsPINSetup(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: false, IsValidated: false},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15551111111"}, "Unique-ID": {"call-2"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "PIN") {
		t.Errorf("expected PIN setup prompt, got: %s", body)
	}
	// Session should be created
	sess, err := sessions.Get(req.Context(), "call-2")
	if err != nil {
		t.Fatalf("session not created: %v", err)
	}
	if sess.State.Step != "responder_set_pin" {
		t.Errorf("expected step=responder_set_pin, got %s", sess.State.Step)
	}
}

func TestVoiceHandler_KnownResponder_ValidatedPromptsPIN(t *testing.T) {
	responders := newMockResponderStore(
		&store.Responder{PhoneNumber: "+15551111111", Available: true, IsValidated: true},
	)
	sessions := newMockSessionStore()
	h := &VoiceHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}

	form := url.Values{"Caller-ID-Number": {"+15551111111"}, "Unique-ID": {"call-3"}}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "PIN") {
		t.Errorf("expected PIN prompt, got: %s", body)
	}
	sess, err := sessions.Get(req.Context(), "call-3")
	if err != nil {
		t.Fatalf("session not created: %v", err)
	}
	if sess.State.Step != "responder_pin" {
		t.Errorf("expected step=responder_pin, got %s", sess.State.Step)
	}
}

func TestVoiceHandler_BadForm(t *testing.T) {
	h := &VoiceHandler{
		Responders: newMockResponderStore(),
		Sessions:   newMockSessionStore(),
		BaseURL:    "https://example.com",
	}
	req := httptest.NewRequest("POST", "/fs/voice", strings.NewReader("%invalid"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
```

**Step 2: Run to verify they fail**

```bash
go test ./internal/handler/... -run TestVoiceHandler -v
```
Expected: compile errors — mock types not matching handler field types (fixed by Task 2).

After Task 2 is done, this should compile and tests should pass.

**Step 3: Run and verify pass**

```bash
go test ./internal/handler/... -run TestVoiceHandler -v
```
Expected: all PASS.

**Step 4: Commit**

```bash
git add internal/handler/voice_test.go
git commit -m "test: add VoiceHandler unit tests"
```

---

## Task 4: GatherHandler tests

**Files:**
- Create: `internal/handler/gather_test.go`

The `store.Responder` type uses bcrypt for PINs via `VerifyPIN`. Check `internal/store/responder.go` for the exact method — the mock sets `PINHash` directly. You may need to use bcrypt to hash the PIN in test setup, or check if `VerifyPIN` compares raw strings in tests. Use `golang.org/x/crypto/bcrypt` if needed.

**Step 1: Read `internal/store/responder.go`** to understand `VerifyPIN` before writing tests.

**Step 2: Write the failing tests**

```go
package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mattventura/respond/internal/store"
)

func makeSession(callSid, caller, step string) *store.Session {
	return &store.Session{
		CallSid: callSid,
		Caller:  caller,
		State:   store.SessionState{Step: step, Pending: map[string]string{}},
	}
}

func hashPIN(t *testing.T, pin string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}
	return string(h)
}

func postGather(h *GatherHandler, callSid, pinInput, menuInput string) *httptest.ResponseRecorder {
	form := url.Values{"Unique-ID": {callSid}}
	if pinInput != "" {
		form.Set("pin_input", pinInput)
	}
	if menuInput != "" {
		form.Set("menu_input", menuInput)
	}
	req := httptest.NewRequest("POST", "/fs/gather", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func newGatherHandler(responders *mockResponderStore, sessions *mockSessionStore) *GatherHandler {
	return &GatherHandler{
		Responders: responders,
		Sessions:   sessions,
		BaseURL:    "https://example.com",
	}
}

// --- PIN setup flow ---

func TestGather_SetPIN_StoresAndAdvances(t *testing.T) {
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_set_pin"))
	h := newGatherHandler(newMockResponderStore(), sessions)

	rr := postGather(h, "call-1", "4321", "")

	body := rr.Body.String()
	if !strings.Contains(body, "confirm") {
		t.Errorf("expected confirm prompt, got: %s", body)
	}
	sess, _ := sessions.Get(nil, "call-1")
	if sess.State.Step != "responder_confirm_pin" {
		t.Errorf("expected responder_confirm_pin, got %s", sess.State.Step)
	}
	if sess.State.Pending["new_pin"] != "4321" {
		t.Errorf("expected pending new_pin=4321, got %s", sess.State.Pending["new_pin"])
	}
}

func TestGather_ConfirmPIN_Mismatch_RestartsFlow(t *testing.T) {
	sessions := newMockSessionStore()
	sess := makeSession("call-1", "+15551111111", "responder_confirm_pin")
	sess.State.Pending["new_pin"] = "4321"
	sessions.Upsert(nil, sess)
	h := newGatherHandler(newMockResponderStore(), sessions)

	rr := postGather(h, "call-1", "9999", "") // wrong confirmation

	body := rr.Body.String()
	if !strings.Contains(body, "did not match") {
		t.Errorf("expected mismatch message, got: %s", body)
	}
	updated, _ := sessions.Get(nil, "call-1")
	if updated.State.Step != "responder_set_pin" {
		t.Errorf("expected reset to responder_set_pin, got %s", updated.State.Step)
	}
}

func TestGather_ConfirmPIN_Match_SavesPINAndShowsMenu(t *testing.T) {
	responder := &store.Responder{PhoneNumber: "+15551111111", IsValidated: false}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sess := makeSession("call-1", "+15551111111", "responder_confirm_pin")
	sess.State.Pending["new_pin"] = "4321"
	sessions.Upsert(nil, sess)
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", "4321", "") // matching confirmation

	body := rr.Body.String()
	if !strings.Contains(body, "available") && !strings.Contains(body, "unavailable") {
		t.Errorf("expected responder menu, got: %s", body)
	}
}

// --- PIN verification flow ---

func TestGather_ResponderPIN_Correct_ShowsMenu(t *testing.T) {
	pin := "1234"
	responder := &store.Responder{
		PhoneNumber: "+15551111111",
		Available:   false,
		IsValidated: true,
		PINHash:     hashPIN(t, pin),
	}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_pin"))
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", pin, "")

	body := rr.Body.String()
	if !strings.Contains(body, "available") && !strings.Contains(body, "unavailable") {
		t.Errorf("expected responder menu after correct PIN, got: %s", body)
	}
}

func TestGather_ResponderPIN_Wrong_Goodbye(t *testing.T) {
	responder := &store.Responder{
		PhoneNumber: "+15551111111",
		IsValidated: true,
		PINHash:     hashPIN(t, "1234"),
	}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_pin"))
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", "9999", "")

	body := rr.Body.String()
	if !strings.Contains(body, "Incorrect") {
		t.Errorf("expected incorrect PIN message, got: %s", body)
	}
}

// --- Responder menu ---

func TestGather_ResponderMenu_1_TogglesAvailability(t *testing.T) {
	responder := &store.Responder{PhoneNumber: "+15551111111", Available: false, IsValidated: true}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_menu"))
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", "", "1")

	body := rr.Body.String()
	if !strings.Contains(body, "available") {
		t.Errorf("expected availability confirmation, got: %s", body)
	}
	if responder.Available != true {
		t.Error("expected Available to be toggled to true")
	}
}

// --- Missing session ---

func TestGather_NoSession_SaysGoodbye(t *testing.T) {
	h := newGatherHandler(newMockResponderStore(), newMockSessionStore())

	rr := postGather(h, "nonexistent-call", "1234", "")

	body := rr.Body.String()
	if !strings.Contains(body, "Session not found") {
		t.Errorf("expected session not found message, got: %s", body)
	}
}

// --- Input sanitization integration ---

func TestGather_LongPINInput_IsTruncated(t *testing.T) {
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_set_pin"))
	h := newGatherHandler(newMockResponderStore(), sessions)

	// Send a very long PIN — should be truncated to 128 digits
	longPin := strings.Repeat("1", 200)
	rr := postGather(h, "call-1", longPin, "")

	// Should still advance normally (not crash or error)
	body := rr.Body.String()
	if !strings.Contains(body, "confirm") {
		t.Errorf("expected confirm prompt even for long input, got: %s", body)
	}
	sess, _ := sessions.Get(nil, "call-1")
	if len(sess.State.Pending["new_pin"]) > 128 {
		t.Errorf("stored PIN longer than 128 chars: %d", len(sess.State.Pending["new_pin"]))
	}
}
```

**Step 3: Run to verify they fail (some may fail due to interface issues fixed in Task 2)**

```bash
go test ./internal/handler/... -run TestGather -v
```

**Step 4: Fix any issues and run until all pass**

Common issue: `hashPIN` uses `bcrypt` — check that `golang.org/x/crypto` is already in go.mod (it should be, as `bcrypt` is used in the store). If not: `go get golang.org/x/crypto`.

Also check what `store.Responder.PINHash` field is actually called by reading `internal/store/responder.go` — it may be `PINHash` or `PasswordHash`. Adjust the mock accordingly.

```bash
go test ./internal/handler/... -v
```
Expected: all pass.

**Step 5: Commit**

```bash
git add internal/handler/gather_test.go
git commit -m "test: add GatherHandler unit tests"
```

---

## Task 5: SMS session expiry background worker

**Files:**
- Modify: `cmd/respond/main.go`

Add a background goroutine that calls `smsStore.DeleteExpired` on a ticker. Expired sessions (inactive > `tree.TimeoutMinutes`) are cleaned up automatically.

**Step 1: Write a test for the expiry loop logic**

The loop itself is hard to unit test directly (it's time-based), but we can verify `DeleteExpired` is called. Since the loop is simple, we rely on the existing `DeleteExpired` store tests and just verify the wiring compiles and runs correctly.

Instead, write a simple integration smoke test that the goroutine doesn't panic at startup. Since we can't easily test the goroutine in isolation, we verify the `DeleteExpired` call signature matches expectations.

Add to `internal/store/sms_session_test.go` (already exists — add a new test function):

The existing tests already cover `DeleteExpired`. No new test needed here — just wire it in main.

**Step 2: Add the background worker to `main.go`**

After `smsStore` is created and before `http.ListenAndServe`, add:

```go
// Start background SMS session expiry
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    timeout := time.Duration(tree.TimeoutMinutes) * time.Minute
    for range ticker.C {
        n, err := smsStore.DeleteExpired(context.Background(), timeout)
        if err != nil {
            log.Printf("sms session expiry: %v", err)
        } else if n > 0 {
            log.Printf("sms session expiry: deleted %d expired sessions", n)
        }
    }
}()
```

Add `"time"` to the imports in `main.go`.

**Step 3: Build**

```bash
go build ./...
```
Expected: clean.

**Step 4: Commit**

```bash
git add cmd/respond/main.go
git commit -m "feat: add background SMS session expiry goroutine"
```

---

## Task 6: Helm chart update

**Files:**
- Modify: `charts/respond/Chart.yaml`
- Modify: `charts/respond/values.yaml`
- Modify: `charts/respond/templates/secret.yaml`
- Modify: `charts/respond/templates/deployment.yaml`
- Modify: `charts/respond/templates/ingress.yaml`
- Create: `charts/respond/templates/sms-tree-configmap.yaml`

**Step 1: Update `Chart.yaml`**

Change description:
```yaml
apiVersion: v2
name: respond
description: On-call responder service using FreeSWITCH and VoIP.ms
version: 0.2.0
appVersion: "0.2.0"
```

**Step 2: Update `values.yaml`**

Replace `secrets.twilioAuthToken` with FreeSWITCH/VoIP.ms secrets, add SMS tree config:

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
  # databaseUrl: ""      # Optional: overrides CNPG-generated URL
  fsSharedSecret: ""     # Shared secret for FreeSWITCH webhook validation
  voipmsUsername: ""     # VoIP.ms API username
  voipmsPassword: ""     # VoIP.ms API password
  voipmsDid: ""          # VoIP.ms DID for outbound SMS
  # bootstrapAdmins: ""  # Optional: comma-separated phone:pin pairs

smsTree:
  # Inline YAML content for the SMS decision tree
  # This is mounted at /config/sms-tree.yaml
  content: |
    greeting: "Hi! How can we help? Reply with a number."
    timeout_minutes: 30
    nodes:
      root:
        prompt: "Press 1 for option A, 2 for option B."
        options:
          "1": node_a
          "2": node_b
      node_a:
        response: "For option A, please call our main line."
      node_b:
        response: "Please email support@example.com."

serviceAccount:
  create: true
  name: ""
  imagePullSecrets: []

database:
  enabled: true
  instances: 2
  storage:
    size: 1Gi
    storageClass: ""
```

**Step 3: Update `templates/secret.yaml`**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "respond.name" . }}
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-1"
    "helm.sh/hook-delete-policy": before-hook-creation
  labels:
    app: {{ include "respond.name" . }}
type: Opaque
stringData:
  {{- if .Values.secrets.databaseUrl }}
  DATABASE_URL: {{ .Values.secrets.databaseUrl | quote }}
  {{- end }}
  FS_SHARED_SECRET: {{ .Values.secrets.fsSharedSecret | quote }}
  VOIPMS_USERNAME: {{ .Values.secrets.voipmsUsername | quote }}
  VOIPMS_PASSWORD: {{ .Values.secrets.voipmsPassword | quote }}
  VOIPMS_DID: {{ .Values.secrets.voipmsDid | quote }}
  {{- if .Values.secrets.bootstrapAdmins }}
  BOOTSTRAP_ADMINS: {{ .Values.secrets.bootstrapAdmins | quote }}
  {{- end }}
```

**Step 4: Create `templates/sms-tree-configmap.yaml`**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "respond.name" . }}-sms-tree
  labels:
    app: {{ include "respond.name" . }}
data:
  sms-tree.yaml: |
{{ .Values.smsTree.content | indent 4 }}
```

**Step 5: Update `templates/deployment.yaml`**

Add the ConfigMap volume mount and `SMS_TREE_PATH` env var. Replace the current deployment template with:

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
      annotations:
        rollme: {{ .Release.Revision | quote }}
    spec:
      serviceAccountName: {{ include "respond.serviceAccountName" . }}
      volumes:
        - name: sms-tree
          configMap:
            name: {{ include "respond.name" . }}-sms-tree
      containers:
        - name: respond
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          ports:
            - containerPort: {{ .Values.service.port }}
          envFrom:
            - secretRef:
                name: {{ include "respond.name" . }}
          env:
            {{- if and .Values.database.enabled (not .Values.secrets.databaseUrl) }}
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: {{ include "respond.name" . }}-db-app
                  key: uri
            {{- end }}
            - name: BASE_URL
              value: {{ .Values.config.baseUrl | quote }}
            - name: PORT
              value: {{ .Values.config.port | quote }}
            - name: SMS_TREE_PATH
              value: /config/sms-tree.yaml
          volumeMounts:
            - name: sms-tree
              mountPath: /config
              readOnly: true
          readinessProbe:
            httpGet:
              path: /healthz
              port: {{ .Values.service.port }}
            initialDelaySeconds: 5
            periodSeconds: 10
```

**Step 6: Update `templates/ingress.yaml`**

Replace the `/twilio` path prefix with `/fs` and `/sms`:

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
          - path: /fs
            pathType: Prefix
            backend:
              service:
                name: {{ include "respond.name" . }}
                port:
                  number: {{ .Values.service.port }}
          - path: /sms
            pathType: Prefix
            backend:
              service:
                name: {{ include "respond.name" . }}
                port:
                  number: {{ .Values.service.port }}
          - path: /healthz
            pathType: Exact
            backend:
              service:
                name: {{ include "respond.name" . }}
                port:
                  number: {{ .Values.service.port }}
{{- end }}
```

**Step 7: Verify no Twilio references remain in charts**

```bash
grep -r "twilio\|Twilio\|TWILIO" /home/matt/src/respond/charts/
```
Expected: zero results.

**Step 8: Commit**

```bash
git add charts/
git commit -m "feat: update Helm chart for FreeSWITCH migration (remove Twilio, add SMS ConfigMap)"
```

---

## Task 7: Final verification

**Step 1: Run full test suite**

```bash
cd /home/matt/src/respond && go test ./... -v
```
Expected: all pass (DB tests skip without DATABASE_URL).

**Step 2: Run with race detector**

```bash
go test -race ./...
```
Expected: clean.

**Step 3: Build**

```bash
go build ./...
```
Expected: clean.

**Step 4: Check for any remaining Twilio references in Go files**

```bash
grep -r "twilio\|Twilio\|TWILIO\|twiml\|TwiML" --include="*.go" .
```
Expected: zero results.

**Step 5: Commit and push**

```bash
git add -A
git commit -m "chore: final verification pass"
```
