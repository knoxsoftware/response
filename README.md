# respond

Twilio-based on-call responder service. When an unknown caller dials your Twilio number, it simultaneously rings all available responders. Responders can toggle their own availability by calling in. Admins (verified by caller ID + PIN) can manage the responder list via a phone menu.

## Call Flow

```mermaid
flowchart TD
    CALL([Incoming Call]) --> LOOKUP{Caller in\nResponderStore?}

    %% Unknown caller
    LOOKUP -->|No| DISPATCH[Dial all available\nresponders simultaneously]

    %% Known responder/admin shared flow
    LOOKUP -->|Yes| VALIDATED{PIN set?}
    VALIDATED -->|No - first call| SET_PIN[Prompt: enter a PIN]
    SET_PIN --> CONFIRM_PIN[Prompt: confirm PIN]
    CONFIRM_PIN -->|Mismatch| SET_PIN
    CONFIRM_PIN -->|Match| SAVE_PIN[Save PIN + mark validated]
    SAVE_PIN --> RESP_MENU

    VALIDATED -->|Yes| ENTER_PIN[Prompt: enter PIN]
    ENTER_PIN -->|Wrong| BYE1([Goodbye])
    ENTER_PIN -->|Correct| RESP_MENU

    RESP_MENU["Responder menu
    1 - Toggle availability
    2 - Change PIN
    3 - Admin menu (admins only)"]

    RESP_MENU -->|1| TOGGLE[Toggle availability → Goodbye]
    RESP_MENU -->|2| NEW_PIN[Prompt: new PIN]
    NEW_PIN --> CONFIRM_NEW_PIN[Prompt: confirm new PIN]
    CONFIRM_NEW_PIN -->|Mismatch| RESP_MENU
    CONFIRM_NEW_PIN -->|Match| UPDATE_PIN([Update PIN → Goodbye])
    RESP_MENU -->|3 admin only| ADMIN_MENU

    ADMIN_MENU["Admin menu
    1 - Add or remove responder
    2 - List responders
    3 - Change availability
    4 - Status summary
    5 - Promote or demote admin"]

    ADMIN_MENU -->|1| ADD_RM_NUM[Enter phone number]
    ADD_RM_NUM -->|not found| ADD_CONFIRM[Press 1 to add\nor hang up to cancel]
    ADD_RM_NUM -->|found| RM_CONFIRM[Press 1 to remove\nor hang up to cancel]
    ADD_CONFIRM -->|1| ADD[Add responder] --> ADMIN_MENU
    ADD_CONFIRM -->|other| ADMIN_MENU
    RM_CONFIRM -->|1| RM[Remove responder] --> ADMIN_MENU
    RM_CONFIRM -->|other| ADMIN_MENU

    ADMIN_MENU -->|2| LIST[Read list] --> ADMIN_MENU
    ADMIN_MENU -->|3| CHG_NUM[Enter phone number]
    CHG_NUM --> TOGGLE_AVAIL[Toggle availability] --> ADMIN_MENU
    ADMIN_MENU -->|4| SUMMARY[Read counts] --> ADMIN_MENU

    ADMIN_MENU -->|5| PROMO_NUM[Enter phone number]
    PROMO_NUM -->|not found| ADMIN_MENU
    PROMO_NUM -->|is admin| DEMOTE_CONFIRM[Press 1 to demote\nor hang up to cancel]
    PROMO_NUM -->|not admin| PROMOTE_CONFIRM[Press 1 to promote\nor hang up to cancel]
    DEMOTE_CONFIRM -->|1| DEMOTE[Demote to responder] --> ADMIN_MENU
    DEMOTE_CONFIRM -->|other| ADMIN_MENU
    PROMOTE_CONFIRM -->|1| PROMOTE[Promote to admin] --> ADMIN_MENU
    PROMOTE_CONFIRM -->|other| ADMIN_MENU
```

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
