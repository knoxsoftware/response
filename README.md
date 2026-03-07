# respond

Twilio-based on-call responder service. When an unknown caller dials your Twilio number, it simultaneously rings all available responders. Responders can toggle their own availability by calling in. Admins (verified by caller ID + PIN) can manage the responder list via a phone menu.

## Call Flow

```mermaid
flowchart TD
    CALL([Incoming Call]) --> LOOKUP{Caller in\nResponderStore?}

    %% Unknown caller
    LOOKUP -->|No| DISPATCH[Dial all available\nresponders simultaneously]

    %% Responder flow
    LOOKUP -->|Yes, not admin| VALIDATED{"Is validated (PIN set)?"}
    VALIDATED -->|No - first call| SET_PIN[Prompt: set a PIN]
    SET_PIN --> CONFIRM_PIN[Prompt: confirm PIN]
    CONFIRM_PIN -->|Mismatch| SET_PIN
    CONFIRM_PIN -->|Match| SAVE_PIN[Save PIN, mark validated]
    SAVE_PIN --> RESP_MENU

    VALIDATED -->|Yes| RESP_PIN[Prompt: enter PIN]
    RESP_PIN -->|Wrong| BYE1[Goodbye]
    RESP_PIN -->|Correct| RESP_MENU[Responder menu\n1 - Toggle availability\n2 - Change PIN]
    RESP_MENU -->|1| TOGGLE_RESP[Toggle availability → Goodbye]
    RESP_MENU -->|2| NEW_PIN[Prompt: new PIN]
    NEW_PIN --> CONFIRM_NEW_PIN[Prompt: confirm new PIN]
    CONFIRM_NEW_PIN -->|Mismatch| RESP_MENU
    CONFIRM_NEW_PIN -->|Match| UPDATE_PIN[Update PIN → Goodbye]

    %% Admin flow
    LOOKUP -->|Yes, is admin| PRE_PIN[Tell current availability status\n1 - Toggle availability\n2 - Continue to admin menu]
    PRE_PIN -->|1 pressed| TOGGLE_ADMIN[Toggle availability]
    TOGGLE_ADMIN --> ADMIN_PIN
    PRE_PIN -->|2 pressed| ADMIN_PIN[Prompt: enter admin PIN]
    ADMIN_PIN -->|Wrong| BYE2[Goodbye]
    ADMIN_PIN -->|Correct| ADMIN_MENU[Admin menu\n1 Add responder\n2 Remove responder\n3 List responders\n4 Change availability\n5 Status summary\n6 Change PIN\n7 Promote to admin\n8 Demote to responder]
    ADMIN_MENU -->|1| ADD_NUM[Enter phone number → confirm → add]
    ADMIN_MENU -->|2| RM_NUM[Enter phone number → remove]
    ADMIN_MENU -->|3| LIST[Read list → back to menu]
    ADMIN_MENU -->|4| CHG_AVAIL[Enter phone number → toggle]
    ADMIN_MENU -->|5| SUMMARY[Read counts → back to menu]
    ADMIN_MENU -->|6| CHG_PIN[Enter new PIN → confirm → update]
    ADMIN_MENU -->|7| PROMOTE[Enter phone number → promote to admin]
    ADMIN_MENU -->|8| DEMOTE[Enter phone number → demote to responder]
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
