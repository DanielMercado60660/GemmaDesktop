#!/usr/bin/env python3
"""Operate the local Harness Lab through its application API.

This is a packet transport, not a model client or proposal generator. Run an
isolated app with --serve 127.0.0.1:PORT --data-dir PATH, then use
`call METHOD --params JSON_FILE` or `packet RESPONSE_FILE`.
All model execution, validation, grading and persistence remain owned by Go.
"""
import argparse
import json
import pathlib
import sys
import urllib.error
import urllib.parse
import urllib.request


def call(base, method, params):
    endpoint = urllib.parse.urlsplit(base)
    if (endpoint.scheme != "http" or endpoint.hostname not in ("127.0.0.1", "localhost", "::1")
            or endpoint.username or endpoint.password or endpoint.path not in ("", "/")
            or endpoint.query or endpoint.fragment):
        raise ValueError("Use an explicit loopback HTTP application endpoint")
    payload = json.dumps({"method": method, "params": params}).encode()
    if len(payload) > 2 * 1024 * 1024:
        raise ValueError("Request exceeds 2 MiB")
    request = urllib.request.Request(base.rstrip("/") + "/api/call", data=payload,
                                     headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError(error.read(32768).decode(errors="replace")) from error


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:8799")
    parser.add_argument("--output", type=pathlib.Path)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("status")
    invoke = commands.add_parser("call")
    invoke.add_argument("method")
    invoke.add_argument("--params", type=pathlib.Path)
    packet = commands.add_parser("packet")
    packet.add_argument("response", type=pathlib.Path)
    args = parser.parse_args()
    if args.command == "status":
        method, params = "harness.status", {}
    elif args.command == "packet":
        method, params = "harness.packets.submit", json.loads(args.response.read_text())
    else:
        if not args.method.startswith("harness."):
            parser.error("This helper exposes only Harness Lab operations")
        method = args.method
        params = json.loads(args.params.read_text()) if args.params else {}
    if not isinstance(params, dict):
        parser.error("Parameters must be a JSON object")
    result = call(args.url, method, params)
    text = json.dumps(result, indent=2, ensure_ascii=False) + "\n"
    if args.output:
        args.output.write_text(text)
        print(str(args.output.resolve()))
    else:
        print(text, end="")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
