#!/usr/bin/env python3
"""Small IPv4-only CONNECT proxy for the run-confined release-QA service."""

from __future__ import annotations

import argparse
import errno
import ipaddress
import json
import os
from pathlib import Path
import selectors
import signal
import socket
import socketserver
import sys
import subprocess
import threading
import time


MAX_HEADER = 16 * 1024
CONNECT_TIMEOUT = 10.0
IDLE_TIMEOUT = 60.0
MAX_HANDLERS = 16
MAX_DNS_ADDRESSES = 32
CANCEL_POLL = 0.05


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


def lookup_public_ipv4(host: str) -> list[str]:
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
            if len(addresses) == MAX_DNS_ADDRESSES:
                break
    return addresses


def resolve_public_ipv4(host: str, *, deadline: float | None = None,
                        stopping: threading.Event | None = None) -> list[str]:
    # getaddrinfo has no portable cancellation API. Isolate only that blocking
    # operation in an owned child; timeout/cancellation kill and reap the child.
    deadline = deadline if deadline is not None else time.monotonic() + CONNECT_TIMEOUT
    if stopping is not None and stopping.is_set():
        raise InterruptedError("proxy is stopping")
    process = subprocess.Popen(
        [sys.executable, str(Path(__file__).resolve()), "--resolve-ipv4", host],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
        close_fds=True,
    )
    try:
        while True:
            if stopping is not None and stopping.is_set():
                raise InterruptedError("proxy is stopping")
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("upstream resolution deadline expired")
            try:
                output, _ = process.communicate(timeout=min(CANCEL_POLL, remaining))
                break
            except subprocess.TimeoutExpired:
                continue
        if process.returncode != 0 or len(output) > 1024:
            raise OSError("upstream resolver failed")
        try:
            addresses = json.loads(output)
            if (not isinstance(addresses, list) or len(addresses) > MAX_DNS_ADDRESSES
                    or any(not isinstance(address, str) or not allowed_upstream_ipv4(address)
                           for address in addresses)):
                raise ValueError("invalid resolver result")
        except (ValueError, UnicodeDecodeError) as exc:
            raise OSError("upstream resolver returned invalid IPv4 addresses") from exc
        return addresses
    finally:
        if process.poll() is None:
            process.kill()
        process.wait()  # Actual child exit is required before releasing admission.
        if process.stdout is not None:
            process.stdout.close()


def connect_ipv4(host: str, *, stopping: threading.Event | None = None) -> socket.socket:
    last_error: OSError | None = None
    deadline = time.monotonic() + CONNECT_TIMEOUT
    for address in resolve_public_ipv4(host, deadline=deadline, stopping=stopping):
        if stopping is not None and stopping.is_set():
            raise InterruptedError("proxy is stopping")
        if deadline <= time.monotonic():
            raise TimeoutError("upstream connection deadline expired")
        upstream = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            upstream.setblocking(False)
            error = upstream.connect_ex((address, 443))
            if error not in {0, errno.EINPROGRESS, errno.EWOULDBLOCK, errno.EALREADY}:
                raise OSError(error, os.strerror(error))
            with selectors.DefaultSelector() as selector:
                selector.register(upstream, selectors.EVENT_WRITE)
                while error:
                    if stopping is not None and stopping.is_set():
                        raise InterruptedError("proxy is stopping")
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise TimeoutError("upstream connection deadline expired")
                    if selector.select(min(CANCEL_POLL, remaining)):
                        error = upstream.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR)
                        if error:
                            raise OSError(error, os.strerror(error))
            upstream.settimeout(None)
            return upstream
        except OSError as exc:
            last_error = exc
            upstream.close()
    if last_error is not None:
        raise last_error
    raise OSError("target has no public IPv4 address")


