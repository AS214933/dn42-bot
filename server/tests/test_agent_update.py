import importlib.util
import sys
import types
import unittest
from pathlib import Path
from unittest import mock


class _FakeBot:
    def __init__(self):
        self.edits = []
        self.answers = []

    def message_handler(self, **_kwargs):
        return lambda func: func

    def callback_query_handler(self, **_kwargs):
        return lambda func: func

    def edit_message_text(self, *args, **kwargs):
        self.edits.append((args, kwargs))

    def answer_callback_query(self, *args, **kwargs):
        self.answers.append((args, kwargs))

    def send_message(self, *_args, **_kwargs):
        return None


class _Button:
    def __init__(self, text, callback_data):
        self.text = text
        self.callback_data = callback_data


class _Markup:
    def __init__(self):
        self.rows = []

    def row(self, *buttons):
        self.rows.append(buttons)


def _load_agent_update_module():
    bot = _FakeBot()

    base = types.ModuleType("base")
    base.bot = bot
    base.db_privilege = {100}
    base.servers = {"a": "Node A", "b": "Node B"}

    config = types.ModuleType("config")
    config.HOSTS = {}
    config.ENDPOINT = "nodes.example"
    config.API_PORT = 54321
    config.API_TOKEN = "secret"

    requests = types.ModuleType("requests")
    requests.post = mock.Mock()

    telebot_types = types.ModuleType("telebot.types")
    telebot_types.InlineKeyboardButton = _Button
    telebot_types.InlineKeyboardMarkup = _Markup
    telebot_types.ReplyKeyboardRemove = object
    telebot = types.ModuleType("telebot")
    telebot.types = telebot_types

    path = Path(__file__).resolve().parents[1] / "commands" / "tools" / "agent_update.py"
    spec = importlib.util.spec_from_file_location("agent_update_under_test", path)
    module = importlib.util.module_from_spec(spec)
    modules = {
        "base": base,
        "config": config,
        "requests": requests,
        "telebot": telebot,
        "telebot.types": telebot_types,
    }
    with mock.patch.dict(sys.modules, modules):
        spec.loader.exec_module(module)
    return module, bot


def _callback(data):
    return types.SimpleNamespace(
        id="callback-id",
        data=data,
        message=types.SimpleNamespace(
            chat=types.SimpleNamespace(id=100),
            message_id=42,
        ),
    )


class AgentUpdateTest(unittest.TestCase):
    def setUp(self):
        self.agent_update, self.bot = _load_agent_update_module()

    def test_apply_uses_300_second_timeout_and_reports_success(self):
        calls = []

        def post(server_key, endpoint, payload, timeout):
            calls.append((server_key, endpoint, payload, timeout))
            return {
                "ok": True,
                "status": 202,
                "json": {
                    "latest_version": "v2.1.0-alpha.2",
                    "update_available": True,
                    "installed": True,
                    "restart_required": True,
                },
            }

        self.agent_update._post_agent_update = post
        output = self.agent_update._run_update("a", "apply", "candidate")

        self.assertEqual(
            calls,
            [("a", "update/apply", {"channel": "candidate", "force": False}, 300)],
        )
        self.assertIn("Update succeeded.", output)
        self.assertIn("更新成功。", output)
        self.assertIn("Agent restart scheduled.", output)
        self.assertIn("Agent 重启已安排。", output)

    def test_apply_reports_current_and_failure_states(self):
        current = self.agent_update._format_apply_result(
            "a",
            {
                "ok": True,
                "status": 200,
                "json": {
                    "current_version": "v2.1.0-alpha.2",
                    "update_available": False,
                    "installed": False,
                },
            },
        )
        failed = self.agent_update._format_apply_result(
            "b",
            {"ok": False, "status": 502, "text": "download failed"},
        )

        self.assertIn("Already up to date.", current)
        self.assertIn("已是最新版本。", current)
        self.assertIn("Update failed.", failed)
        self.assertIn("更新失败。", failed)
        self.assertIn("HTTP 502: download failed", failed)

    def test_apply_result_is_terminal_without_action_keyboard(self):
        self.agent_update._run_update = mock.Mock(return_value="Update succeeded.\n更新成功。")

        self.agent_update.handle_update_callback(_callback("upd:apply:candidate:a:0"))

        self.assertEqual(len(self.bot.edits), 2)
        self.assertIsNone(self.bot.edits[-1][1]["reply_markup"])

    def test_all_nodes_keep_individual_success_and_failure_results(self):
        results = {
            "a": {
                "ok": True,
                "status": 202,
                "json": {
                    "latest_version": "v2.1.0-alpha.2",
                    "update_available": True,
                    "installed": True,
                    "restart_required": True,
                },
            },
            "b": {"ok": False, "error": "request timed out"},
        }
        calls = []

        def post(server_key, endpoint, payload, timeout):
            calls.append((server_key, endpoint, payload, timeout))
            return results[server_key]

        self.agent_update._post_agent_update = post
        output = self.agent_update._run_update("all", "apply", "candidate")

        self.assertEqual([call[0] for call in calls], ["a", "b"])
        self.assertTrue(all(call[3] == 300 for call in calls))
        self.assertLess(output.index("Node A:"), output.index("Node B:"))
        self.assertIn("Update succeeded.", output)
        self.assertIn("Update failed.", output)
        self.assertIn("request timed out", output)

    def test_check_keeps_action_keyboard_and_short_timeout(self):
        calls = []

        def post(server_key, endpoint, payload, timeout):
            calls.append((server_key, endpoint, payload, timeout))
            return {
                "ok": True,
                "status": 200,
                "json": {
                    "current_version": "v2.1.0-alpha.1",
                    "latest_version": "v2.1.0-alpha.2",
                    "channel": "candidate",
                    "update_available": True,
                    "installed": False,
                    "restart_required": False,
                },
            }

        self.agent_update._post_agent_update = post
        self.agent_update.handle_update_callback(_callback("upd:check:candidate:a"))

        self.assertEqual(calls, [("a", "update/check", {"channel": "candidate"}, 15)])
        self.assertIsInstance(self.bot.edits[-1][1]["reply_markup"], _Markup)

    def test_malformed_success_response_is_reported_as_failure(self):
        output = self.agent_update._format_apply_result(
            "a",
            {
                "ok": True,
                "status": 202,
                "json": {"installed": True, "restart_required": False},
            },
        )

        self.assertIn("Update failed.", output)
        self.assertIn("HTTP 202", output)

    def test_force_noop_response_is_reported_as_failure(self):
        output = self.agent_update._format_apply_result(
            "a",
            {
                "ok": True,
                "status": 200,
                "json": {
                    "update_available": False,
                    "installed": False,
                    "restart_required": False,
                },
            },
            force=True,
        )

        self.assertIn("Update failed.", output)
        self.assertNotIn("Already up to date.", output)


if __name__ == "__main__":
    unittest.main()
