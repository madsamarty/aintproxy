# IP Flip

Flip your public IP address by rebooting or toggling a Huawei 4G/5G modem.

Most consumer 4G/5G connections sit behind ISP **CGNAT**, so your public IP is
only stable for as long as the modem holds its PPP session. By dropping that
session (reboot, or a mobile-data toggle) you force the ISP to hand out a new
IP. This project automates that whole dance and exposes it as a one-shot CLI
command or a tiny HTTP endpoint.

Built and tested on Linux against Huawei HiLink gateways (B525, B535, E5172 and
similar) on Vodafone CGNAT.

## How it works

```
Scraper / app ──POST /rotate──▶ ipflip (HTTP service :5000)
                                        │
                                        ▼
                              nmcli / ip / sysctl     (Linux routing)
                                        │
                                        ▼
                               local proxy :3128      (squid / tinyproxy)
                                        │
                                        ▼
                        Huawei 4G/5G modem 192.168.8.1 (HiLink API)
                                        │
                                     ISP CGNAT ──▶ new public IP
```

A flip runs these steps:

1. Read the current public IP through your local proxy.
2. **Reboot the modem** (default) *or* **toggle mobile data** off/on.
3. Reconnect the NetworkManager interface and re-apply the policy routing rules.
4. Poll until a new public IP appears (or fail after a timeout).

## Requirements

- Linux with **NetworkManager** and **iproute2** (`nmcli`, `ip`, `sysctl`).
- A Huawei 4G/5G modem running the HiLink web API (admin page reachable over
  HTTP, e.g. `192.168.8.1`).
- Root (or passwordless `sudo`) for the networking commands.
- A local HTTP proxy (squid, tinyproxy, …) that routes traffic out through the
  modem. The flip verifies the new IP through this proxy.
- An ISP that actually hands out fresh IPs on reconnect (works with CGNAT).

> **Not for rate-limited scrapers on residential lines** — some ISPs hand out
> the same IP again from the pool, or throttle rapid reconnects. You may need
> to wait a bit between flips.

## Installation

```bash
git clone https://github.com/<you>/ipflip.git
cd ipflip

python3 -m venv .venv
source .venv/bin/activate
pip install -e ".[dev]"     # include [dev] only if you want to run the tests

cp .env.example .env        # then edit .env — MODEM_PASS is required
```

## Configuration

All settings live in a `.env` file next to the working directory (see
[`.env.example`](.env.example)). Real environment variables always win over the
file.

| Variable | Default | Description |
| --- | --- | --- |
| `MODEM_IP` | `192.168.8.1` | Modem admin page IP. |
| `MODEM_USER` | `admin` | Modem admin username. |
| `MODEM_PASS` | *(required)* | Modem admin password. **Never commit it.** |
| `MODEM_INTERFACE` | `vodafone0` | NetworkManager name of the modem interface (`nmcli device`). |
| `LOCAL_IP` | *(empty)* | Your host IP on the modem subnet (e.g. `192.168.8.104`) for the policy-routing rule. Empty skips the `ip rule` step. |
| `ROUTING_TABLE` | `100` | Routing table used for the modem's default route. |
| `PROXY_HOST` / `PROXY_PORT` | `127.0.0.1` / `3128` | Local proxy through which traffic leaves. |
| `IP_CHECK_URL` | `https://api.ipify.org` | Endpoint used to verify the public IP. |
| `ROTATION_MODE` | `reboot` | `reboot` restarts the modem; `toggle` flips mobile data off/on (faster, less disruptive). |
| `REBOOT_WAIT_SECONDS` | `35` | Pause after a reboot. |
| `TOGGLE_WAIT_SECONDS` | `35` | Pause while data is off (lets the ISP release the lease). |
| `CONNECT_ATTEMPTS` | `15` | IP poll attempts before giving up. |
| `CONNECT_RETRY_SECONDS` | `3` | Seconds between IP polls. |
| `ROTATION_HOST` / `ROTATION_PORT` | `0.0.0.0` / `5000` | HTTP service bind address/port. |
| `AUTH_TOKEN` | *(empty)* | If set, `/rotate` requires the `X-Auth-Token` header. |
| `USE_SUDO` | `true` | Set `false` when running as root (Docker containers run as root by default). |

## Usage

### CLI

```bash
ipflip rotate        # one flip, prints old + new IP
ipflip reboot        # just reboot the modem
ipflip toggle        # just toggle mobile data off/on
ipflip serve         # start the HTTP service (foreground)
```

Use `--env-file /path/to/.env` to point at a different config file.

### HTTP API

```bash
ipflip serve &

# Verify it is up
curl http://127.0.0.1:5000/health

# Trigger a flip
curl -X POST http://127.0.0.1:5000/rotate
# => {"old_ip": "100.64.1.1", "new_ip": "100.64.9.8", "rotated": true}
```

If you set `AUTH_TOKEN`:

```bash
curl -X POST -H "X-Auth-Token: your-token" http://127.0.0.1:5000/rotate
```

Concurrent `/rotate` calls are serialized with a lock so two requests can't
fight over the modem.

## Run with Docker

```bash
cp .env.example .env    # edit — MODEM_PASS is required

docker compose up -d --build

# Verify it is up
curl http://127.0.0.1:5000/health

# Trigger a flip
curl -X POST http://127.0.0.1:5000/rotate
```

The container runs with `--network host` and `privileged` so it can reach the
modem on your LAN, the local proxy on `127.0.0.1:3128`, and run `nmcli`/`ip`/`sysctl`
against the host's network stack.

To stop: `docker compose down`

## Security notes

- **Never commit your `.env`** — it is gitignored; use `.env.example`.
- The HTTP service has **no auth by default**. If you bind to `0.0.0.0`
  (default) anyone on your network can trigger flips. Set `AUTH_TOKEN`
  and/or bind to `127.0.0.1`, and restrict access at the firewall.
- The modem API is plain HTTP on your LAN; that is expected for this hardware.

## Development

```bash
pip install -e ".[dev]"
pytest -v
```

The test suite mocks the modem API and networking, so it runs anywhere.

## Troubleshooting

- **`Login failed`** — wrong `MODEM_PASS`/`MODEM_USER`, or the admin page
  changed on your firmware. Verify you can log in at `http://<MODEM_IP>`.
- **`Timed out waiting for a new public IP`** — the proxy isn't routing through
  the modem, `IP_CHECK_URL` is unreachable, or the ISP reuses IPs. Try raising
  `REBOOT_WAIT_SECONDS`/`TOGGLE_WAIT_SECONDS`.
- **`Command failed`** — check the modem interface name with `nmcli device` and
  your `sudo` setup.
- **Same IP every time** — your ISP hands out the same address from a pool.
  Wait longer between flips or switch to the `toggle` mode.

## License

[MIT](LICENSE)
