import pytest
import requests
from requests.structures import CaseInsensitiveDict

from ipflip.huawei import HuaweiModem, HuaweiModemError

MODEM_URL = "http://192.168.8.1"


class FakeResponse:
    def __init__(self, text="OK", headers=None):
        self.text = text
        self.headers = CaseInsensitiveDict(headers or {})

    def raise_for_status(self):
        return None


def _login_fake(url, **kwargs):
    if url.endswith("/api/webserver/SesTokInfo"):
        return FakeResponse(
            "<response><SesInfo>cookie1</SesInfo><TokInfo>tok1</TokInfo></response>"
        )
    raise AssertionError(f"unexpected GET {url}")


def _post_login_fake(url, **kwargs):
    if url.endswith("/api/user/login"):
        assert "<Username>admin</Username>" in kwargs["data"]
        assert "password_type" in kwargs["data"]
        return FakeResponse(
            "OK",
            {
                "Set-Cookie": "cookie2; Path=/",
                "__RequestVerificationTokenOne": "tok2",
            },
        )
    raise AssertionError(f"unexpected POST {url}")


def test_password_hash_known_vector():
    actual = HuaweiModem._password_hash("admin", "admin", "1234")
    assert actual == (
        "M2IzNWMxMTA0ODU0ZTU5ZjFhMjA4YmQ5NTJhMDQ3YzkyODhlMzNiYTEyYTQ4YTkx"
        "NGZiZjY0MGRmMzY0NTZiZQ=="
    )


def test_session_tokens(monkeypatch):
    monkeypatch.setattr(requests, "get", _login_fake)
    modem = HuaweiModem("192.168.8.1", "admin", "admin")
    cookie, token = modem._session_tokens()
    assert cookie == "cookie1"
    assert token == "tok1"


def test_is_ok():
    assert HuaweiModem._is_ok(FakeResponse("<response>OK</response>")) is True
    assert HuaweiModem._is_ok(FakeResponse("<response>ERROR</response>")) is False
    assert HuaweiModem._is_ok(FakeResponse("OK")) is True


def test_login_refreshes_cookie_and_token(monkeypatch):
    monkeypatch.setattr(requests, "get", _login_fake)
    monkeypatch.setattr(requests, "post", _post_login_fake)
    modem = HuaweiModem("192.168.8.1", "admin", "admin")
    modem.login()
    assert modem.cookie == "cookie2"
    assert modem.token == "tok2"


def test_login_failure_raises(monkeypatch):
    def fail_login(url, **kwargs):
        if url.endswith("/api/user/login"):
            return FakeResponse("<response>ERROR</response>")
        raise AssertionError(f"unexpected POST {url}")

    monkeypatch.setattr(requests, "get", _login_fake)
    monkeypatch.setattr(requests, "post", fail_login)
    modem = HuaweiModem("192.168.8.1", "admin", "wrong")
    with pytest.raises(HuaweiModemError):
        modem.login()


def test_reboot_sends_control_command(monkeypatch):
    def post(url, **kwargs):
        if url.endswith("/api/user/login"):
            return _post_login_fake(url, **kwargs)
        if url.endswith("/api/device/control"):
            assert "<Control>1</Control>" in kwargs["data"]
            return FakeResponse("OK")
        raise AssertionError(f"unexpected POST {url}")

    monkeypatch.setattr(requests, "get", _login_fake)
    monkeypatch.setattr(requests, "post", post)
    modem = HuaweiModem("192.168.8.1", "admin", "admin")
    modem.reboot()
    assert modem.cookie == "cookie2"


def test_reboot_failure_raises(monkeypatch):
    def post(url, **kwargs):
        if url.endswith("/api/user/login"):
            return _post_login_fake(url, **kwargs)
        if url.endswith("/api/device/control"):
            return FakeResponse("<response>ERROR</response>")
        raise AssertionError(f"unexpected POST {url}")

    monkeypatch.setattr(requests, "get", _login_fake)
    monkeypatch.setattr(requests, "post", post)
    modem = HuaweiModem("192.168.8.1", "admin", "admin")
    with pytest.raises(HuaweiModemError):
        modem.reboot()


def test_set_mobile_data(monkeypatch):
    calls = []

    def post(url, **kwargs):
        if url.endswith("/api/user/login"):
            return _post_login_fake(url, **kwargs)
        if url.endswith("/api/dialup/mobile-dataswitch"):
            calls.append(kwargs["data"])
            return FakeResponse("OK")
        raise AssertionError(f"unexpected POST {url}")

    monkeypatch.setattr(requests, "get", _login_fake)
    monkeypatch.setattr(requests, "post", post)
    modem = HuaweiModem("192.168.8.1", "admin", "admin")
    modem.set_mobile_data(False)
    modem.set_mobile_data(True)
    assert calls == [
        "<request><dataswitch>0</dataswitch></request>",
        "<request><dataswitch>1</dataswitch></request>",
    ]
