import pytest

from ipflip.config import Config
from ipflip.rotation import IPRotationError, Rotator


class FakeModemNoOp:
    def __init__(self, *args, **kwargs):
        self.reboot_called = False
        self.calls = []

    def reboot(self):
        self.reboot_called = True

    def set_mobile_data(self, enabled):
        self.calls.append(enabled)


def _config(**overrides):
    defaults = dict(modem_pass="pw", modem_ip="192.168.8.1")
    defaults.update(overrides)
    return Config(**defaults)


def _ok_result(*args, **kwargs):
    class Result:
        returncode = 0
        stdout = ""
        stderr = ""

    return Result()


def test_current_ip_uses_configured_proxy(monkeypatch):
    import requests

    captured = {}

    class FakeResp:
        text = "1.2.3.4"

    def fake_get(url, **kwargs):
        captured["url"] = url
        captured["proxies"] = kwargs["proxies"]
        return FakeResp()

    monkeypatch.setattr(requests, "get", fake_get)
    rotator = Rotator(_config(proxy_port=9999))
    assert rotator.current_ip() == "1.2.3.4"
    assert captured["url"] == "https://api.ipify.org"
    assert captured["proxies"] == {
        "http": "http://127.0.0.1:9999",
        "https": "http://127.0.0.1:9999",
    }


def test_current_ip_returns_none_on_error(monkeypatch):
    import requests

    def boom(*args, **kwargs):
        raise requests.RequestException("nope")

    monkeypatch.setattr(requests, "get", boom)
    assert Rotator(_config()).current_ip() is None


def test_run_raises_on_nonzero_exit(monkeypatch):
    from ipflip import rotation as rotmod

    class BadResult:
        returncode = 1
        stdout = ""
        stderr = "boom"

    monkeypatch.setattr(rotmod.subprocess, "run", lambda *a, **k: BadResult())
    with pytest.raises(IPRotationError):
        Rotator(_config())._run(["false"])


def test_rotate_reboot_mode(monkeypatch):
    from ipflip import rotation as rotmod

    rotator = Rotator(
        _config(
            rotation_mode="reboot",
            reboot_wait_seconds=0,
            connect_attempts=1,
            connect_retry_seconds=0,
        )
    )
    rotator.modem = FakeModemNoOp()
    ips = iter([None, "9.9.9.9"])
    monkeypatch.setattr(rotator, "current_ip", lambda: next(ips))
    monkeypatch.setattr(rotmod.subprocess, "run", _ok_result)
    monkeypatch.setattr(rotmod.time, "sleep", lambda *a: None)

    old_ip, new_ip = rotator.rotate()
    assert rotator.modem.reboot_called is True
    assert old_ip is None
    assert new_ip == "9.9.9.9"


def test_rotate_toggle_mode(monkeypatch):
    from ipflip import rotation as rotmod

    rotator = Rotator(
        _config(
            rotation_mode="toggle",
            toggle_wait_seconds=0,
            connect_attempts=1,
            connect_retry_seconds=0,
        )
    )
    rotator.modem = FakeModemNoOp()
    monkeypatch.setattr(rotator, "current_ip", lambda: "1.1.1.1")
    monkeypatch.setattr(rotmod.subprocess, "run", _ok_result)
    monkeypatch.setattr(rotmod.time, "sleep", lambda *a: None)

    old_ip, new_ip = rotator.rotate()
    assert rotator.modem.calls == [False, True]
    assert old_ip == "1.1.1.1"
    assert new_ip == "1.1.1.1"


def test_rotate_times_out_waiting_for_ip(monkeypatch):
    from ipflip import rotation as rotmod

    rotator = Rotator(
        _config(
            rotation_mode="reboot",
            reboot_wait_seconds=0,
            connect_attempts=2,
            connect_retry_seconds=0,
        )
    )
    rotator.modem = FakeModemNoOp()
    monkeypatch.setattr(rotator, "current_ip", lambda: None)
    monkeypatch.setattr(rotmod.subprocess, "run", _ok_result)
    monkeypatch.setattr(rotmod.time, "sleep", lambda *a: None)

    with pytest.raises(IPRotationError):
        rotator.rotate()
