# Admin-Responder Merge Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate the `admins` table and `AdminStore` by adding `is_admin` to `responders`, unifying the data model and removing duplicated PIN logic.

**Architecture:** Add `is_admin BOOLEAN` to `responders`, migrate existing admin data, drop `admins` table. Delete `AdminStore`; extend `ResponderStore` with admin-specific methods. Update handlers to do a single `FindByPhone` and branch on `IsAdmin`.

**Tech Stack:** Go, PostgreSQL (pgx/v5), Twilio TwiML

---

### Task 1: Write migration 006

**Files:**
- Create: `migrations/006_merge_admins.sql`

**Step 1: Create the migration file**

```sql
-- Step 1: add is_admin column
ALTER TABLE responders ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- Step 2: backfill pin_hash and mark is_admin for existing admins
UPDATE responders r
SET is_admin = TRUE,
    pin_hash  = a.pin_hash
FROM admins a
WHERE r.phone_number = a.phone_number;

-- Step 3: insert any admins not yet in responders
INSERT INTO responders (phone_number, is_admin, pin_hash)
SELECT a.phone_number, TRUE, a.pin_hash
FROM admins a
WHERE NOT EXISTS (
    SELECT 1 FROM responders r WHERE r.phone_number = a.phone_number
);

-- Step 4: drop the admins table
DROP TABLE admins;
```

**Step 2: Verify the file looks correct, then commit**

```bash
git add migrations/006_merge_admins.sql
git commit -m "feat: add migration to merge admins into responders"
```

---

### Task 2: Update `Responder` struct and all SELECT queries

**Files:**
- Modify: `internal/store/responder.go`

**Step 1: Add `IsAdmin bool` to the struct**

In the `Responder` struct (around line 11), add:
```go
IsAdmin     bool
```

The full struct becomes:
```go
type Responder struct {
	ID          string
	PhoneNumber string
	Available   bool
	IsValidated bool
	IsAdmin     bool
	PinHash     *string
}
```

**Step 2: Update `FindByPhone` query**

Replace the SELECT and Scan in `FindByPhone`:
```go
err := s.db.QueryRow(ctx,
    `SELECT id, phone_number, available, is_validated, is_admin, pin_hash FROM responders WHERE phone_number=$1`, phone,
).Scan(&r.ID, &r.PhoneNumber, &r.Available, &r.IsValidated, &r.IsAdmin, &r.PinHash)
```

**Step 3: Update `ListAvailable` query**

```go
rows, err := s.db.Query(ctx,
    `SELECT id, phone_number, available, is_validated, is_admin, pin_hash FROM responders WHERE available=TRUE AND is_validated=TRUE ORDER BY phone_number`,
)
```
And in the scan loop:
```go
if err := rows.Scan(&r.ID, &r.PhoneNumber, &r.Available, &r.IsValidated, &r.IsAdmin, &r.PinHash); err != nil {
```

**Step 4: Update `ListAll` query**

Same pattern as `ListAvailable` — add `is_admin` to SELECT and Scan.

**Step 5: Add `CountAdmins`, `CreateAdmin`, and `SetAdmin` methods**

Append to `responder.go`:

```go
func (s *ResponderStore) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM responders WHERE is_admin=TRUE`).Scan(&n)
	return n, err
}

