"""HTTP service that exposes rotation as a simple POST endpoint.

Endpoints:
    POST /rotate  -> runs one rotation, returns {"old_ip", "new_ip", "rotated"}
    GET  /health  -> {"status": "ok"}
"""

from __future__ import annotations

import hmac
import json
import logging
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from .config import Config
from .rotation import Rotator

logger = logging.getLogger(__name__)


class RotateRequestHandler(BaseHTTPRequestHandler):
    server_version = "ipflip/0.1.0"

    # -- helpers -----------------------------------------------------------

    def _send_json(self, status: int, payload: dict) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _authorized(self) -> bool:
        expected = self.server.config.auth_token
        if not expected:
            return True
        supplied = self.headers.get("X-Auth-Token", "")
        return hmac.compare_digest(supplied, expected)

    def log_message(self, fmt: str, *args) -> None:
        logger.info("%s - %s", self.address_string(), fmt % args)

    # -- routes ------------------------------------------------------------

    def do_GET(self) -> None:
        if self.path == "/health":
            self._send_json(200, {"status": "ok"})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self) -> None:
        if self.path != "/rotate":
            self._send_json(404, {"error": "not found"})
            return
        if not self._authorized():
            self._send_json(401, {"error": "unauthorized"})
            return
        with self.server.rotation_lock:
            try:
                old_ip, new_ip = self.server.rotator.rotate()
                self._send_json(
                    200,
                    {
                        "old_ip": old_ip,
                        "new_ip": new_ip,
                        "rotated": old_ip != new_ip,
                    },
                )
            except Exception as exc:  # noqa: BLE001 - surface to caller
                logger.exception("Rotation failed")
                self._send_json(500, {"error": str(exc)})


def create_server(config: Config) -> ThreadingHTTPServer:
    """Build (but do not start) a configured rotation server."""
    server = ThreadingHTTPServer(
        (config.rotation_host, config.rotation_port), RotateRequestHandler
    )
    server.config = config
    server.rotator = Rotator(config)
    server.rotation_lock = threading.Lock()
    return server


def serve(config: Config) -> None:
    server = create_server(config)
    logger.info(
        "Rotation service listening on %s:%s",
        config.rotation_host,
        config.rotation_port,
    )
    server.serve_forever()
