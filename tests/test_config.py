import pytest

from ipflip.config import Config

ALL_ENV = [
    "MODEM_IP",
    "MODEM_USER",
    "MODEM_PASS",
    "MODEM_INTERFACE",
    "LOCAL_IP",
    "ROUTING_TABLE",
    "PROXY_HOST",
    "PROXY_PORT",
    "IP_CHECK_URL",
    "ROTATION_MODE",
    "REBOOT_WAIT_SECONDS",
    "TOGGLE_WAIT_SECONDS",
    "CONNECT_ATTEMPTS",
    "CONNECT_RETRY_SECONDS",
    "ROTATION_HOST",
    "ROTATION_PORT",
    "AUTH_TOKEN",
    "USE_SUDO",
]


def _clean_env(monkeypatch):
    for key in ALL_ENV:
        monkeypatch.delenv(key, raising=False)


def test_defaults(tmp_path, monkeypatch):
    _clean_env(monkeypatch)
    env_file = tmp_path / ".env"
    env_file.write_text("")
    config = Config.load(env_file)
    assert config.modem_ip == "192.168.8.1"
    assert config.modem_user == "admin"
    assert config.modem_pass == ""
    assert config.rotation_mode == "reboot"
    assert config.rotation_port == 5000
    assert config.proxy_port == 3128
    assert config.connect_attempts == 15
    assert config.use_sudo is True


def test_env_file_values(tmp_path, monkeypatch):
    _clean_env(monkeypatch)
    env_file = tmp_path / ".env"
    env_file.write_text(
        "MODEM_IP=10.0.0.1\n"
        "MODEM_PASS=s3cret\n"
        "ROTATION_MODE=toggle\n"
        "CONNECT_ATTEMPTS=7\n"
        "USE_SUDO=false\n"
    )
    config = Config.load(env_file)
    assert config.modem_ip == "10.0.0.1"
    assert config.modem_pass == "s3cret"
    assert config.rotation_mode == "toggle"
    assert config.connect_attempts == 7
    assert config.use_sudo is False


def test_real_environment_wins_over_file(tmp_path, monkeypatch):
    _clean_env(monkeypatch)
    env_file = tmp_path / ".env"
    env_file.write_text("MODEM_IP=10.0.0.1\n")
    monkeypatch.setenv("MODEM_IP", "10.9.9.9")
    config = Config.load(env_file)
    assert config.modem_ip == "10.9.9.9"


def test_invalid_rotation_mode(tmp_path, monkeypatch):
    _clean_env(monkeypatch)
    env_file = tmp_path / ".env"
    env_file.write_text("ROTATION_MODE=warp\n")
    with pytest.raises(ValueError):
        Config.load(env_file)


def test_missing_env_file_is_a_noop(tmp_path, monkeypatch):
    _clean_env(monkeypatch)
    config = Config.load(tmp_path / "does-not-exist.env")
    assert config.modem_ip == "192.168.8.1"
