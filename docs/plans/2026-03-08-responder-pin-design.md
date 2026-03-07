# Responder PIN Design

Date: 2026-03-08

## Overview

Give all responders PINs, required to toggle availability. Responders set their PIN on their first call (before validation completes). On subsequent calls they enter their PIN to access the responder menu, where they can toggle availability or change their PIN.

Admin-responders continue to use their admin PIN for the admin flow; the responder PIN applies only to the responder call flow.

## Database

Add `pin_hash TEXT` (nullable) to the `responders` table via a new migration. A null value means the responder has not yet set a PIN (i.e., is unvalidated). The column is populated on first call when the PIN is created.

## Store Layer

Add to `ResponderStore`:
- `SetPIN(ctx, phone, pin string) error` — bcrypt hash and store; used on first call
- `UpdatePIN(ctx, phone, pin string) error` — same, for PIN changes after validation
- `VerifyPIN(pin string) bool` on the `Responder` struct — mirrors `Admin.VerifyPIN`

## Call Flows

### First call (unvalidated, no PIN)

1. Welcome message
2. "Please enter a PIN to secure your account, followed by the pound sign." → session step `responder_set_pin`
3. "Enter your PIN again to confirm, followed by the pound sign." → session step `responder_confirm_pin`
4. On match: call `SetPIN` + `SetValidated`, then present responder menu
5. On mismatch: re-prompt from step 2

### Subsequent calls (validated, has PIN)

1. "Please enter your PIN followed by the pound sign." → session step `responder_pin`
2. On correct PIN: present responder menu
3. On incorrect PIN: "Incorrect PIN. Goodbye."

### Responder menu

Session step `responder_menu`:

> "You are currently [available/unavailable]. Press 1 to toggle your availability. Press 2 to change your PIN."

- Press 1 → toggle, announce new state, goodbye
- Press 2 → "Please enter your new PIN followed by the pound sign." → `responder_new_pin`
  - Confirm → `responder_confirm_new_pin`
  - On match: `UpdatePIN`, "Your PIN has been updated. Goodbye."
  - On mismatch: return to responder menu with error message

## Session Steps Added

| Step | Description |
|------|-------------|
| `responder_set_pin` | First-call PIN entry |
| `responder_confirm_pin` | First-call PIN confirmation |
| `responder_pin` | PIN entry on subsequent calls |
| `responder_menu` | Main responder menu (toggle or change PIN) |
| `responder_new_pin` | New PIN entry for PIN change |
| `responder_confirm_new_pin` | New PIN confirmation for PIN change |

The existing `responder_toggle` step is replaced by `responder_menu` (which handles both toggle and PIN change).

## Migration

New migration file `005_responder_pin.sql`:

```sql
ALTER TABLE responders ADD COLUMN IF NOT EXISTS pin_hash TEXT;
```
