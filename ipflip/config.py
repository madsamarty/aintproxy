"""Configuration loading for ipflip.

Settings are read from a `.env` file and/or real environment variables.
Real environment variables always take precedence over the file.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from dotenv import load_dotenv

DEFAULT_ENV_FILE = Path(".env")

VALID_ROTATION_MODES = ("reboot", "toggle")


def _get_bool(value: str) -> bool:
    return value.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True)
class Config:
    """All runtime settings for the proxy rotator."""

    # Huawei modem
    modem_ip: str = "192.168.8.1"
    modem_user: str = "admin"
    modem_pass: str = ""
    # Linux networking
    modem_interface: str = "vodafone0"
    local_ip: str = ""
    routing_table: str = "100"
    # Local HTTP proxy that carries the traffic
    proxy_host: str = "127.0.0.1"
    proxy_port: int = 3128
    ip_check_url: str = "https://api.ipify.org"
    # Rotation behaviour
    rotation_mode: str = "reboot"
    reboot_wait_seconds: int = 35
    toggle_wait_seconds: int = 35
    connect_attempts: int = 15
    connect_retry_seconds: int = 3
    # HTTP rotation service
    rotation_host: str = "0.0.0.0"
    rotation_port: int = 5000
    auth_token: str = ""
    # Privileges
    use_sudo: bool = True

    @classmethod
    def load(cls, env_file: str | os.PathLike[str] | None = None) -> "Config":
        """Build a Config from a `.env` file (default `./.env`) and environment."""
        path = Path(env_file) if env_file is not None else DEFAULT_ENV_FILE
        load_dotenv(path)

        mode = os.getenv("ROTATION_MODE", "reboot").strip().lower()
        if mode not in VALID_ROTATION_MODES:
            raise ValueError(
                f"ROTATION_MODE must be one of {VALID_ROTATION_MODES}, got {mode!r}"
            )

        return cls(
            modem_ip=os.getenv("MODEM_IP", "192.168.8.1"),
            modem_user=os.getenv("MODEM_USER", "admin"),
            modem_pass=os.getenv("MODEM_PASS", ""),
            modem_interface=os.getenv("MODEM_INTERFACE", "vodafone0"),
            local_ip=os.getenv("LOCAL_IP", ""),
            routing_table=os.getenv("ROUTING_TABLE", "100"),
            proxy_host=os.getenv("PROXY_HOST", "127.0.0.1"),
            proxy_port=int(os.getenv("PROXY_PORT", "3128")),
            ip_check_url=os.getenv("IP_CHECK_URL", "https://api.ipify.org"),
            rotation_mode=mode,
            reboot_wait_seconds=int(os.getenv("REBOOT_WAIT_SECONDS", "35")),
            toggle_wait_seconds=int(os.getenv("TOGGLE_WAIT_SECONDS", "35")),
            connect_attempts=int(os.getenv("CONNECT_ATTEMPTS", "15")),
            connect_retry_seconds=int(os.getenv("CONNECT_RETRY_SECONDS", "3")),
            rotation_host=os.getenv("ROTATION_HOST", "0.0.0.0"),
            rotation_port=int(os.getenv("ROTATION_PORT", "5000")),
            auth_token=os.getenv("AUTH_TOKEN", ""),
            use_sudo=_get_bool(os.getenv("USE_SUDO", "true")),
        )
