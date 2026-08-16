"""Command-line interface for ipflip."""

from __future__ import annotations

import argparse
import logging
import sys

from .config import Config
from .rotation import Rotator


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="ipflip",
        description="Force a new public IP by rebooting or toggling a Huawei "
        "4G/5G modem.",
    )
    parser.add_argument(
        "--env-file",
        metavar="PATH",
        help="Path to an .env file (default: ./.env)",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser(
        "rotate", help="Run a single IP rotation and print old/new IPs."
    )
    sub.add_parser("serve", help="Start the HTTP rotation service.")
    sub.add_parser("reboot", help="Reboot the Huawei modem.")
    sub.add_parser("toggle", help="Toggle the modem mobile data off/on.")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _build_parser().parse_args(argv)
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s"
    )

    try:
        config = Config.load(args.env_file)
    except ValueError as exc:
        print(f"Configuration error: {exc}", file=sys.stderr)
        return 2

    if not config.modem_pass:
        print(
            "Configuration error: MODEM_PASS is not set "
            "(see .env.example).",
            file=sys.stderr,
        )
        return 2

    rotator = Rotator(config)
    try:
        if args.command == "rotate":
            old_ip, new_ip = rotator.rotate()
            print(f"Old IP: {old_ip or 'unknown'}")
            print(f"New IP: {new_ip}")
        elif args.command == "serve":
            from .server import serve

            serve(config)
        elif args.command == "reboot":
            rotator.reboot_modem()
            print("Reboot command sent to the modem.")
        elif args.command == "toggle":
            rotator.toggle_mobile_data()
            print("Mobile data toggled.")
    except Exception as exc:  # noqa: BLE001 - top-level CLI handler
        print(f"Error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
