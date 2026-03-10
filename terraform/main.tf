terraform {
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.0"
    }
  }
  required_version = ">= 1.3"
}

provider "digitalocean" {
  token = var.do_token
}

# SSH key — references an existing key in your DO account by fingerprint
data "digitalocean_ssh_key" "deploy" {
  name = var.ssh_key_name
}

# Reserved IP for stable SIP registration with VoIP.ms
resource "digitalocean_reserved_ip" "freeswitch" {
  region = var.region
}

resource "digitalocean_reserved_ip_assignment" "freeswitch" {
  ip_address = digitalocean_reserved_ip.freeswitch.ip_address
  droplet_id = digitalocean_droplet.freeswitch.id
}

# Droplet
resource "digitalocean_droplet" "freeswitch" {
  name      = "freeswitch-${var.environment}"
  region    = var.region
  size      = var.droplet_size
  image     = "ubuntu-22-04-x64"
  ssh_keys  = [data.digitalocean_ssh_key.deploy.id]
  user_data = local.cloud_init

  tags = ["freeswitch", var.environment]
}

# Firewall
resource "digitalocean_firewall" "freeswitch" {
  name        = "freeswitch-${var.environment}"
  droplet_ids = [digitalocean_droplet.freeswitch.id]

  # SSH
  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = var.ssh_allowed_cidrs
  }

  # SIP signalling — UDP from VoIP.ms POPs
  # See https://voip.ms/en/how-to/setup/voipms-servers for full list
  inbound_rule {
    protocol         = "udp"
    port_range       = "5060"
    source_addresses = var.voipms_cidrs
  }

  # RTP media — wide range required for voice audio
  inbound_rule {
    protocol         = "udp"
    port_range       = "16384-32768"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  # Allow all outbound
  outbound_rule {
    protocol              = "tcp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "udp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

locals {
  cloud_init = <<-EOF
    #cloud-config
    package_update: true
    package_upgrade: true

    packages:
      - gnupg2
      - wget
      - curl
      - ufw
      - fail2ban

    write_files:
      - path: /etc/fail2ban/jail.local
        content: |
          [DEFAULT]
          bantime  = 3600
          findtime = 600
          maxretry = 5

          [sshd]
          enabled = true

      - path: /usr/local/bin/install-freeswitch.sh
        permissions: '0755'
        content: |
          #!/bin/bash
          set -euo pipefail

          # FreeSWITCH packages via SignalWire
          # Requires SIGNALWIRE_TOKEN env var — set in /etc/environment before running
          TOKEN="${SIGNALWIRE_TOKEN:-}"
          if [ -z "$TOKEN" ]; then
            echo "ERROR: SIGNALWIRE_TOKEN not set. Get a free token at https://signalwire.com"
            echo "Then run: SIGNALWIRE_TOKEN=<token> /usr/local/bin/install-freeswitch.sh"
            exit 1
          fi

          wget --http-user=signalwire --http-password="$TOKEN" \
            -O /usr/share/keyrings/signalwire-freeswitch-repo.gpg \
            https://freeswitch.signalwire.com/repo/deb/debian-release/signalwire-freeswitch-repo.gpg

          echo "machine freeswitch.signalwire.com login signalwire password $TOKEN" \
            > /etc/apt/auth.conf.d/freeswitch.conf
          chmod 600 /etc/apt/auth.conf.d/freeswitch.conf

          echo "deb [signed-by=/usr/share/keyrings/signalwire-freeswitch-repo.gpg] \
            https://freeswitch.signalwire.com/repo/deb/debian-release/ focal main" \
            > /etc/apt/sources.list.d/freeswitch.list

          apt-get update
          apt-get install -y \
            freeswitch \
            freeswitch-mod-sofia \
            freeswitch-mod-dptools \
            freeswitch-mod-flite \
            freeswitch-mod-xml-curl \
            freeswitch-mod-commands \
            freeswitch-mod-dialplan-xml \
            freeswitch-mod-loopback \
            freeswitch-mod-console \
            freeswitch-mod-logfile \
            freeswitch-mod-syslog

          systemctl enable freeswitch
          systemctl start freeswitch
          echo "FreeSWITCH installed and started."

    runcmd:
      - systemctl enable fail2ban
      - systemctl start fail2ban
      - echo "Droplet ready. Run /usr/local/bin/install-freeswitch.sh to install FreeSWITCH (requires SIGNALWIRE_TOKEN)."
  EOF
}
