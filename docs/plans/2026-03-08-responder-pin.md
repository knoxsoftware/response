# Responder PIN Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Require responders to set and enter a PIN to toggle their availability, with the ability to change their PIN, mirroring the admin PIN flow.

**Architecture:** Add `pin_hash` column to `responders` table. Extend `ResponderStore` with `VerifyPIN`/`SetPIN`/`UpdatePIN`. Rework the responder call flow in `voice.go` and `gather.go` to collect PIN on first call (before marking validated) and verify PIN on subsequent calls, presenting a menu with toggle and change-PIN options.

**Tech Stack:** Go, PostgreSQL (pgx/v5), bcrypt (golang.org/x/crypto/bcrypt), Twilio TwiML

---

### Task 1: Add migration for `pin_hash` column

**Files:**
- Create: `migrations/005_responder_pin.sql`

**Step 1: Create migration file**

```sql
ALTER TABLE responders ADD COLUMN IF NOT EXISTS pin_hash TEXT;
```

**Step 2: Apply migration to local/dev DB**

```bash
psql $DATABASE_URL -f migrations/005_responder_pin.sql
```

Expected: `ALTER TABLE`

**Step 3: Commit**

```bash
git add migrations/005_responder_pin.sql
git commit -m "feat: add pin_hash column to responders"
```

---

### Task 2: Extend `Responder` struct and store with PIN methods

**Files:**
- Modify: `internal/store/responder.go`

**Step 1: Add `PinHash` to `Responder` struct**

In the `Responder` struct, add:
```go
PinHash string
```

**Step 2: Update all SELECT queries to include `pin_hash`**

Every query that scans into a `Responder` must include `pin_hash` in the SELECT and add `&r.PinHash` to the `.Scan(...)` call.

Affected queries (all SELECT statements in `responder.go`):
- `FindByPhone` — `SELECT id, phone_number, available, is_validated, pin_hash FROM responders WHERE phone_number=$1` → scan adds `&r.PinHash`
- `ListAvailable` — same column list, scan adds `&r.PinHash`
- `ListAll` — same column list, scan adds `&r.PinHash`

**Step 3: Add `VerifyPIN` method on `Responder`**

```go
func (r *Responder) VerifyPIN(pin string) bool {
	return bcrypt.CompareHashAndPassword([]byte(r.PinHash), []byte(pin)) == nil
}
```

Add `"golang.org/x/crypto/bcrypt"` to imports (already present in admin.go — just add to responder.go).

**Step 4: Add `SetPIN` method on `ResponderStore`**

```go
func (s *ResponderStore) SetPIN(ctx context.Context, phone, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	_, err = s.db.Exec(ctx, `UPDATE responders SET pin_hash=$1 WHERE phone_number=$2`, string(hash), phone)
	return err
}
```

**Step 5: Add `UpdatePIN` method on `ResponderStore`**

```go
func (s *ResponderStore) UpdatePIN(ctx context.Context, phone, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	_, err = s.db.Exec(ctx, `UPDATE responders SET pin_hash=$1 WHERE phone_number=$2`, string(hash), phone)
	return err
}
```

(These are identical in implementation — `SetPIN` is used on first call, `UpdatePIN` on change. Keeping them separate makes the intent clear at call sites.)

**Step 6: Commit**

```bash
git add internal/store/responder.go
git commit -m "feat: add PIN fields and methods to ResponderStore"
```

---

### Task 3: Rework `startResponderFlow` in `voice.go`

**Files:**
- Modify: `internal/handler/voice.go`

The current `startResponderFlow` immediately sets validated and presents a toggle menu. Replace it with branching:

**Step 1: Update `startResponderFlow`**

Replace the entire `startResponderFlow` function:

```go
func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	if !resp.IsValidated {
		// First call: collect PIN before validating
		sess := &store.Session{
			CallSid: callSid,
			Caller:  resp.PhoneNumber,
			State:   store.SessionState{Step: "responder_set_pin", Pending: map[string]string{}},
		}
		if err := h.Sessions.Upsert(ctx, sess); err != nil {
			log.Printf("upsert session: %v", err)
		}
		w.Write([]byte(twiml.Gather("Welcome. Please enter a PIN to secure your account, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
		return
	}

	// Validated: require PIN
	sess := &store.Session{
		CallSid: callSid,
		Caller:  resp.PhoneNumber,
		State:   store.SessionState{Step: "responder_pin", Pending: map[string]string{}},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather("Please enter your PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
}
```

