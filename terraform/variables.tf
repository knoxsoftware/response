variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
  sensitive   = true
}

variable "environment" {
  description = "Environment name (e.g. prod, staging)"
  type        = string
  default     = "prod"
}

variable "region" {
  description = "DigitalOcean region slug"
  type        = string
  default     = "nyc3"
  # Pick closest to your VoIP.ms POP:
  # nyc1/nyc3 → newyork.voip.ms
  # sfo3      → losangeles.voip.ms or seattle.voip.ms
  # tor1      → toronto.voip.ms
  # ams3      → (EU, not ideal for VoIP.ms)
}

variable "droplet_size" {
  description = "Droplet size slug — s-1vcpu-2gb handles ~100 concurrent calls"
  type        = string
  default     = "s-1vcpu-2gb"
}

variable "ssh_key_name" {
  description = "Name of the SSH key in your DigitalOcean account"
  type        = string
}

variable "ssh_allowed_cidrs" {
  description = "CIDRs allowed to SSH to the droplet"
  type        = list(string)
  default     = ["0.0.0.0/0", "::/0"]
}

variable "voipms_cidrs" {
  description = "VoIP.ms server IP ranges for SIP inbound. See https://voip.ms/en/how-to/setup/voipms-servers"
  type        = list(string)
  default = [
    # Primary VoIP.ms POP ranges — update to match your configured POP
    "74.50.0.0/16",    # Various VoIP.ms POPs
    "209.105.0.0/16",  # VoIP.ms Canada
    "23.239.0.0/18",   # Linode-hosted POPs
    "0.0.0.0/0",       # Fallback: open (lock down once you know your POP's IP)
  ]
}
