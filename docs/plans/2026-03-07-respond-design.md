# Respond Service — Design Document

**Date:** 2026-03-07
**Status:** Superseded by [2026-03-10-freeswitch-migration-design.md](2026-03-10-freeswitch-migration-design.md)

## Overview

`respond` is a Go service that integrates with Twilio to manage on-call responders. It maintains a list of responder phone numbers and their availability state. When an unknown caller dials the Twilio number, the service returns TwiML that simultaneously rings all available responders. Known responders can toggle their own availability by calling in. Known admins (verified by caller ID + PIN) can manage the responder list via a phone menu.

---

## Architecture

A single Go service with three inbound call flows, all entering via the same Twilio webhook. The caller's phone number (from `From` field) is looked up at call entry to determine which flow to invoke. Admin flow takes priority if a caller is both a responder and an admin.

**Flows:**
1. **Dispatch** — unknown caller → TwiML to simultaneously dial all available responders
2. **Responder self-service** — known responder → DTMF toggle of own availability
3. **Admin menu** — known admin (caller ID allowlisted + PIN verified) → manage responder list

---

## Data Model

PostgreSQL database with three tables:

### `responders`
| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key |
| phone_number | TEXT | E.164 format, unique |
| name | TEXT | |
| available | BOOLEAN | default false |
| created_at | TIMESTAMPTZ | |

### `admins`
| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key |
| phone_number | TEXT | E.164 format, unique |
| name | TEXT | |
| pin_hash | TEXT | bcrypt |
| created_at | TIMESTAMPTZ | |

### `call_sessions`
| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key |
| call_sid | TEXT | Twilio CallSid, unique |
| caller | TEXT | E.164 phone number |
| session_state | JSONB | tracks menu position and pending inputs |
| created_at | TIMESTAMPTZ | |

---

## Call Flow

### Entry: `POST /twilio/voice`

1. Validate Twilio signature
2. Lookup `From` number in DB
3. Route:
   - Admin → prompt for PIN → verify → admin menu
   - Responder (non-admin) → read current availability → offer DTMF toggle → confirm
   - Unknown → return TwiML with `<Dial>` dialing all available responders simultaneously

### Admin Menu (post-PIN)
- **1** — Add responder (collect number + name via DTMF/speech)
- **2** — Remove responder (collect number)
- **3** — List all responders with availability (read back via TTS)
- **4** — Change a responder's availability (collect number, toggle)

### Session State
Stored in `call_sessions` keyed by Twilio `CallSid`. Required because Twilio makes a fresh HTTP request for each DTMF gather, so state cannot be held in memory.

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/twilio/voice` | Entry point for all inbound calls |
| POST | `/twilio/voice/gather` | Handles DTMF input, dispatched by session state |
| POST | `/twilio/status` | Twilio status callback — cleans up call_sessions |

All Twilio endpoints validate `X-Twilio-Signature` using the Twilio auth token.

---

## Configuration

Environment variables:

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | PostgreSQL connection string |
| `TWILIO_AUTH_TOKEN` | Used for webhook signature validation |
| `PORT` | HTTP listen port (default 8080) |

---

## Deployment

**Container:** Single Go binary in a distroless image. Stateless — safe to run multiple replicas.

**Helm chart (`charts/respond/`):**
- `Deployment` — configurable replica count
- `Service` — ClusterIP
- `Ingress` — exposes webhooks to Twilio
- `Secret` — database credentials, Twilio auth token
- `ServiceAccount`
- CloudNativePG `Cluster` CRD — on-cluster PostgreSQL, no PVC dependency
- Migration `Job` — runs DB migrations on each helm upgrade before rollout

---

## Security

- Twilio signature validation on all webhook endpoints (prevents spoofed requests)
- Admin authentication: caller ID allowlist + bcrypt PIN hash (two factors)
- All secrets managed via Kubernetes `Secret` objects, injected as env vars
- No public REST API — all management is phone-based
