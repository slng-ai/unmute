"""Offline Agora generated-runtime tests: stdlib lifecycle and real SDK HTTP boundary.

Run with agora-agents installed: python scripts/test_agora_runtime.py [server.py]
No cloud request is sent. The optional path exercises a compiled artifact.
"""

import importlib.machinery
import importlib.util
import json
import os
import sys
import threading
import unittest
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.request import Request, urlopen

DEFAULT = Path(__file__).resolve().parents[1] / "internal/generate/templates/agora_v1/server.py.tmpl"
source = Path(sys.argv.pop(1)) if len(sys.argv) > 1 and not sys.argv[1].startswith("-") else DEFAULT
loader = importlib.machinery.SourceFileLoader("agora_generated_server", str(source))
spec = importlib.util.spec_from_loader(loader.name, loader)
server = importlib.util.module_from_spec(spec)
sys.modules[loader.name] = server
loader.exec_module(server)


class FakeBackend:
    def __init__(self):
        self.started = 0
        self.stopped = []
        self.fail_start = False
        self.fail_stop = False
        self.start_entered = None
        self.start_release = None

    def prepare(self, channel, seconds):
        return object(), {"app_id": "app", "channel": channel, "uid": 1001, "token": "browser-token"}

    def start(self, handle):
        self.started += 1
        if self.start_entered:
            self.start_entered.set()
            if not self.start_release.wait(3):
                raise TimeoutError("test barrier timed out")
        if self.fail_start:
            raise RuntimeError("start failed")
        return "cloud-agent"

    def stop(self, handle, agent_id):
        if self.fail_stop:
            raise RuntimeError("stop failed")
        self.stopped.append(agent_id)


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.now = 10
        self.backend = FakeBackend()
        self.manager = server.SessionManager(self.backend, 120, clock=lambda: self.now)
        self.session = self.manager.prepare()
        self.args = self.session["id"], self.session["key"]

    def tearDown(self):
        self.backend.fail_stop = False
        self.manager.close()

    def drain_cleanup(self):
        for job in list(self.manager.cleanup_jobs.values()):
            job.result(timeout=3)

    def test_prepare_does_not_start_and_identity_is_unique(self):
        other = self.manager.prepare()
        self.assertNotEqual(other["channel"], self.session["channel"])
        self.assertNotEqual(other["key"], self.session["key"])
        self.assertEqual(self.backend.started, 0)

    def test_start_and_duplicate_stop_are_idempotent(self):
        self.manager.start(*self.args)
        self.manager.start(*self.args)
        self.assertEqual(self.backend.started, 1)
        self.manager.stop(*self.args)
        self.manager.stop(*self.args)
        self.assertEqual(self.backend.stopped, ["cloud-agent"])

    def test_wrong_capability_cannot_start_or_stop(self):
        for operation in (self.manager.start, self.manager.stop):
            with self.assertRaises(server.SessionError) as raised:
                operation(self.args[0], "wrong")
            self.assertEqual(raised.exception.status, 404)
        self.assertEqual(self.backend.started, 0)

    def test_failed_start_is_not_reused(self):
        self.backend.fail_start = True
        with self.assertRaises(RuntimeError):
            self.manager.start(*self.args)
        with self.assertRaises(server.SessionError):
            self.manager.start(*self.args)
        self.assertEqual(self.manager.get(*self.args).state, "stopped")

    def test_unused_preparation_expires_without_cloud_start(self):
        self.now += 61
        with self.assertRaises(server.SessionError) as raised:
            self.manager.start(*self.args)
        self.assertEqual(raised.exception.status, 410)
        self.assertEqual(self.backend.started, 0)

    def test_expiry_stops_cloud_and_removes_old_tombstones(self):
        self.manager.start(*self.args)
        self.now += 121
        self.manager.reap()
        self.drain_cleanup()
        self.assertEqual(self.backend.stopped, ["cloud-agent"])
        self.now += 61
        self.manager.reap()
        self.drain_cleanup()
        self.assertNotIn(self.args[0], self.manager.sessions)

    def test_stop_failure_is_retained_and_retried(self):
        self.manager.start(*self.args)
        self.backend.fail_stop = True
        with self.assertRaises(RuntimeError):
            self.manager.stop(*self.args)
        self.assertEqual(self.manager.get(*self.args).state, "cleanup_pending")
        self.backend.fail_stop = False
        self.manager.reap()
        self.drain_cleanup()
        self.assertEqual(self.backend.stopped, ["cloud-agent"])

    def test_stop_waits_for_inflight_start(self):
        self.backend.start_entered = threading.Event()
        self.backend.start_release = threading.Event()
        errors = []

        def call(operation):
            try:
                operation(*self.args)
            except (RuntimeError, server.SessionError) as error:
                errors.append(error)

        start = threading.Thread(target=call, args=(self.manager.start,))
        start.start()
        self.assertTrue(self.backend.start_entered.wait(3))
        stop = threading.Thread(target=call, args=(self.manager.stop,))
        stop.start()
        self.backend.start_release.set()
        start.join(3)
        stop.join(3)
        self.assertFalse(start.is_alive() or stop.is_alive())
        self.assertEqual(errors, [])
        self.assertEqual(self.backend.stopped, ["cloud-agent"])
        self.assertEqual(self.manager.get(*self.args).state, "stopped")

    def test_blocked_cleanup_does_not_delay_other_expired_session(self):
        other = self.manager.prepare()
        self.manager.start(*self.args)
        self.manager.start(other["id"], other["key"])
        blocked = self.manager.get(*self.args).handle
        entered, release, other_stopped = threading.Event(), threading.Event(), threading.Event()

        def stop(handle, _agent_id):
            if handle is blocked:
                entered.set()
                if not release.wait(3):
                    raise TimeoutError("test barrier timed out")
            else:
                other_stopped.set()

        self.now += 121
        with patch.object(self.backend, "stop", side_effect=stop):
            try:
                self.manager.reap()
                self.assertTrue(entered.wait(3))
                self.assertTrue(other_stopped.wait(3))
                # Repeated sweeps must not add a second job for the blocked session.
                blocked_job = self.manager.cleanup_jobs[self.args[0]]
                self.manager.reap()
                self.assertIs(self.manager.cleanup_jobs[self.args[0]], blocked_job)
                self.assertLessEqual(len(self.manager.cleanup_jobs), server.MAX_SESSIONS)
            finally:
                release.set()
                self.drain_cleanup()

    def test_shutdown_stops_active_session_and_refuses_new_one(self):
        self.manager.start(*self.args)
        self.manager.close()
        self.assertEqual(self.backend.stopped, ["cloud-agent"])
        with self.assertRaises(server.SessionError):
            self.manager.prepare()

    def test_capacity_is_released_by_stop(self):
        for _ in range(server.MAX_SESSIONS - 1):
            self.manager.prepare()
        with self.assertRaises(server.SessionError) as raised:
            self.manager.prepare()
        self.assertEqual(raised.exception.status, 429)
        self.manager.stop(*self.args)
        self.manager.prepare()


