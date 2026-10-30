import importlib.util
import json
import os
from pathlib import Path
import pwd
import shutil
import signal
import socket
import socketserver
import subprocess
import tempfile
import threading
import time
import sys
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("egress_proxy", ROOT / "egress_proxy.py")
proxy = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(proxy)


class EgressProxyTests(unittest.TestCase):
    def test_resolution_is_ipv4_only_and_rejects_nonpublic_answers(self):
        answers = [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("127.0.0.1", 443)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("203.0.113.10", 443)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("224.0.0.1", 443)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("239.1.1.1", 443)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("8.8.8.8", 443)),
        ]
        with mock.patch.object(socket, "getaddrinfo", return_value=answers) as lookup:
            self.assertEqual(proxy.lookup_public_ipv4("api.example"), ["8.8.8.8"])
        lookup.assert_called_once_with("api.example", 443, socket.AF_INET, socket.SOCK_STREAM)
        for rejected in ("0.0.0.0", "10.0.0.1", "127.0.0.1", "169.254.1.1",
                         "224.0.0.1", "239.1.1.1", "240.0.0.1", "255.255.255.255"):
            with self.subTest(rejected=rejected):
                self.assertFalse(proxy.allowed_upstream_ipv4(rejected))

    def test_server_listens_on_ipv4_loopback_only(self):
        with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
            self.assertEqual(server.address_family, socket.AF_INET)
            self.assertEqual(server.server_address[0], "127.0.0.1")

    def test_non_connect_and_non_443_requests_never_open_upstream(self):
        with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
            worker = threading.Thread(target=server.handle_request)
            worker.start()
            with socket.create_connection(server.server_address) as client:
                client.sendall(b"CONNECT example.com:80 HTTP/1.1\r\n\r\n")
                self.assertIn(b"400 Bad Request", client.recv(1024))
            worker.join(timeout=2)
            self.assertFalse(worker.is_alive())

    def test_connect_tunnels_bytes_over_real_sockets(self):
        upstream_for_proxy, upstream_for_test = socket.socketpair()
        with mock.patch.object(proxy, "connect_ipv4", return_value=upstream_for_proxy):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.handle_request)
                worker.start()
                with socket.create_connection(server.server_address) as client:
                    client.sendall(b"CONNECT api.example:443 HTTP/1.1\r\nHost: api.example\r\n\r\n")
                    self.assertIn(b"200 Connection Established", client.recv(1024))
                    client.sendall(b"tunnel-payload")
                    self.assertEqual(upstream_for_test.recv(1024), b"tunnel-payload")
                upstream_for_test.close()
                worker.join(timeout=2)
                self.assertFalse(worker.is_alive())

    def test_upstream_failure_returns_502(self):
        with mock.patch.object(proxy, "connect_ipv4", side_effect=OSError("fixture")):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.handle_request)
                worker.start()
                with socket.create_connection(server.server_address) as client:
                    client.sendall(b"CONNECT api.example:443 HTTP/1.1\r\n\r\n")
                    self.assertIn(b"502 Bad Gateway", client.recv(1024))
                worker.join(timeout=2)
                self.assertFalse(worker.is_alive())

    def test_connect_preserves_bytes_received_with_the_header(self):
        upstream_for_proxy, upstream_for_test = socket.socketpair()
        upstream_for_test.settimeout(1)
        with upstream_for_test, mock.patch.object(proxy, "connect_ipv4", return_value=upstream_for_proxy):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.handle_request)
                worker.start()
                with socket.create_connection(server.server_address) as client:
                    client.settimeout(1)
                    client.sendall(b"CONNECT api.example:443 HTTP/1.1\r\n\r\npipelined-tls")
                    self.assertIn(b"200 Connection Established", client.recv(1024))
                    self.assertEqual(upstream_for_test.recv(1024), b"pipelined-tls")
                worker.join(timeout=2)
                self.assertFalse(worker.is_alive())

    def test_slow_header_has_one_deadline(self):
        with mock.patch.object(proxy, "CONNECT_TIMEOUT", 0.08):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.handle_request)
                worker.start()
                with socket.create_connection(server.server_address) as client:
                    client.settimeout(0.5)
                    stopped = threading.Event()

                    def drip():
                        while not stopped.wait(0.01):
                            try:
                                client.sendall(b"C")
                            except OSError:
                                return

                    writer = threading.Thread(target=drip)
                    writer.start()
                    try:
                        self.assertEqual(client.recv(1024), b"")
                    finally:
                        stopped.set()
                        writer.join(timeout=1)
                worker.join(timeout=1)
                self.assertFalse(worker.is_alive())

    def test_relay_stops_when_destination_does_not_read(self):
        left, client = socket.socketpair()
        right, upstream = socket.socketpair()
        errors = []
        right.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)

        def transfer():
            try:
                proxy.relay(left, right)
            except OSError as exc:
                errors.append(exc)

        with left, client, right, upstream, mock.patch.object(proxy, "CONNECT_TIMEOUT", 0.08):
            worker = threading.Thread(target=transfer)
            worker.start()
            try:
                client.settimeout(1)
                client.sendall(b"x" * (64 * 1024))
                worker.join(timeout=1)
                self.assertFalse(worker.is_alive(), "relay blocked on an unread destination")
                self.assertEqual(len(errors), 1)
                self.assertIsInstance(errors[0], TimeoutError)
            finally:
                client.close()
                upstream.close()
                worker.join(timeout=1)

    def test_tunnel_failure_does_not_inject_an_http_error_after_success(self):
        upstream_for_proxy, upstream_for_test = socket.socketpair()
        with upstream_for_test, mock.patch.object(proxy, "connect_ipv4", return_value=upstream_for_proxy), \
                mock.patch.object(proxy, "relay", side_effect=TimeoutError("stalled write")):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.handle_request)
                worker.start()
                with socket.create_connection(server.server_address) as client:
                    client.settimeout(1)
                    client.sendall(b"CONNECT api.example:443 HTTP/1.1\r\n\r\n")
                    response = bytearray()
                    while chunk := client.recv(1024):
                        response.extend(chunk)
                    self.assertIn(b"200 Connection Established", response)
                    self.assertNotIn(b"502 Bad Gateway", response)
                worker.join(timeout=1)
                self.assertFalse(worker.is_alive())

    def test_resolver_process_returns_filtered_numeric_address(self):
        self.assertEqual(proxy.resolve_public_ipv4("8.8.8.8"), ["8.8.8.8"])
        self.assertEqual(proxy.resolve_public_ipv4("127.0.0.1"), [])

    def test_blocked_resolver_is_killed_and_reaped_at_deadline(self):
        real_popen = subprocess.Popen
        children = []

        def blocked_resolver(*_args, **kwargs):
            child = real_popen([sys.executable, "-c", "import time; time.sleep(30)"], **kwargs)
            children.append(child)
            return child

        try:
            with mock.patch.object(proxy, "CONNECT_TIMEOUT", 0.08), \
                    mock.patch.object(proxy.subprocess, "Popen", side_effect=blocked_resolver):
                started = time.monotonic()
                with self.assertRaises(TimeoutError):
                    proxy.resolve_public_ipv4("api.example")
                self.assertLess(time.monotonic() - started, 1)
            self.assertEqual(len(children), 1)
            self.assertIsNotNone(children[0].returncode, "deadline released an unreaped resolver")
            self.assertIsNotNone(children[0].poll())
        finally:
            for child in children:
                if child.poll() is None:
                    child.kill()
                child.wait()

    def test_shutdown_cancels_dns_and_joins_handler_and_resolver(self):
        real_popen = subprocess.Popen
        children = []
        launched = threading.Event()

        def blocked_resolver(*_args, **kwargs):
            child = real_popen([sys.executable, "-c", "import time; time.sleep(30)"], **kwargs)
            children.append(child)
            launched.set()
            return child

        with mock.patch.object(proxy, "CONNECT_TIMEOUT", 30), \
                mock.patch.object(proxy.subprocess, "Popen", side_effect=blocked_resolver):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01})
                worker.start()
                try:
                    with socket.create_connection(server.server_address) as client:
                        client.settimeout(1)
                        client.sendall(b"CONNECT api.example:443 HTTP/1.1\r\n\r\n")
                        self.assertTrue(launched.wait(1), "resolver child was never started")
                        started = time.monotonic()
                        server.shutdown()
                        server.server_close()
                        self.assertLess(time.monotonic() - started, 1)
                        self.assertEqual(client.recv(1024), b"")
                    self.assertIsNotNone(children[0].returncode)
                    self.assertFalse(server._sockets, "shutdown left an admitted socket")
                finally:
                    server.shutdown()
                    worker.join(timeout=1)
                    for child in children:
                        if child.poll() is None:
                            child.kill()
                        child.wait()
                self.assertFalse(worker.is_alive())

    def test_handler_admission_is_bounded_and_slot_is_reused(self):
        active = 0
        maximum = 0
        changed = threading.Condition()

        class CountingHandler(socketserver.BaseRequestHandler):
            def handle(self):
                nonlocal active, maximum
                with changed:
                    active += 1
                    maximum = max(maximum, active)
                    changed.notify_all()
                try:
                    self.request.recv(1)
                finally:
                    with changed:
                        active -= 1
                        changed.notify_all()

        with proxy.IPv4ThreadingServer(("127.0.0.1", 0), CountingHandler) as server:
            worker = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01})
            worker.start()
            clients = []
            try:
                for count in range(proxy.MAX_HANDLERS):
                    clients.append(socket.create_connection(server.server_address, timeout=2))
                    with changed:
                        self.assertTrue(changed.wait_for(lambda: active == count + 1, timeout=2))
                with changed:
                    self.assertTrue(changed.wait_for(lambda: active == proxy.MAX_HANDLERS, timeout=1))
                with socket.create_connection(server.server_address, timeout=1) as rejected:
                    rejected.settimeout(1)
                    self.assertEqual(rejected.recv(1), b"", "overload created another blocked handler")
                clients.pop().close()
                with changed:
                    self.assertTrue(changed.wait_for(lambda: active == proxy.MAX_HANDLERS - 1, timeout=1))
                # Join the finished handler so its admission release is observed.
                deadline = time.monotonic() + 1
                while len(server._sockets) != proxy.MAX_HANDLERS - 1 and time.monotonic() < deadline:
                    time.sleep(0.001)
                clients.append(socket.create_connection(server.server_address, timeout=1))
                with changed:
                    self.assertTrue(changed.wait_for(lambda: active == proxy.MAX_HANDLERS, timeout=1))
                self.assertEqual(maximum, proxy.MAX_HANDLERS)
            finally:
                for client in clients:
                    client.close()
                server.shutdown()
                worker.join(timeout=1)
            self.assertFalse(worker.is_alive())
        self.assertEqual(active, 0)

    def test_shutdown_interrupts_established_backpressured_tunnel(self):
        upstream_for_proxy, upstream_for_test = socket.socketpair()
        upstream_for_proxy.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        with upstream_for_test, mock.patch.object(proxy, "connect_ipv4", return_value=upstream_for_proxy):
            with proxy.IPv4ThreadingServer(("127.0.0.1", 0), proxy.ConnectHandler) as server:
                worker = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01})
                worker.start()
                try:
                    with socket.create_connection(server.server_address) as client:
                        client.settimeout(1)
                        client.sendall(b"CONNECT api.example:443 HTTP/1.1\r\n\r\n")
                        self.assertIn(b"200 Connection Established", client.recv(1024))
                        client.sendall(b"x" * (64 * 1024))
                        started = time.monotonic()
                        server.shutdown()
                        server.server_close()
                        self.assertLess(time.monotonic() - started, 1)
                    self.assertFalse(server._sockets)
                finally:
                    server.shutdown()
                    worker.join(timeout=1)
                self.assertFalse(worker.is_alive())

    def test_executable_sigterm_reaps_resolver_before_stopped_status(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            runs = root / "runs"
            run_root = runs / "proxy-lifetime"
            marker = root / "resolver.pid"
            script = root / "egress_proxy.py"
            source = (ROOT / "egress_proxy.py").read_text()
            source = source.replace('Path("/var/lib/routerd-release-qa")', f"Path({str(runs)!r})")
            # Keep the actual child/entry/signal code. Block only its OS lookup
            # boundary so this test requires neither external DNS nor a provider.
            source = source.replace(
                'def lookup_public_ipv4(host: str) -> list[str]:\n',
                'def lookup_public_ipv4(host: str) -> list[str]:\n'
                '    if host == "blocked.example":\n'
                f'        Path({str(marker)!r}).write_text(str(os.getpid()))\n'
                '        time.sleep(30)\n',
            )
            script.write_text(source)
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as reservation:
                reservation.bind(("127.0.0.1", 0))
                port = reservation.getsockname()[1]
            process = subprocess.Popen(
                [sys.executable, str(script), "--run-root", str(run_root), "--port", str(port)],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
            )
            try:
                status_path = run_root / "runtime/evidence/egress-proxy/status.json"
                deadline = time.monotonic() + 2
                while not status_path.exists() and process.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.01)
                self.assertTrue(status_path.exists(), "proxy did not publish readiness")
                with socket.create_connection(("127.0.0.1", port), timeout=1) as client:
                    client.sendall(b"CONNECT blocked.example:443 HTTP/1.1\r\n\r\n")
                    deadline = time.monotonic() + 2
                    while not marker.exists() and time.monotonic() < deadline:
                        time.sleep(0.01)
                    self.assertTrue(marker.exists(), "resolver never reached the blocked boundary")
                    resolver_pid = int(marker.read_text())
                    self.assertNotEqual(resolver_pid, process.pid)
                    process.send_signal(signal.SIGTERM)
                    stdout, stderr = process.communicate(timeout=2)
                    self.assertEqual(process.returncode, 0, (stdout, stderr))
                    self.assertEqual(json.loads(status_path.read_text())["status"], "stopped")
                    with self.assertRaises(ProcessLookupError):
                        os.kill(resolver_pid, 0)
            finally:
                # The fixture owns a private process group, including a resolver
                # if a regression prevents the parent from acknowledging stop.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.communicate(timeout=2)

    def test_tracked_units_bound_proxy_outside_main_lifecycle(self):
        proxy_unit = (ROOT / "supervisor/routerd-release-qa-egress-proxy@.service").read_text()
        main_unit = (ROOT / "supervisor/routerd-release-qa@.service").read_text()
        prepare_unit = (ROOT / "supervisor/routerd-release-qa-prepare@.service").read_text()
        self.assertIn("RuntimeMaxSec=9600", proxy_unit)  # 145 paid + 15 outer minutes.
        self.assertIn("Restart=no", proxy_unit)
        self.assertNotIn("StartLimitIntervalSec=0", proxy_unit)
        self.assertIn("RestrictAddressFamilies=AF_INET AF_UNIX", proxy_unit)
        self.assertIn("Type=notify", proxy_unit)
        self.assertIn("TimeoutStartSec=10s", proxy_unit)
        self.assertIn("WantedBy=multi-user.target", proxy_unit)
        self.assertIn("BindsTo=routerd-release-qa-egress-proxy@%i.service", main_unit)
        self.assertIn("Requires=routerd-release-qa-egress-proxy@%i.service", prepare_unit)
        self.assertNotIn("PartOf=routerd-release-qa@%i.service", proxy_unit)

    def test_manager_owns_only_proxy_start_readiness_and_exact_stop(self):
        source = (ROOT / "drivers/manage-egress-proxy.sh").read_text()
        enable = source.index('systemctl enable --now "$unit"')
        self.assertLess(enable, source.index('systemctl is-active "$unit"', enable))
        self.assertLess(source.index('systemctl disable --now "$unit"'),
                        source.rindex('proxy socket remained after unit stop'))
        self.assertIn('evidence_root="$run_root/runtime/evidence"', source)
        self.assertIn('require_private_service_directory "$evidence_root"', source)
        self.assertNotIn('routerd-release-qa@$run_id', source)
        self.assertNotIn('inventory-driver.sh', source)
        self.assertNotIn('finalize-sealed-provider-auth.sh', source)

    def test_documented_outer_order_preserves_existing_qa_contract(self):
        docs = (ROOT / "README.md").read_text()
        self.assertLess(docs.index('manage-egress-proxy.sh start'), docs.index('authoritative baseline zero'))
        self.assertLess(docs.index('bounded final inventory'), docs.index('manage-egress-proxy.sh stop'))
        self.assertLess(docs.index('manage-egress-proxy.sh stop'), docs.rindex('finalizer'))

    def test_manager_selects_atomic_endpoint_waits_ready_and_stops_exact_unit(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            runs = root / "runs"
            etc = root / "etc"
            run_id = "relqa-staging-fixture"
            run_root = runs / run_id
            framework = run_root / "repo/tools/release-qa-labs"
            runtime = run_root / "runtime"
            framework.mkdir(parents=True)
            runtime.mkdir()
            source_unit = ROOT / "supervisor/routerd-release-qa-egress-proxy@.service"
            tracked_unit = framework / "supervisor/routerd-release-qa-egress-proxy@.service"
            tracked_unit.parent.mkdir()
            shutil.copy2(source_unit, tracked_unit)
            installed_unit = etc / "routerd-release-qa-egress-proxy@.service"
            etc.mkdir()
            shutil.copy2(source_unit, installed_unit)
            run_env = runtime / "run.env.json"
            run_env.write_text(json.dumps({"releaseRepo": str(run_root / "repo")}), encoding="utf-8")
            run_env.chmod(0o600)
            fake_bin = root / "bin"
            fake_bin.mkdir()
            calls = root / "calls"
            state = root / "state"
            (fake_bin / "systemctl").write_text(
                '#!/bin/sh\necho "$*" >>"$CALLS"\n'
                'case "$1" in enable) echo active >"$STATE"; [ "${FAIL_ENABLE:-}" != 1 ];; '
                'disable) echo inactive >"$STATE";; '
                'is-active) cat "$STATE" 2>/dev/null || echo inactive;; esac\n', encoding="utf-8")
            (fake_bin / "ss").write_text(
                '#!/bin/sh\n[ "${OCCUPIED:-}" = 1 ] && echo "LISTEN fixture"\nexit 0\n', encoding="utf-8")
            for command in ("systemctl", "ss"):
                (fake_bin / command).chmod(0o755)
            script = root / "manage.sh"
            text = (ROOT / "drivers/manage-egress-proxy.sh").read_text()
            text = text.replace('[ "$(id -u)" -eq 0 ]', '[ 1 -eq 1 ]')
            text = text.replace("/var/lib/routerd-release-qa", str(runs))
            text = text.replace("/etc/systemd/system", str(etc))
            text = text.replace("service_user=routerd-release-qa", f"service_user={pwd.getpwuid(os.getuid()).pw_name}")
            script.write_text(text, encoding="utf-8")
            script.chmod(0o755)
            environment = os.environ.copy()
            environment.update(PATH=f"{fake_bin}:{environment['PATH']}", CALLS=str(calls), STATE=str(state))
            started = subprocess.run([script, "start", run_id], text=True, capture_output=True,
                                     env=environment, check=False)
            self.assertEqual(started.returncode, 0, started.stderr)
            endpoint = json.loads(run_env.read_text())["httpsProxy"]
            self.assertRegex(endpoint, r"^http://127\.0\.0\.1:18[0-9]{3}$")
            self.assertEqual(run_env.stat().st_mode & 0o777, 0o600)
            self.assertEqual((runtime / "evidence").stat().st_mode & 0o777, 0o700)
            self.assertTrue((runtime / "evidence/egress-proxy").is_dir())
            self.assertEqual((runtime / "evidence/egress-proxy").stat().st_mode & 0o777, 0o700)
            stopped = subprocess.run([script, "stop", run_id], text=True, capture_output=True,
                                     env=environment, check=False)
            self.assertEqual(stopped.returncode, 0, stopped.stderr)
            log = calls.read_text()
            self.assertIn("enable --now routerd-release-qa-egress-proxy@", log)
            self.assertIn("disable --now routerd-release-qa-egress-proxy@", log)
            failed_environment = environment.copy()
            failed_environment["FAIL_ENABLE"] = "1"
            failed = subprocess.run([script, "start", run_id], text=True, capture_output=True,
                                    env=failed_environment, check=False)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(state.read_text().strip(), "inactive")
            self.assertGreaterEqual(calls.read_text().count("disable --now"), 2)
            occupied_environment = environment.copy()
            occupied_environment["OCCUPIED"] = "1"
            before = calls.read_text().count("enable --now")
            occupied = subprocess.run([script, "start", run_id], text=True, capture_output=True,
                                      env=occupied_environment, check=False)
            self.assertNotEqual(occupied.returncode, 0)
            self.assertIn("already listening", occupied.stderr)
            self.assertEqual(calls.read_text().count("enable --now"), before)

    def test_manager_rejects_unsafe_preexisting_evidence_root(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            runs = root / "runs"
            etc = root / "etc"
            run_id = "relqa-staging-fixture"
            run_root = runs / run_id
            framework = run_root / "repo/tools/release-qa-labs"
            runtime = run_root / "runtime"
            framework.mkdir(parents=True)
            runtime.mkdir()
            evidence = runtime / "evidence"
            evidence.mkdir(mode=0o755)
            evidence.chmod(0o755)
            source_unit = ROOT / "supervisor/routerd-release-qa-egress-proxy@.service"
            tracked_unit = framework / "supervisor/routerd-release-qa-egress-proxy@.service"
            tracked_unit.parent.mkdir()
            shutil.copy2(source_unit, tracked_unit)
            etc.mkdir()
            shutil.copy2(source_unit, etc / "routerd-release-qa-egress-proxy@.service")
            run_env = runtime / "run.env.json"
            run_env.write_text(json.dumps({"releaseRepo": str(run_root / "repo")}), encoding="utf-8")
            run_env.chmod(0o600)
            fake_bin = root / "bin"
            fake_bin.mkdir()
            for command, content in {
                "systemctl": '#!/bin/sh\nexit 99\n',
                "ss": '#!/bin/sh\nexit 0\n',
            }.items():
                path = fake_bin / command
                path.write_text(content, encoding="utf-8")
                path.chmod(0o755)
            script = root / "manage.sh"
            text = (ROOT / "drivers/manage-egress-proxy.sh").read_text()
            text = text.replace('[ "$(id -u)" -eq 0 ]', '[ 1 -eq 1 ]')
            text = text.replace("/var/lib/routerd-release-qa", str(runs))
            text = text.replace("/etc/systemd/system", str(etc))
            text = text.replace("service_user=routerd-release-qa", f"service_user={pwd.getpwuid(os.getuid()).pw_name}")
            script.write_text(text, encoding="utf-8")
            script.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = f"{fake_bin}:{environment['PATH']}"
            result = subprocess.run([script, "start", run_id], text=True, capture_output=True,
                                    env=environment, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("evidence root is not owned privately", result.stderr)

    def test_manager_rejects_external_or_occupied_endpoint(self):
        source = (ROOT / "drivers/manage-egress-proxy.sh").read_text()
        self.assertIn('httpsProxy is not an IPv4 loopback endpoint', source)
        self.assertIn('configured proxy port is already listening', source)


if __name__ == "__main__":
    unittest.main()