**Step 2: Commit**

```bash
git add internal/handler/voice.go
git commit -m "feat: branch responder flow on validation status and require PIN"
```

---

### Task 4: Add new responder session step handlers in `gather.go`

**Files:**
- Modify: `internal/handler/gather.go`

**Step 1: Register new steps in the switch statement**

In `GatherHandler.ServeHTTP`, add cases before `default`:

```go
case "responder_set_pin":
    h.handleResponderSetPIN(w, r, sess, digits)
case "responder_confirm_pin":
    h.handleResponderConfirmPIN(w, r, sess, digits)
case "responder_pin":
    h.handleResponderPIN(w, r, sess, digits)
case "responder_menu":
    h.handleResponderMenu(w, r, sess, digits)
case "responder_new_pin":
    h.handleResponderNewPIN(w, r, sess, digits)
case "responder_confirm_new_pin":
    h.handleResponderConfirmNewPIN(w, r, sess, digits)
```

**Step 2: Add `responderMenuPrompt` helper**

```go
func responderMenuPrompt(status string) string {
    return "You are currently " + status + ". Press 1 to toggle your availability. Press 2 to change your PIN."
}
```

**Step 3: Add `handleResponderSetPIN`**

Stores the PIN candidate in session pending, moves to confirm step:

```go
func (h *GatherHandler) handleResponderSetPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
    ctx := r.Context()
    if sess.State.Pending == nil {
        sess.State.Pending = map[string]string{}
    }
    sess.State.Pending["new_pin"] = digits
    sess.State.Step = "responder_confirm_pin"
    if err := h.Sessions.Upsert(ctx, sess); err != nil {
        log.Printf("upsert session: %v", err)
    }
    w.Write([]byte(twiml.Gather("Please enter your PIN again to confirm, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
}
```

**Step 4: Add `handleResponderConfirmPIN`**

On match: set PIN, set validated, present menu. On mismatch: re-prompt from start.

```go
func (h *GatherHandler) handleResponderConfirmPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
    ctx := r.Context()
    newPIN := sess.State.Pending["new_pin"]
    if digits != newPIN {
        sess.State.Step = "responder_set_pin"
        sess.State.Pending = map[string]string{}
        h.Sessions.Upsert(ctx, sess)
        w.Write([]byte(twiml.Gather("PINs did not match. Please enter a new PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
        return
    }
    if err := h.Responders.SetPIN(ctx, sess.Caller, newPIN); err != nil {
        log.Printf("set pin: %v", err)
        w.Write([]byte(twiml.Say("Error setting PIN. Goodbye.")))
        return
    }
    if err := h.Responders.SetValidated(ctx, sess.Caller); err != nil {
        log.Printf("set validated: %v", err)
    }
    resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
    if err != nil {
        log.Printf("find responder: %v", err)
        w.Write([]byte(twiml.Say("Error loading account. Goodbye.")))
        return
    }
    status := "unavailable"
    if resp.Available {
        status = "available"
    }
    sess.State.Step = "responder_menu"
    sess.State.Pending = map[string]string{}
    h.Sessions.Upsert(ctx, sess)
    w.Write([]byte(twiml.Gather("PIN set. "+responderMenuPrompt(status), h.BaseURL+"/twilio/voice/gather", 1)))
}
```

**Step 5: Add `handleResponderPIN`**

Verifies PIN, then presents menu:

```go
func (h *GatherHandler) handleResponderPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
    ctx := r.Context()
    resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
    if err != nil || !resp.VerifyPIN(digits) {
        w.Write([]byte(twiml.Say("Incorrect PIN. Goodbye.")))
        return
    }
    status := "unavailable"
    if resp.Available {
        status = "available"
    }
    sess.State.Step = "responder_menu"
    h.Sessions.Upsert(ctx, sess)
    w.Write([]byte(twiml.Gather(responderMenuPrompt(status), h.BaseURL+"/twilio/voice/gather", 1)))
}
```

**Step 6: Add `handleResponderMenu`**

