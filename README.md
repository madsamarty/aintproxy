# AintProxy

Flip your public IP address by rotating the IP on a 4G/5G modem.

Most consumer 4G/5G connections sit behind ISP **CGNAT**, so your public IP is
only stable for as long as the modem holds its PPP session. By dropping that
session (mobile-data toggle, or a modem reboot) you force the ISP to hand out a new
IP. This project automates that whole dance and exposes it as a one-shot CLI
command or a tiny HTTP endpoint.

Currently supported: Huawei HiLink gateways (B525, B535, E5172 and similar).
Run `aintproxy drivers` to see all supported modems.

## How it works

```
Scraper / app ──POST /rotate──▶ aintproxy (HTTP service :5000)
                                        │
                                        ▼
                              ip / sysctl               (Linux routing)
                                        │
                                        ▼
                               local proxy :3128        (squid / tinyproxy)
                                        │
                                        ▼
                           4G/5G modem 192.168.8.1     (auto-detected)
                                        │
                                     ISP CGNAT ──▶ new public IP
```

A flip runs these steps:

1. Read the current public IP through your local proxy.
2. **Toggle mobile data** off/on (default) *or* **reboot the modem**.
3. Reconnect the network interface and re-apply the policy routing rules.
4. Poll until a new public IP appears (or fail after a timeout).

## Requirements

- Linux with **iproute2** (`ip`, `sysctl`).
- A supported 4G/5G modem (admin page reachable over HTTP). Run
  `aintproxy drivers` to see supported models.
- Root (or passwordless `sudo`) for the networking commands.
- A local HTTP proxy (squid, tinyproxy, ...) that routes traffic out through the
  modem. The flip verifies the new IP through this proxy.
- An ISP that actually hands out fresh IPs on reconnect (works with CGNAT).

## Installation

### From source

```bash
git clone https://github.com/mohamed-sameh/aintproxy.git
cd aintproxy

go build -o aintproxy .
sudo install -D -m 755 aintproxy /usr/bin/aintproxy
sudo install -D -m 644 config.example.yaml /etc/aintproxy/config.yaml
sudo chmod 600 /etc/aintproxy/config.yaml
```

### Debian package (Ubuntu/Debian)

```bash
# Install build dependencies
sudo apt install debhelper golang-any

# Build the package
dpkg-buildpackage -b -uc

# Install
sudo dpkg -i ../aintproxy_0.1.0-1_*.deb
```

## Configuration

Edit `/etc/aintproxy/config.yaml`:

```yaml
modem:
  ip: "192.168.8.1"
  user: "admin"
  password: "YOUR_MODEM_PASSWORD"   # REQUIRED
  interface: "vodafone0"

network:
  local_ip: "192.168.8.104"         # your host IP on modem subnet
  routing_table: "100"
  proxy_host: "127.0.0.1"
  proxy_port: 3128
  ip_check_url: "https://api.ipify.org"
  use_sudo: true

rotation:
  reboot_wait: 35
  data_off_wait: 35
  ip_check_attempts: 15
  ip_check_interval: 3

server:
  host: "0.0.0.0"
  port: 5000
  auth_token: ""                     # optional
```

See `config.example.yaml` for all options.

## Usage

### CLI

```bash
aintproxy rotate              # one flip, prints old + new IP
aintproxy hard-rotate         # full hardware modem reboot on all interfaces
aintproxy serve               # start the HTTP service (foreground)
aintproxy drivers             # list supported modem drivers
aintproxy version             # print version

aintproxy --config /path/to/config.yaml rotate   # custom config
```

### HTTP API

```bash
aintproxy serve &

# Health check
curl http://127.0.0.1:5000/health

# Trigger a flip
curl -X POST http://127.0.0.1:5000/rotate
# => {"old_ip":"100.64.1.1","new_ip":"100.64.9.8","rotated":true}

# With auth token
curl -X POST -H "X-Auth-Token: your-token" http://127.0.0.1:5000/rotate
```

## Systemd service

```bash
sudo systemctl enable --now aintproxy
journalctl -u aintproxy -f
```

The service runs with hardened defaults:
- `NoNewPrivileges=true`
- `ProtectSystem=strict`
- `ProtectHome=true`
- `CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_SYS_ADMIN`

## Shell completions

Bash, zsh, and fish completions are included in the Debian package.
Manual install:

```bash
# Bash
sudo install -D -m 644 debian/aintproxy.bash-completion \
    /usr/share/bash-completion/completions/aintproxy

# Zsh
sudo install -D -m 644 debian/aintproxy.zsh-completion \
    /usr/share/zsh/vendor-completions/_aintproxy

# Fish
sudo install -D -m 644 debian/aintproxy.fish-completion \
    /usr/share/fish/vendor_completions.d/aintproxy.fish
```

## Development

```bash
go build -o aintproxy .
go test ./...
go vet ./...
```

## Troubleshooting

- **Login failed** -- wrong `modem.password`/`modem.user`, or the admin page
  changed on your firmware. Verify you can log in at `http://<modem.ip>`.
- **Timed out waiting for a new public IP** -- the proxy isn't routing through
  the modem, `ip_check_url` is unreachable, or the ISP reuses IPs. Try raising
  `reboot_wait`/`data_off_wait` or use `hard-rotate`.
- **Command failed** -- check the modem interface name and your `sudo` setup.
- **Same IP every time** -- your ISP hands out the same address from a pool.
  Wait longer between flips or try `hard-rotate` for a full modem reboot.

## License

[MIT](LICENSE)
