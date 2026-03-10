# Responder Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an `is_validated` field to responders so that unvalidated responders are never dispatched, and the first time a responder calls in they hear a welcome message and are marked as validated.

**Architecture:** Add a DB migration, update the `Responder` struct and all store queries, gate `ListAvailable` on `is_validated=TRUE`, detect first-call in `startResponderFlow`, and update admin list display to show "unvalidated" status.

**Tech Stack:** Go, PostgreSQL (via pgx/v5), Twilio TwiML

---

### Task 1: Migration

**Files:**
- Create: `migrations/004_responder_validation.sql`

**Step 1: Write the migration**

```sql
ALTER TABLE responders ADD COLUMN IF NOT EXISTS is_validated BOOLEAN NOT NULL DEFAULT FALSE;
```

**Step 2: Commit**

```bash
git add migrations/004_responder_validation.sql
git commit -m "feat: add is_validated column to responders"
```

---

### Task 2: Update Responder struct and store queries

**Files:**
- Modify: `internal/store/responder.go`

**Step 1: Add `IsValidated` to the struct**

In the `Responder` struct (line 10), add the field:

```go
type Responder struct {
	ID          string
	PhoneNumber string
	Available   bool
	IsValidated bool
}
```

**Step 2: Update all SELECT queries to include `is_validated`**

`FindByPhone` (line 22):
```go
err := s.db.QueryRow(ctx,
    `SELECT id, phone_number, available, is_validated FROM responders WHERE phone_number=$1`, phone,
).Scan(&r.ID, &r.PhoneNumber, &r.Available, &r.IsValidated)
```

`ListAvailable` (line 32) — also add `is_validated` filter:
```go
rows, err := s.db.Query(ctx,
    `SELECT id, phone_number, available, is_validated FROM responders WHERE available=TRUE AND is_validated=TRUE ORDER BY phone_number`,
)
```
Update the `rows.Scan` call to include `&r.IsValidated`.

`ListAll` (line 51):
```go
rows, err := s.db.Query(ctx,
    `SELECT id, phone_number, available, is_validated FROM responders ORDER BY phone_number`,
)
```
Update the `rows.Scan` call to include `&r.IsValidated`.

**Step 3: Add `SetValidated` method**

Add after `ToggleAvailable`:

```go
func (s *ResponderStore) SetValidated(ctx context.Context, phone string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE responders SET is_validated=TRUE WHERE phone_number=$1`, phone,
	)
	return err
}
```

**Step 4: Build to verify no compile errors**

```bash
go build ./...
```
Expected: no output (success)

**Step 5: Commit**

```bash
git add internal/store/responder.go
git commit -m "feat: add IsValidated to Responder struct and SetValidated store method"
```

---

### Task 3: First-call validation flow in voice handler

**Files:**
- Modify: `internal/handler/voice.go`

**Step 1: Update `startResponderFlow` to detect unvalidated responders**

Replace the existing `startResponderFlow` (lines 57-72) with:

```go
func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	if !resp.IsValidated {
		if err := h.Responders.SetValidated(ctx, resp.PhoneNumber); err != nil {
			log.Printf("set validated: %v", err)
		}
		// Reload so toggle menu reflects actual state
		resp.IsValidated = true
	}

	status := "unavailable"
	if resp.Available {
		status = "available"
	}

	var msg string
	if !resp.IsValidated {
		// This branch is no longer reachable after SetValidated above, but kept for clarity
		msg = "Your phone number has not been validated yet. Validation is now complete. " +
			"You are currently " + status + ". Press 1 to toggle your availability, or press 2 to keep it as is."
	} else {
		msg = "You are currently " + status + ". Press 1 to toggle your availability, or press 2 to keep it as is."
	}
```

Wait — the validation message should only play on the first call (before `SetValidated` is called). Use a local variable to track whether this was the first call:

```go
func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	firstCall := !resp.IsValidated
	if firstCall {
		if err := h.Responders.SetValidated(ctx, resp.PhoneNumber); err != nil {
			log.Printf("set validated: %v", err)
		}
	}

	status := "unavailable"
	if resp.Available {
		status = "available"
	}

	var prefix string
	if firstCall {
		prefix = "Welcome. Your phone number has been registered and is now validated. "
	}

	msg := prefix + "You are currently " + status + ". Press 1 to toggle your availability, or press 2 to keep it as is."
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
```

**Step 2: Build to verify**

```bash
go build ./...
```
Expected: no output

**Step 3: Commit**

```bash
git add internal/handler/voice.go
git commit -m "feat: announce and mark validation on first responder call"
```

---

### Task 4: Update admin list display for validation status

**Files:**
- Modify: `internal/handler/gather.go`

**Step 1: Update responder status helper in `handleAdminMenu` case `"3"` (around line 148)**

Replace the status string logic:
```go
// Before:
status := "unavailable"
if resp.Available {
    status = "available"
}

// After:
var status string
switch {
case !resp.IsValidated:
    status = "unvalidated"
case resp.Available:
    status = "available"
default:
    status = "unavailable"
}
```

**Step 2: Build to verify**

```bash
go build ./...
```
Expected: no output

**Step 3: Commit**

```bash
git add internal/handler/gather.go
git commit -m "feat: show unvalidated status in admin responder list"
```

---

### Task 5: End-to-end smoke test

**Manual verification steps:**

1. Apply the migration against your local DB:
   ```bash
   psql $DATABASE_URL -f migrations/004_responder_validation.sql
   ```
2. Confirm existing responders have `is_validated=FALSE`:
   ```sql
   SELECT phone_number, is_validated FROM responders;
   ```
3. Run the service locally and place a test call from an unvalidated responder number. Confirm:
   - The greeting includes the validation announcement
   - The DB row for that number now has `is_validated=TRUE`
   - A second call does NOT include the announcement
4. Confirm `ListAvailable` excludes unvalidated responders (even if `available=TRUE`)
5. Call the admin "list all" option and confirm unvalidated responders show as "unvalidated"