def relay(left: socket.socket, right: socket.socket, initial: bytes = b"") -> None:
    # Read readiness does not imply that the other endpoint will accept bytes.
    # Bound the complete sendall, including a stalled reader's backpressure.
    left.settimeout(CONNECT_TIMEOUT)
    right.settimeout(CONNECT_TIMEOUT)
    if initial:
        right.sendall(initial)
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
        deadline = time.monotonic() + CONNECT_TIMEOUT
        header = bytearray()
        try:
            while b"\r\n\r\n" not in header and len(header) < MAX_HEADER:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return
                self.request.settimeout(remaining)
                chunk = self.request.recv(4096)
                if not chunk:
                    return
                header.extend(chunk)
        except OSError:
            return
        self.request.settimeout(CONNECT_TIMEOUT)
        boundary = header.find(b"\r\n\r\n")
        if boundary < 0 or boundary + 4 > MAX_HEADER:
            self.request.sendall(b"HTTP/1.1 431 Request Header Fields Too Large\r\n\r\n")
            return
        initial = bytes(header[boundary + 4:])
        header = header[:boundary + 4]
        try:
            request_line = bytes(header).split(b"\r\n", 1)[0].decode("ascii")
            method, authority, version = request_line.split(" ")
            host, port_text = authority.rsplit(":", 1)
            if method != "CONNECT" or version not in {"HTTP/1.0", "HTTP/1.1"} or int(port_text) != 443:
                raise ValueError
            if (not host or any(character in host for character in "[]/@\\")
                    or any(ord(character) < 33 or ord(character) == 127 for character in host)):
                raise ValueError
        except (UnicodeDecodeError, ValueError):
            self.request.sendall(b"HTTP/1.1 400 Bad Request\r\n\r\n")
            return
        established = False
        try:
            with connect_ipv4(host, stopping=self.server.stopping) as upstream:
                self.server.track_socket(upstream)
                try:
                    self.request.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                    established = True
                    relay(self.request, upstream, initial)
                finally:
                    self.server.untrack_socket(upstream)
        except OSError:
            if not established and not self.server.stopping.is_set():
                try:
                    self.request.sendall(b"HTTP/1.1 502 Bad Gateway\r\n\r\n")
                except OSError:
                    pass


class IPv4ThreadingServer(socketserver.ThreadingTCPServer):
    address_family = socket.AF_INET
    daemon_threads = False
    block_on_close = True
    allow_reuse_address = True

    def __init__(self, address, handler, *, max_handlers: int = MAX_HANDLERS):
        if max_handlers < 1:
            raise ValueError("proxy handler limit must be positive")
        self.stopping = threading.Event()
        self._admission = threading.BoundedSemaphore(max_handlers)
        self._sockets: set[socket.socket] = set()
        self._socket_lock = threading.Lock()
        super().__init__(address, handler)

    def track_socket(self, connection: socket.socket) -> None:
        with self._socket_lock:
            if self.stopping.is_set():
                raise InterruptedError("proxy is stopping")
            self._sockets.add(connection)

    def untrack_socket(self, connection: socket.socket) -> None:
        with self._socket_lock:
            self._sockets.discard(connection)

    def process_request(self, request, client_address) -> None:
        if not self._admission.acquire(blocking=False):
            self.shutdown_request(request)
            return
        # Serialize admission/thread registration against close/join. No worker
        # can start after shutdown has taken its complete socket snapshot.
        with self._socket_lock:
            if self.stopping.is_set():
                self._admission.release()
                self.shutdown_request(request)
                return
            self._sockets.add(request)
            try:
                super().process_request(request, client_address)
            except BaseException:
                self._sockets.discard(request)
                self._admission.release()
                raise

    def process_request_thread(self, request, client_address) -> None:
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.untrack_socket(request)
            self._admission.release()

    def cancel_requests(self) -> None:
        with self._socket_lock:
            self.stopping.set()
            for connection in self._sockets:
                try:
                    connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass

    def shutdown(self) -> None:
        self.cancel_requests()
        super().shutdown()

    def server_close(self) -> None:
        self.cancel_requests()
        super().server_close()  # Join every admitted handler and its resolver child.


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
        shutdown_worker: threading.Thread | None = None

        def stop(_signum, _frame):
            nonlocal shutdown_worker
            if shutdown_worker is None:
                shutdown_worker = threading.Thread(target=server.shutdown)
                shutdown_worker.start()
        signal.signal(signal.SIGTERM, stop)
        signal.signal(signal.SIGINT, stop)
        notify_systemd(b"READY=1\nSTATUS=IPv4 CONNECT proxy ready")
        server.serve_forever(poll_interval=0.2)
    if shutdown_worker is not None:
        shutdown_worker.join()
    write_status(status_path, "stopped", args.port)
    notify_systemd(b"STOPPING=1\nSTATUS=IPv4 CONNECT proxy stopped")
    return 0


if __name__ == "__main__":
    try:
        if len(sys.argv) == 3 and sys.argv[1] == "--resolve-ipv4":
            print(json.dumps(lookup_public_ipv4(sys.argv[2])))
            raise SystemExit(0)
        raise SystemExit(main(sys.argv[1:]))
    except (OSError, ValueError) as exc:
        print(f"release QA egress proxy: {exc}", file=sys.stderr)
        raise SystemExit(2)
