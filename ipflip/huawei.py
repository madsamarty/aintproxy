"""Minimal client for the Huawei HiLink web API.

Tested against consumer Huawei 4G/5G gateways (e.g. B525/B535/E5172-style
models). The API uses cookie + CSRF-token sessions over plain HTTP XML.
"""

from __future__ import annotations

import base64
import hashlib
import logging
import re
import xml.etree.ElementTree as ET
from typing import Optional, Tuple

import requests

logger = logging.getLogger(__name__)

_SES_RE = re.compile(r"<SesInfo>(.*?)</SesInfo>")
_TOK_RE = re.compile(r"<TokInfo>(.*?)</TokInfo>")


class HuaweiModemError(Exception):
    """Raised when a Huawei modem API call fails."""


class HuaweiModem:
    """Talk to a Huawei 4G/5G modem over its HiLink HTTP API."""

    def __init__(
        self,
        ip: str,
        username: str = "admin",
        password: str = "",
        timeout: float = 5.0,
    ):
        self.base_url = f"http://{ip}"
        self.username = username
        self.password = password
        self.timeout = timeout
        self.cookie: Optional[str] = None
        self.token: Optional[str] = None

    # -- session handling ----------------------------------------------------

    def login(self) -> None:
        """Open an authenticated session and store its cookie + CSRF token."""
        cookie, token = self._session_tokens()
        payload = (
            "<request>"
            f"<Username>{self.username}</Username>"
            f"<Password>{self._password_hash(self.username, self.password, token)}</Password>"
            "<password_type>4</password_type>"
            "</request>"
        )
        resp = requests.post(
            f"{self.base_url}/api/user/login",
            headers={
                "Cookie": cookie,
                "__RequestVerificationToken": token,
                "Content-Type": "application/xml",
            },
            data=payload,
            timeout=self.timeout,
        )
        if not self._is_ok(resp):
            raise HuaweiModemError(f"Login failed: {resp.text}")

        new_cookie = self._header_cookie(resp, cookie)
        new_token = self._header_token(resp, token)
        if not new_token:
            # Some firmware does not echo the token after login.
            tok_resp = requests.get(
                f"{self.base_url}/api/webserver/TokInfo",
                headers={"Cookie": new_cookie},
                timeout=self.timeout,
            )
            match = _TOK_RE.search(tok_resp.text)
            if match:
                new_token = match.group(1)

        self.cookie, self.token = new_cookie, new_token
        logger.info("Authenticated with Huawei modem at %s", self.base_url)

    def _session_tokens(self) -> Tuple[str, str]:
        resp = requests.get(
            f"{self.base_url}/api/webserver/SesTokInfo", timeout=self.timeout
        )
        resp.raise_for_status()
        cookie = _SES_RE.search(resp.text)
        token = _TOK_RE.search(resp.text)
        if not cookie or not token:
            raise HuaweiModemError(
                f"Could not parse session tokens from: {resp.text}"
            )
        return cookie.group(1), token.group(1)

    # -- commands ------------------------------------------------------------

    def reboot(self) -> None:
        """Reboot the modem hardware. The session goes offline with it."""
        self.login()
        resp = self._post(
            "/api/device/control", "<request><Control>1</Control></request>"
        )
        if not self._is_ok(resp):
            raise HuaweiModemError(f"Reboot command failed: {resp.text}")
        logger.info("Reboot command accepted.")

    def set_mobile_data(self, enabled: bool) -> None:
        """Turn the mobile data connection on (True) or off (False)."""
        self.login()
        value = "1" if enabled else "0"
        resp = self._post(
            "/api/dialup/mobile-dataswitch",
            f"<request><dataswitch>{value}</dataswitch></request>",
        )
        if not self._is_ok(resp):
            action = "enable" if enabled else "disable"
            raise HuaweiModemError(f"Failed to {action} mobile data: {resp.text}")
        logger.info("Mobile data turned %s.", "on" if enabled else "off")

    # -- internals -----------------------------------------------------------

    def _post(self, path: str, payload: str) -> requests.Response:
        if not self.cookie or not self.token:
            self.login()
        headers = {
            "Cookie": self.cookie,
            "__RequestVerificationToken": self.token,
            "Content-Type": "application/xml",
        }
        resp = requests.post(
            f"{self.base_url}{path}", headers=headers, data=payload, timeout=self.timeout
        )
        # The firmware may issue a fresh cookie/token with each response.
        new_token = self._header_token(resp, None)
        if new_token:
            self.token = new_token
        new_cookie = self._header_cookie(resp, None)
        if new_cookie:
            self.cookie = new_cookie
        return resp

    @staticmethod
    def _header_cookie(resp: requests.Response, default: Optional[str]) -> Optional[str]:
        raw = resp.headers.get("Set-Cookie")
        if not raw:
            return default
        return raw.split(";")[0]

    @staticmethod
    def _header_token(resp: requests.Response, default: Optional[str]) -> Optional[str]:
        return resp.headers.get(
            "__requestverificationtokenone"
        ) or resp.headers.get("__requestverificationtoken", default)

    @staticmethod
    def _password_hash(username: str, password: str, token: str) -> str:
        """Huawei's login challenge: base64(sha256(password hex) then re-hash)."""
        hash1 = base64.b64encode(
            hashlib.sha256(password.encode()).hexdigest().encode()
        ).decode()
        return base64.b64encode(
            hashlib.sha256((username + hash1 + token).encode()).hexdigest().encode()
        ).decode()

    @staticmethod
    def _is_ok(resp: requests.Response) -> bool:
        try:
            root = ET.fromstring(resp.text)
            return root.text == "OK"
        except ET.ParseError:
            return "OK" in resp.text
