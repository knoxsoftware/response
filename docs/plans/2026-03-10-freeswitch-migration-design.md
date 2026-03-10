# FreeSWITCH Migration — Design Document

**Date:** 2026-03-10
**Status:** Approved
**Branch:** feature/freeswitch-migration

## Overview

Migrate `respond` from Twilio to a self-hosted stack:

- **FreeSWITCH** on a dedicated VPS handles all voice (SIP/RTP)
- **VoIP.ms** provides PSTN connectivity (SIP trunk for voice, HTTP webhooks for SMS)
- **Go app** continues to run on Kubernetes — business logic, session state, and DB are unchanged
- **SMS decision tree** added as a new customer-facing interface, config-driven via YAML

---

## Architecture

```
Inbound call → VoIP.ms SIP trunk → FreeSWITCH (VPS)
                                        │
                                  mod_xml_curl
                                        │
                                  Go app (K8s) ← POST /fs/voice, /fs/gather, /fs/status
                                        │
                                   PostgreSQL

Inbound SMS → VoIP.ms HTTP webhook → Go app (K8s) ← POST /sms/inbound
                                        │
                              SMS conversation engine
                                        │
                               VoIP.ms REST API (outbound SMS)
```

FreeSWITCH and the Go app are decoupled — FreeSWITCH calls the Go app's HTTP endpoints on each call event, exactly as Twilio did. The Go app has no direct dependency on FreeSWITCH.

---

## Voice Layer

### FreeSWITCH

Deployed on a standalone VPS (not Kubernetes) with `hostNetwork` semantics — it binds directly to the node IP for SIP and RTP. Managed by systemd. SIP trunk configured to VoIP.ms.

FreeSWITCH uses `mod_xml_curl` to fetch call handling instructions from the Go app on each call event. This mirrors the Twilio webhook model: FreeSWITCH POSTs call data to the Go app, which responds with XML dialplan instructions.

### Go App Changes

The `internal/twiml` package is replaced by `internal/fsxml`, generating FreeSWITCH-compatible XML. All handler logic, session state, and store calls are unchanged.

| Current | New |
|---|---|
| `POST /twilio/voice` | `POST /fs/voice` |
| `POST /twilio/voice/gather` | `POST /fs/gather` |
| `POST /twilio/status` | `POST /fs/status` |
| `internal/twiml` | `internal/fsxml` |
| Twilio signature validation | FreeSWITCH shared secret header |
| `TWILIO_AUTH_TOKEN` | `FS_SHARED_SECRET` |

### Authentication

Twilio signature validation is removed. FreeSWITCH is configured to include a shared secret header (`X-FS-Secret`) on all webhook requests. The Go app validates this header on all `/fs/*` endpoints.

---

## SMS Layer

### Inbound

VoIP.ms delivers inbound SMS via HTTP webhook to `POST /sms/inbound`. The handler parses the sender number and message body, then runs the conversation engine.

### Conversation Engine

A state machine keyed by sender phone number. Current node position is stored in Postgres in a new `sms_sessions` table, with a configurable inactivity timeout (default 30 minutes). On timeout or terminal node, the session is cleared.

On each inbound message:
1. Load or create session for sender
2. Look up current node in tree
3. Match reply against node options (case-insensitive)
4. Advance to next node, send response
5. If terminal node: send response, optionally trigger `notify_responders` action, clear session

Unrecognized replies repeat the current prompt with a brief error prefix.

### Outbound

Outbound SMS sent via VoIP.ms REST API (simple HTTP POST). A thin `internal/voipms` client wraps this.

### `notify_responders` Action

When a terminal SMS node has `action: notify_responders`, the app sends an SMS to all currently available responders with the customer's number and a brief context message.

---

## SMS Decision Tree Config

YAML file mounted as a Kubernetes ConfigMap. Loaded at startup from the path in `SMS_TREE_PATH` (default `/config/sms-tree.yaml`).

```yaml
greeting: "Hi! How can we help? Reply with a number to continue."
timeout_minutes: 30
nodes:
  root:
    prompt: "Are you experiencing: 1) Situation A  2) Situation B  3) Other"
    options:
      "1": node_a
      "2": node_b
      "3": node_other
  node_a:
    response: "For situation A, please call 555-1234. Our team is available 9–5 weekdays."
  node_b:
    prompt: "Is this urgent? Reply Y or N."
    options:
      Y: node_b_urgent
      N: node_b_normal
  node_b_urgent:
    response: "We're connecting you with an on-call responder now."
    action: notify_responders
  node_b_normal:
    response: "Please email support@example.com. We respond within 24 hours."
  node_other:
    response: "Please call our main line at 555-0000 or email support@example.com."
```

Nodes are either **branch nodes** (have `prompt` + `options`) or **terminal nodes** (have `response`, optional `action`). The tree is validated at startup — missing node references or cycles cause a fatal error.

---

## Data Model Changes

New table `sms_sessions`:

| Column | Type | Notes |
|---|---|---|
| id | UUID | primary key |
| phone_number | TEXT | E.164, unique |
| current_node | TEXT | node key in tree |
| last_activity | TIMESTAMPTZ | used for timeout |
| created_at | TIMESTAMPTZ | |

No changes to existing tables.

---

## Configuration

Remove `TWILIO_AUTH_TOKEN`. Add:

| Variable | Description |
|---|---|
| `FS_SHARED_SECRET` | Validates incoming FreeSWITCH webhook requests |
| `VOIPMS_USERNAME` | VoIP.ms API username |
| `VOIPMS_PASSWORD` | VoIP.ms API password |
| `VOIPMS_DID` | DID to send outbound SMS from |
| `SMS_TREE_PATH` | Path to YAML decision tree (default `/config/sms-tree.yaml`) |

---

## Deployment

### FreeSWITCH VPS

- Single VPS (2 CPU, 2GB RAM sufficient for this use case)
- systemd service, auto-restart on failure
- SIP trunk: VoIP.ms, credentials in `/etc/freeswitch/sip_profiles/`
- `mod_xml_curl` pointed at Go app's `/fs/voice` endpoint
- Firewall: allow UDP 5060 (SIP) from VoIP.ms IPs, UDP 16384–32768 (RTP) open

### Go App (Kubernetes)

- Unchanged deployment model
- New ConfigMap for `sms-tree.yaml`, mounted at `/config/sms-tree.yaml`
- Updated Secret: remove `TWILIO_AUTH_TOKEN`, add `FS_SHARED_SECRET`, `VOIPMS_USERNAME`, `VOIPMS_PASSWORD`, `VOIPMS_DID`
- New migration Job for `sms_sessions` table

---

## Security

- FreeSWITCH shared secret replaces Twilio signature validation
- VoIP.ms SMS webhook validated by source IP allowlist (VoIP.ms publishes their IP ranges)
- VoIP.ms API credentials stored in Kubernetes Secret
- No changes to admin PIN auth or responder caller ID validation
