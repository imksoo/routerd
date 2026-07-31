import importlib.util
import json
import os
from pathlib import Path
import pwd
import shutil
import socket
import subprocess
import tempfile
import threading
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
            self.assertEqual(proxy.resolve_public_ipv4("api.example"), ["8.8.8.8"])
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

    def test_tracked_units_bound_proxy_outside_main_lifecycle(self):
        proxy_unit = (ROOT / "supervisor/routerd-release-qa-egress-proxy@.service").read_text()
        main_unit = (ROOT / "supervisor/routerd-release-qa@.service").read_text()
        prepare_unit = (ROOT / "supervisor/routerd-release-qa-prepare@.service").read_text()
        self.assertIn("RuntimeMaxSec=4500", proxy_unit)
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
        self.assertIn('runtime/evidence/egress-proxy', source)
        self.assertNotIn('routerd-release-qa@$run_id', source)
        self.assertNotIn('inventory-driver.sh', source)
        self.assertNotIn('finalize-sealed-provider-auth.sh', source)

    def test_documented_outer_order_preserves_existing_qa_contract(self):
        docs = (ROOT / "README.md").read_text()
        self.assertLess(docs.index('manage-egress-proxy.sh start'), docs.index('authoritative baseline zero'))
        self.assertLess(docs.index('bounded final inventory'), docs.index('manage-egress-proxy.sh stop'))
        self.assertLess(docs.index('manage-egress-proxy.sh stop'), docs.index('finalizer'))

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
            self.assertTrue((runtime / "evidence/egress-proxy").is_dir())
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

    def test_manager_rejects_external_or_occupied_endpoint(self):
        source = (ROOT / "drivers/manage-egress-proxy.sh").read_text()
        self.assertIn('httpsProxy is not an IPv4 loopback endpoint', source)
        self.assertIn('configured proxy port is already listening', source)


if __name__ == "__main__":
    unittest.main()
