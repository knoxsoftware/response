# FreeSWITCH Configuration

Reference configuration files for the FreeSWITCH VPS that handles voice for `respond`.

## Setup

### Prerequisites

- FreeSWITCH installed on a dedicated VPS (not Kubernetes)
- Modules enabled: `mod_xml_curl`, `mod_flite` (TTS), `mod_dptools`, `mod_sofia`
- VoIP.ms account with a DID and SIP credentials

### File Placement

Copy these files to your FreeSWITCH installation:

| File | Destination |
|------|-------------|
| `sip_profiles/voipms.xml` | `/etc/freeswitch/sip_profiles/` |
| `autoload_configs/xml_curl.conf.xml` | `/etc/freeswitch/autoload_configs/` |
| `dialplan/respond.xml` | `/etc/freeswitch/dialplan/` |

### Configuration

1. Edit `sip_profiles/voipms.xml`:
   - Set `username` to your VoIP.ms SIP username
   - Set `password` to your VoIP.ms SIP password
   - Set `proxy` to your nearest VoIP.ms POP (e.g. `denver.voip.ms`, `newyork.voip.ms`, `chicago.voip.ms`)

2. Edit `autoload_configs/xml_curl.conf.xml`:
   - Set `gateway-url` to your Go app's public URL (e.g. `https://respond.example.com/fs/voice`)

3. In VoIP.ms portal:
   - Point your DID's SIP URI to `sip:YOUR_DID@YOUR_VPS_IP`

### Reload After Changes

```bash
# Reload all XML config
fs_cli -x "reloadxml"

# Reload SIP profile only
fs_cli -x "sofia profile voipms rescan"

# Check gateway status
fs_cli -x "sofia status gateway voipms"
```

### Security

`mod_xml_curl` does not support custom request headers, so the `X-FS-Secret` header
approach is not viable from FreeSWITCH directly. Instead:

- Restrict access to `/fs/*` endpoints at the firewall/ingress level to the FreeSWITCH VPS IP only
- Set `FS_SHARED_SECRET` to any value (it will not be validated by FreeSWITCH) and handle this at the network layer
- Alternatively, replace `FSAuth` middleware on `/fs/*` routes with IP allowlist middleware

### TTS Engine

FreeSWITCH uses `mod_flite` for text-to-speech (the `speak` application). Install on Debian/Ubuntu:

```bash
apt-get install freeswitch-mod-flite
```

Enable in `/etc/freeswitch/autoload_configs/modules.conf.xml`:
```xml
<load module="mod_flite"/>
```
