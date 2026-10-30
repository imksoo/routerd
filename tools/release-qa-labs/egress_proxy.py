#!/usr/bin/env python3
"""Small IPv4-only CONNECT proxy for the run-confined release-QA service."""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
from pathlib import Path
import selectors
import signal
import socket
import socketserver
import sys
import threading
from typing import Any


MAX_HEADER = 16 * 1024
CONNECT_TIMEOUT = 10.0
IDLE_TIMEOUT = 60.0


def notify_systemd(message: bytes) -> None:
    address = os.environ.get("NOTIFY_SOCKET")
    if not address:
        return
    if address.startswith("@"):
        address = "\0" + address[1:]
    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as notifier:
        notifier.connect(address)
        notifier.sendall(message)


def write_status(path: Path, status: str, port: int) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_text(
        json.dumps({"status": status, "pid": os.getpid(), "listen": f"127.0.0.1:{port}"}) + "\n",
        encoding="utf-8",
    )
    os.chmod(temporary, 0o600)
    os.replace(temporary, path)


def allowed_upstream_ipv4(value: str) -> bool:
    address = ipaddress.ip_address(value)
    return (
        isinstance(address, ipaddress.IPv4Address)
        and address.is_global
        and not address.is_multicast
        and not address.is_unspecified
        and not address.is_loopback
        and not address.is_link_local
        and not address.is_private
        and not address.is_reserved
    )


def resolve_public_ipv4(host: str) -> list[str]:
    addresses: list[str] = []
    for family, socktype, proto, _canonical, sockaddr in socket.getaddrinfo(
        host, 443, socket.AF_INET, socket.SOCK_STREAM
    ):
        if family != socket.AF_INET or socktype != socket.SOCK_STREAM:
            continue
        address = sockaddr[0]
        if not allowed_upstream_ipv4(address):
            continue
        if address not in addresses:
            addresses.append(address)
    return addresses


def connect_ipv4(host: str) -> socket.socket:
    last_error: OSError | None = None
    for address in resolve_public_ipv4(host):
        upstream = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        upstream.settimeout(CONNECT_TIMEOUT)
        try:
            upstream.connect((address, 443))
            upstream.settimeout(None)
            return upstream
        except OSError as exc:
            last_error = exc
            upstream.close()
    if last_error is not None:
        raise last_error
    raise OSError("target has no public IPv4 address")


def relay(left: socket.socket, right: socket.socket) -> None:
    selector = selectors.DefaultSelector()
    selector.register(left, selectors.EVENT_READ, right)
    selector.register(right, selectors.EVENT_READ, left)
    try:
        while True:
            events = selector.select(IDLE_TIMEOUT)
            if not events:
                return
            for key, _mask in events:
                data = key.fileobj.recv(64 * 1024)
                if not data:
                    return
                key.data.sendall(data)
    finally:
        selector.close()


class ConnectHandler(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        self.request.settimeout(CONNECT_TIMEOUT)
        header = bytearray()
        while b"\r\n\r\n" not in header and len(header) < MAX_HEADER:
            chunk = self.request.recv(4096)
            if not chunk:
                return
            header.extend(chunk)
        if b"\r\n\r\n" not in header or len(header) >= MAX_HEADER:
            self.request.sendall(b"HTTP/1.1 431 Request Header Fields Too Large\r\n\r\n")
            return
        try:
            request_line = bytes(header).split(b"\r\n", 1)[0].decode("ascii")
            method, authority, version = request_line.split(" ")
            host, port_text = authority.rsplit(":", 1)
            if method != "CONNECT" or version not in {"HTTP/1.0", "HTTP/1.1"} or int(port_text) != 443:
                raise ValueError
            if not host or any(character in host for character in "[]/@\\"):
                raise ValueError
        except (UnicodeDecodeError, ValueError):
            self.request.sendall(b"HTTP/1.1 400 Bad Request\r\n\r\n")
            return
        try:
            with connect_ipv4(host) as upstream:
                self.request.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                self.request.settimeout(None)
                relay(self.request, upstream)
        except OSError:
            self.request.sendall(b"HTTP/1.1 502 Bad Gateway\r\n\r\n")


class IPv4ThreadingServer(socketserver.ThreadingTCPServer):
    address_family = socket.AF_INET
    daemon_threads = True
    allow_reuse_address = True


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-root", type=Path, required=True)
    parser.add_argument("--port", type=int, required=True)
    args = parser.parse_args(argv)
    run_root = args.run_root.resolve()
    expected_parent = Path("/var/lib/routerd-release-qa")
    if run_root.parent != expected_parent or not run_root.name or "/" in run_root.name:
        raise ValueError("run root is not canonical")
    if not 1024 <= args.port <= 65535:
        raise ValueError("proxy port must be in 1024..65535")
    status_path = run_root / "runtime/evidence/egress-proxy/status.json"
    with IPv4ThreadingServer(("127.0.0.1", args.port), ConnectHandler) as server:
        write_status(status_path, "ready", args.port)
        stop = lambda _signum, _frame: threading.Thread(target=server.shutdown, daemon=True).start()
        signal.signal(signal.SIGTERM, stop)
        signal.signal(signal.SIGINT, stop)
        notify_systemd(b"READY=1\nSTATUS=IPv4 CONNECT proxy ready")
        server.serve_forever(poll_interval=0.2)
    write_status(status_path, "stopped", args.port)
    notify_systemd(b"STOPPING=1\nSTATUS=IPv4 CONNECT proxy stopped")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except (OSError, ValueError) as exc:
        print(f"release QA egress proxy: {exc}", file=sys.stderr)
        raise SystemExit(2)
