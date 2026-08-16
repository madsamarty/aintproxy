import json
import threading
import urllib.error
import urllib.request

import pytest

from ipflip.config import Config
from ipflip.server import create_server


class FakeRotator:
    def rotate(self):
        return "1.1.1.1", "2.2.2.2"


@pytest.fixture
def server_and_base():
    config = Config(rotation_host="127.0.0.1", rotation_port=0)
    server = create_server(config)
    server.rotator = FakeRotator()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base = f"http://127.0.0.1:{server.server_address[1]}"
    try:
        yield base, server
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


def _request(base, path, headers=None, method="POST"):
    req = urllib.request.Request(
        f"{base}{path}", data=b"", headers=headers or {}, method=method
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as exc:
        return exc.code, json.loads(exc.read().decode())


def test_health(server_and_base):
    base, _ = server_and_base
    status, body = _request(base, "/health", method="GET")
    assert status == 200
    assert body == {"status": "ok"}


def test_rotate_success(server_and_base):
    base, _ = server_and_base
    status, body = _request(base, "/rotate")
    assert status == 200
    assert body == {
        "old_ip": "1.1.1.1",
        "new_ip": "2.2.2.2",
        "rotated": True,
    }


def test_unknown_route_returns_404(server_and_base):
    base, _ = server_and_base
    status, body = _request(base, "/nope", method="GET")
    assert status == 404


def test_rotate_failure_returns_500(server_and_base):
    base, server = server_and_base

    class FailingRotator:
        def rotate(self):
            raise RuntimeError("modem on fire")

    server.rotator = FailingRotator()
    status, body = _request(base, "/rotate")
    assert status == 500
    assert body["error"] == "modem on fire"


def test_auth_token_required():
    config = Config(
        rotation_host="127.0.0.1", rotation_port=0, auth_token="sekret"
    )
    server = create_server(config)
    server.rotator = FakeRotator()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    base = f"http://127.0.0.1:{server.server_address[1]}"
    try:
        status, _ = _request(base, "/rotate")
        assert status == 401

        status, body = _request(
            base, "/rotate", headers={"X-Auth-Token": "sekret"}
        )
        assert status == 200
        assert body["new_ip"] == "2.2.2.2"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