class HTTPTests(unittest.TestCase):
    def setUp(self):
        self.manager = server.SessionManager(FakeBackend())
        self.http = server.DemoServer(("127.0.0.1", 0), self.manager)
        self.thread = threading.Thread(target=self.http.serve_forever)
        self.thread.start()
        self.base = f"http://127.0.0.1:{self.http.server_port}"

    def tearDown(self):
        self.http.shutdown()
        self.thread.join()
        self.http.server_close()
        self.manager.close()

    def test_untrusted_origin_cannot_create_billable_session(self):
        request = Request(self.base + "/sessions", data=b"{}", headers={"Origin": "https://example.org"})
        with self.assertRaises(HTTPError) as raised:
            urlopen(request)
        self.assertEqual(raised.exception.code, 403)
        self.assertEqual(self.manager.sessions, {})

    def test_no_settings_or_certificate_file_is_served(self):
        with self.assertRaises(HTTPError) as raised:
            urlopen(self.base + "/settings.json")
        self.assertEqual(raised.exception.code, 404)

    def test_session_bootstrap_then_authenticated_start(self):
        request = Request(self.base + "/sessions", data=b"{}", headers={"Origin": self.base})
        with urlopen(request) as response:
            bootstrap = json.load(response)
        self.assertNotIn("certificate", bootstrap)
        request = Request(self.base + f"/sessions/{bootstrap['id']}/start", data=b"{}",
                          headers={"Origin": self.base, "Authorization": "Bearer " + bootstrap["key"]})
        with urlopen(request) as response:
            self.assertEqual(json.load(response), {"status": "started"})


class SDKContractTests(unittest.TestCase):
    def test_real_sdk_serializes_managed_pipeline_and_token_auth(self):
        import httpx

        settings = {
            "instructions": "Be concise.", "greeting": "Hello.", "interruptions": True,
            "stt": {"vendor": "deepgram", "model": "nova-3", "language": "en"},
            "llm": {"vendor": "openai", "model": "gpt-4o-mini"},
            "tts": {"vendor": "minimax", "model": "speech_2_6_turbo", "voice": "English_captivating_female1"},
        }
        requests = []

        def respond(request):
            requests.append(request)
            return httpx.Response(200, json={"agent_id": "sdk-contract-agent", "status": "RUNNING"})

        with (
            patch.dict(os.environ, {"AGORA_APP_ID": "a" * 32, "AGORA_APP_CERTIFICATE": "b" * 32, "AGORA_AREA": "us"}),
            httpx.Client(transport=httpx.MockTransport(respond)) as client,
        ):
            backend = server.AgoraBackend(settings, httpx_client=client)
            handle, bootstrap = backend.prepare("contract-channel", 120)
            agent_id = backend.start(handle)
            backend.stop(handle, agent_id)
        self.assertEqual(agent_id, "sdk-contract-agent")
        self.assertEqual(bootstrap["uid"], 1001)
        self.assertTrue(bootstrap["token"].startswith("007"))
        self.assertEqual(len(requests), 2)
        body = json.loads(requests[0].content)
        props = body["properties"]
        self.assertEqual(props["channel"], "contract-channel")
        self.assertEqual(props["agent_rtc_uid"], "2001")
        self.assertEqual(props["remote_rtc_uids"], ["1001"])
        self.assertEqual(props["llm"]["greeting_message"], "Hello.")
        self.assertEqual(props["llm"]["system_messages"][0]["content"], "Be concise.")
        self.assertTrue(requests[0].headers["authorization"].startswith("agora token="))
        self.assertNotIn("api_key", props["llm"])
        self.assertTrue(str(requests[1].url).endswith("/agents/sdk-contract-agent/leave"))


if __name__ == "__main__":
    unittest.main()
