output "droplet_ip" {
  description = "Ephemeral droplet IP (use reserved_ip for SIP registration)"
  value       = digitalocean_droplet.freeswitch.ipv4_address
}

output "reserved_ip" {
  description = "Reserved IP — use this as your FreeSWITCH public IP in VoIP.ms and freeswitch config"
  value       = digitalocean_reserved_ip.freeswitch.ip_address
}

output "ssh_command" {
  description = "SSH command to connect to the droplet"
  value       = "ssh root@${digitalocean_reserved_ip.freeswitch.ip_address}"
}

output "next_steps" {
  description = "Post-deploy steps"
  value       = <<-EOT
    1. SSH in:
         ssh root@${digitalocean_reserved_ip.freeswitch.ip_address}

    2. Install FreeSWITCH (requires a free SignalWire token from https://signalwire.com):
         SIGNALWIRE_TOKEN=<your-token> /usr/local/bin/install-freeswitch.sh

    3. Copy FreeSWITCH config from the repo:
         scp -r freeswitch/* root@${digitalocean_reserved_ip.freeswitch.ip_address}:/etc/freeswitch/

    4. Edit SIP credentials on the droplet:
         /etc/freeswitch/sip_profiles/voipms.xml

    5. Reload FreeSWITCH:
         fs_cli -x "reloadxml"
         fs_cli -x "sofia profile voipms rescan"

    6. In VoIP.ms portal, point your DID to:
         sip:<your-did>@${digitalocean_reserved_ip.freeswitch.ip_address}

    7. Update your Go app's FS_SHARED_SECRET and BASE_URL env vars.
  EOT
}