func (s *ResponderStore) CreateAdmin(ctx context.Context, phone, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO responders (phone_number, is_admin, pin_hash) VALUES ($1, TRUE, $2)`,
		phone, string(hash),
	)
	return err
}

func (s *ResponderStore) SetAdmin(ctx context.Context, phone string, isAdmin bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE responders SET is_admin=$1 WHERE phone_number=$2`, isAdmin, phone,
	)
	return err
}
```

**Step 6: Verify the file compiles**

```bash
go build ./...
```
Expected: no errors.

**Step 7: Commit**

```bash
git add internal/store/responder.go
git commit -m "feat: add is_admin to Responder struct and admin store methods"
```

---

### Task 3: Delete `AdminStore`

**Files:**
- Delete: `internal/store/admin.go`

**Step 1: Delete the file**

```bash
rm internal/store/admin.go
```

**Step 2: Verify it compiles (will fail — that's expected)**

```bash
go build ./...
```
Expected: errors referencing `AdminStore`, `store.Admin`, etc. These will be fixed in the next tasks.

**Step 3: Commit the deletion**

```bash
git add -u internal/store/admin.go
git commit -m "feat: delete AdminStore (replaced by ResponderStore)"
```

---

### Task 4: Update `config.go` — drop `Name` from `BootstrapAdmin`

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Remove `Name` from `BootstrapAdmin` struct**

```go
type BootstrapAdmin struct {
	Phone string
	PIN   string
}
```

**Step 2: Update the parsing loop**

Replace the `BOOTSTRAP_ADMINS` parsing block:
```go
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
```

**Step 3: Verify it compiles**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: drop name from BootstrapAdmin config"
```

---

### Task 5: Update `main.go` — use `ResponderStore` for bootstrap

**Files:**
- Modify: `cmd/respond/main.go`

**Step 1: Remove `admins` store instantiation and update bootstrap block**

Remove:
```go
admins := store.NewAdminStore(pool)
```

Update the bootstrap block to use `responders`:
```go
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
```

**Step 2: Remove `Admins` fields from handler structs**

```go
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
```

**Step 3: Verify it compiles (will fail until handlers are updated)**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add cmd/respond/main.go
git commit -m "feat: update bootstrap to use ResponderStore"
```

---

### Task 6: Update `voice.go` — remove AdminStore, branch on IsAdmin

**Files:**
- Modify: `internal/handler/voice.go`

**Step 1: Remove `Admins *store.AdminStore` from `VoiceHandler`**

```go
type VoiceHandler struct {
	Responders *store.ResponderStore
	Sessions   *store.SessionStore
	BaseURL    string
}
```

**Step 2: Update `ServeHTTP` to single lookup + IsAdmin branch**

Replace the dual-lookup block:
```go
responder, err := h.Responders.FindByPhone(ctx, from)

