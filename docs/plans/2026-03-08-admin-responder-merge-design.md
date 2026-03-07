# Design: Merge Admins into Responders

**Date:** 2026-03-08
**Status:** Approved

## Summary

Eliminate the separate `admins` table and `AdminStore` by adding an `is_admin` flag to the `responders` table. All admins are responders; this unifies the data model, removes duplicated PIN logic, and enables promote/demote via the admin phone menu.

## Data Model

Add `is_admin BOOLEAN NOT NULL DEFAULT FALSE` to `responders`. Drop `admins` table.

Migration 006:
1. Add `is_admin` column to `responders`
2. Update existing responder rows to set `is_admin=true` where `phone_number` matches an admin, backfilling `pin_hash` from `admins`
3. Insert any admin rows not yet in `responders` (with `is_admin=true`, `pin_hash` from `admins`)
4. Drop `admins` table

`name` is dropped entirely — it was already removed from `responders` in migration 002 and is not used in any call flow.

## Store Layer

Delete `internal/store/admin.go`.

Add to `Responder` struct:
- `IsAdmin bool`

Add to `ResponderStore`:
- `CountAdmins(ctx) (int, error)` — for bootstrap check (`WHERE is_admin=TRUE`)
- `CreateAdmin(ctx, phone, pin) error` — INSERT with `is_admin=true`, hashed PIN
- `SetAdmin(ctx, phone, isAdmin bool) error` — promote/demote

All existing `SELECT` queries on `responders` gain `is_admin` in the column list and scan.

`AdminStore.VerifyPIN` and `AdminStore.UpdatePIN` are covered by existing `Responder.VerifyPIN` and `ResponderStore.UpdatePIN`.

## Handler Layer

Remove `Admins *store.AdminStore` from `VoiceHandler` and `GatherHandler`.

**voice.go** — single `FindByPhone` call; branch on `responder.IsAdmin`:
- `IsAdmin=true` → `startAdminFlow`
- `IsAdmin=false` → `startResponderFlow`
- not found → `dispatchFlow`

**gather.go**:
- `handleAdminPIN`: use `Responders.FindByPhone` + `responder.VerifyPIN`
- `handleAdminConfirmPIN`: use `Responders.UpdatePIN`
- Admin menu gains two new options:
  - Press 7: promote a responder to admin (enter phone number)
  - Press 8: demote an admin to responder (enter phone number)
- `adminMenuPrompt()` updated to include options 7 and 8
- New states: `admin_promote_number`, `admin_demote_number`

## Bootstrap

`BOOTSTRAP_ADMINS` env var format changes from `phone:pin:name` to `phone:pin`.

`main.go` calls `responders.CreateAdmin` instead of `admins.Create`. Admin count check uses `responders.CountAdmins`.

`config.go`: `BootstrapAdmin` drops `Name` field; parsing updated to split on `:` into 2 parts.