```go
func (h *GatherHandler) handleResponderMenu(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
    ctx := r.Context()
    switch digits {
    case "1":
        newState, err := h.Responders.ToggleAvailable(ctx, sess.Caller)
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
    case "2":
        if sess.State.Pending == nil {
            sess.State.Pending = map[string]string{}
        }
        sess.State.Step = "responder_new_pin"
        h.Sessions.Upsert(ctx, sess)
        w.Write([]byte(twiml.Gather("Please enter your new PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
    default:
        resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
        if err != nil {
            w.Write([]byte(twiml.Say("Error. Goodbye.")))
            return
        }
        status := "unavailable"
        if resp.Available {
            status = "available"
        }
        w.Write([]byte(twiml.Gather("Invalid selection. "+responderMenuPrompt(status), h.BaseURL+"/twilio/voice/gather", 1)))
    }
}
```

**Step 7: Add `handleResponderNewPIN`**

```go
func (h *GatherHandler) handleResponderNewPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
    ctx := r.Context()
    if sess.State.Pending == nil {
        sess.State.Pending = map[string]string{}
    }
    sess.State.Pending["new_pin"] = digits
    sess.State.Step = "responder_confirm_new_pin"
    h.Sessions.Upsert(ctx, sess)
    w.Write([]byte(twiml.Gather("Please enter your new PIN again to confirm, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
}
```

**Step 8: Add `handleResponderConfirmNewPIN`**

```go
func (h *GatherHandler) handleResponderConfirmNewPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
    ctx := r.Context()
    newPIN := sess.State.Pending["new_pin"]
    if digits != newPIN {
        sess.State.Step = "responder_menu"
        sess.State.Pending = map[string]string{}
        resp, _ := h.Responders.FindByPhone(ctx, sess.Caller)
        status := "unavailable"
        if resp != nil && resp.Available {
            status = "available"
        }
        h.Sessions.Upsert(ctx, sess)
        w.Write([]byte(twiml.Gather("PINs did not match. "+responderMenuPrompt(status), h.BaseURL+"/twilio/voice/gather", 1)))
        return
    }
    if err := h.Responders.UpdatePIN(ctx, sess.Caller, newPIN); err != nil {
        log.Printf("update pin: %v", err)
        sess.State.Step = "responder_menu"
        sess.State.Pending = map[string]string{}
        resp, _ := h.Responders.FindByPhone(ctx, sess.Caller)
        status := "unavailable"
        if resp != nil && resp.Available {
            status = "available"
        }
        h.Sessions.Upsert(ctx, sess)
        w.Write([]byte(twiml.Gather("Error updating PIN. "+responderMenuPrompt(status), h.BaseURL+"/twilio/voice/gather", 1)))
        return
    }
    sess.State.Step = "responder_menu"
    sess.State.Pending = map[string]string{}
    resp, _ := h.Responders.FindByPhone(ctx, sess.Caller)
    status := "unavailable"
    if resp != nil && resp.Available {
        status = "available"
    }
    h.Sessions.Upsert(ctx, sess)
    w.Write([]byte(twiml.Gather("Your PIN has been updated. "+responderMenuPrompt(status), h.BaseURL+"/twilio/voice/gather", 1)))
}
```

**Step 9: Remove the old `handleResponderToggle` function** (it is replaced by `handleResponderMenu`) and remove `case "responder_toggle":` from the switch.

**Step 10: Commit**

```bash
git add internal/handler/gather.go
git commit -m "feat: add responder PIN flow handlers"
```

---

### Task 5: Build and smoke test

**Step 1: Build**

```bash
go build ./...
```

Expected: no errors.

**Step 2: Verify the app starts**

```bash
go run ./cmd/respond/main.go
```

Expected: app starts, connects to DB, listens on port.

**Step 3: Manual flow verification checklist**

- [ ] New unvalidated responder calls → hears PIN setup prompt
- [ ] Enters mismatched PINs → re-prompted
- [ ] Enters matching PINs → hears "PIN set. You are currently unavailable. Press 1..."
- [ ] Hangs up and calls again → hears PIN entry prompt
- [ ] Enters wrong PIN → "Incorrect PIN. Goodbye."
- [ ] Enters correct PIN → responder menu
- [ ] Presses 1 → toggled, goodbye
- [ ] Calls again, enters PIN, presses 2 → PIN change flow
- [ ] Admin-responder calls → still goes to admin responder pre-PIN flow (unchanged)

**Step 4: Commit if any fixes were needed**

```bash
git add -p
git commit -m "fix: <describe fix>"
```