switch {
case err == nil && responder.IsAdmin:
    h.startAdminFlow(w, r, ctx, responder, callSid)
case err == nil:
    h.startResponderFlow(w, r, ctx, responder, callSid)
default:
    h.dispatchFlow(w, ctx)
}
```

**Step 3: Update `startAdminFlow` signature to accept `*store.Responder`**

Change the signature from `admin *store.Admin` to `responder *store.Responder` and update the body. The method currently does a second `FindByPhone` to get responder availability — that lookup is no longer needed since we already have the responder:

```go
func (h *VoiceHandler) startAdminFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, responder *store.Responder, callSid string) {
	status := "unavailable"
	if responder.Available {
		status = "available"
	}
	sess := &store.Session{
		CallSid: callSid,
		Caller:  responder.PhoneNumber,
		State:   store.SessionState{Step: "admin_responder_pre_pin", Pending: map[string]string{}},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	msg := "You are currently " + status + " as a responder. Press 1 to toggle your availability, or press 2 to continue to the admin menu."
	w.Write([]byte(twiml.Gather(msg, h.BaseURL+"/twilio/voice/gather", 1)))
}
```

**Step 4: Verify it compiles**

```bash
go build ./...
```

**Step 5: Commit**

```bash
git add internal/handler/voice.go
git commit -m "feat: update VoiceHandler to use is_admin flag instead of AdminStore"
```

---

### Task 7: Update `gather.go` — remove AdminStore, fix admin PIN handlers, add promote/demote

**Files:**
- Modify: `internal/handler/gather.go`

**Step 1: Remove `Admins *store.AdminStore` from `GatherHandler`**

```go
type GatherHandler struct {
	Responders *store.ResponderStore
	Sessions   *store.SessionStore
	BaseURL    string
}
```

**Step 2: Update `handleAdminPIN` to use `Responders`**

```go
func (h *GatherHandler) handleAdminPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	responder, err := h.Responders.FindByPhone(ctx, sess.Caller)
	if err != nil || !responder.VerifyPIN(digits) {
		w.Write([]byte(twiml.Say("Incorrect PIN. Goodbye.")))
		return
	}
	sess.State.Step = "admin_menu"
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather(adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}
```

**Step 3: Update `handleAdminConfirmPIN` to use `Responders.UpdatePIN`**

Replace `h.Admins.UpdatePIN(ctx, sess.Caller, newPIN)` with:
```go
if err := h.Responders.UpdatePIN(ctx, sess.Caller, newPIN); err != nil {
```

**Step 4: Update `adminMenuPrompt` to include promote/demote options**

```go
func adminMenuPrompt() string {
	return "Admin menu. Press 1 to add a responder. Press 2 to remove a responder. Press 3 to list all responders. Press 4 to change a responder's availability. Press 5 for responder status summary. Press 6 to change your PIN. Press 7 to promote a responder to admin. Press 8 to demote an admin to responder."
}
```

**Step 5: Add cases 7 and 8 to `handleAdminMenu`**

Inside the `switch digits` block in `handleAdminMenu`, add after case "6":
```go
case "7":
    sess.State.Step = "admin_promote_number"
    sess.State.Pending = map[string]string{}
    h.Sessions.Upsert(ctx, sess)
    w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the responder to promote to admin, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
case "8":
    sess.State.Step = "admin_demote_number"
    sess.State.Pending = map[string]string{}
    h.Sessions.Upsert(ctx, sess)
    w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the admin to demote to responder, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
```

**Step 6: Add `handleAdminPromoteNumber` and `handleAdminDemoteNumber` methods**

```go
func (h *GatherHandler) handleAdminPromoteNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	if err := h.Responders.SetAdmin(ctx, phone, true); err != nil {
		log.Printf("promote admin: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error promoting responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(sayPhone(phone)+" is now an admin. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminDemoteNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	if err := h.Responders.SetAdmin(ctx, phone, false); err != nil {
		log.Printf("demote admin: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error demoting admin. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(sayPhone(phone)+" is now a responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}
```

**Step 7: Add the new states to the switch in `ServeHTTP`**

```go
case "admin_promote_number":
    h.handleAdminPromoteNumber(w, r, sess, digits)
case "admin_demote_number":
    h.handleAdminDemoteNumber(w, r, sess, digits)
```

**Step 8: Verify full build**

```bash
go build ./...
```
Expected: no errors.

**Step 9: Commit**

```bash
git add internal/handler/gather.go
git commit -m "feat: update GatherHandler — use ResponderStore for admin PIN, add promote/demote"
```

---

### Task 8: Update Helm chart / deployment config

**Files:**
- Modify: `charts/respond/values.yaml`
- Modify: `charts/respond/templates/secret.yaml` (if `BOOTSTRAP_ADMINS` is set there)

**Step 1: Find where `BOOTSTRAP_ADMINS` is configured**

```bash
grep -r "BOOTSTRAP_ADMINS" charts/
```

**Step 2: Update any `phone:pin:name` entries to `phone:pin` format**

Remove the `:name` suffix from any bootstrap admin entries in the chart values or secrets.

**Step 3: Commit**

```bash
git add charts/
git commit -m "chore: update BOOTSTRAP_ADMINS format in helm chart (drop name)"
```

---

### Task 9: Final verification

**Step 1: Full build**

```bash
go build ./...
```
Expected: clean.

**Step 2: Verify no remaining references to `AdminStore` or `admins` table**

```bash
grep -r "AdminStore\|admins\." --include="*.go" .
grep -r "FROM admins\|INTO admins\|admins WHERE" --include="*.sql" migrations/
```
Expected: no matches (except in migration files 001–003 which are historical and fine to leave).

**Step 3: Commit any final cleanup, then tag**

```bash
git add .
git commit -m "feat: complete admin-responder merge"
```
