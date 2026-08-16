"""IP rotation orchestration.

Drives the Huawei modem and the local Linux networking (NetworkManager +
iproute2) to force the ISP to hand out a new public IP address.
"""

from __future__ import annotations

import logging
import os
import subprocess
import time
from typing import List, Optional, Tuple

import requests

from .config import Config
from .huawei import HuaweiModem

logger = logging.getLogger(__name__)


class IPRotationError(Exception):
    """Raised when any step of an IP rotation fails."""


class Rotator:
    """High-level operations that force a new public IP address."""

    def __init__(self, config: Config):
        self.config = config
        self.modem = HuaweiModem(
            config.modem_ip, config.modem_user, config.modem_pass
        )

    # -- network helpers -----------------------------------------------------

    def current_ip(self) -> Optional[str]:
        """Fetch the current public IP as seen through the local proxy."""
        proxy = f"http://{self.config.proxy_host}:{self.config.proxy_port}"
        try:
            resp = requests.get(
                self.config.ip_check_url,
                proxies={"http": proxy, "https": proxy},
                timeout=5,
            )
            text = resp.text.strip()
            return text or None
        except requests.RequestException:
            return None

    def _privileged(self, command: List[str]) -> List[str]:
        if (
            self.config.use_sudo
            and hasattr(os, "geteuid")
            and os.geteuid() != 0
        ):
            return ["sudo", *command]
        return command

    def _run(
        self,
        command: List[str],
        check: bool = True,
        timeout: int = 60,
    ) -> subprocess.CompletedProcess:
        logger.info("Running: %s", " ".join(command))
        try:
            result = subprocess.run(
                self._privileged(command),
                capture_output=True,
                text=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired as exc:
            raise IPRotationError(
                f"Command timed out: {' '.join(command)}"
            ) from exc
        if check and result.returncode != 0:
            raise IPRotationError(
                f"Command failed ({' '.join(command)}): "
                f"{result.stderr.strip() or result.stdout.strip()}"
            )
        return result

    # -- rotation steps ------------------------------------------------------

    def reboot_modem(self) -> None:
        self.modem.reboot()

    def toggle_mobile_data(self) -> None:
        """Drop and re-establish the modem data connection."""
        self.modem.set_mobile_data(False)
        logger.info(
            "Waiting %ss for the ISP to release the IP lease...",
            self.config.toggle_wait_seconds,
        )
        time.sleep(self.config.toggle_wait_seconds)
        self.modem.set_mobile_data(True)

    def reconnect_interface(self) -> None:
        """Make sure NetworkManager has the modem interface connected."""
        self._run(["nmcli", "device", "connect", self.config.modem_interface])
        time.sleep(5)

    def apply_routing(self) -> None:
        """Recreate the policy routing that sends modem traffic to the proxy."""
        self._run(
            [
                "sysctl",
                "-w",
                f"net.ipv4.conf.{self.config.modem_interface}.rp_filter=2",
            ],
            check=False,
        )
        self._run(
            [
                "ip",
                "route",
                "replace",
                "default",
                "via",
                self.config.modem_ip,
                "dev",
                self.config.modem_interface,
                "table",
                self.config.routing_table,
            ],
            check=False,
        )
        if self.config.local_ip:
            self._run(
                [
                    "ip",
                    "rule",
                    "add",
                    "from",
                    self.config.local_ip,
                    "lookup",
                    self.config.routing_table,
                ],
                check=False,
            )

    def wait_for_ip(self) -> str:
        """Poll until a new public IP appears (or give up)."""
        for attempt in range(1, self.config.connect_attempts + 1):
            ip = self.current_ip()
            if ip:
                return ip
            logger.info(
                "Still connecting... (attempt %s/%s)",
                attempt,
                self.config.connect_attempts,
            )
            time.sleep(self.config.connect_retry_seconds)
        raise IPRotationError("Timed out waiting for a new public IP.")

    # -- main entry ----------------------------------------------------------

    def rotate(self) -> Tuple[Optional[str], str]:
        """Perform one full rotation and return (old_ip, new_ip)."""
        old_ip = self.current_ip()
        logger.info("Current IP: %s", old_ip or "unknown (offline)")

        if self.config.rotation_mode == "toggle":
            logger.info("Rotation mode: toggle mobile data")
            self.toggle_mobile_data()
        else:
            logger.info("Rotation mode: reboot modem")
            self.reboot_modem()
            logger.info(
                "Modem restarting... waiting %ss for re-initialization.",
                self.config.reboot_wait_seconds,
            )
            time.sleep(self.config.reboot_wait_seconds)

        self.reconnect_interface()
        self.apply_routing()
        new_ip = self.wait_for_ip()
        logger.info("New IP: %s", new_ip)
        return old_ip, new_ip
