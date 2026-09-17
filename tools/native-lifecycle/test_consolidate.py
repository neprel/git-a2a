#!/usr/bin/env python3

import unittest

import consolidate


def result(status: str, log: str) -> dict[str, object]:
    return {
        "family": "npm",
        "variant": "npm",
        "image": "node:test",
        "kind": "public-cli",
        "command": "native-test",
        "version_command": "npm --version",
        "observed_version": "test",
        "status": status,
        "detail": status.lower(),
        "log": log,
    }


def document(*items: dict[str, object]) -> dict[str, object]:
    return {"target": "linux/amd64", "results": list(items)}


class ConsolidateResultsTest(unittest.TestCase):
    def assert_sequence(self, statuses: list[str], expected: str) -> None:
        documents = [
            document(result(status, f"{index}-{status}.log"))
            for index, status in enumerate(statuses)
        ]
        consolidated = consolidate.consolidate_results(
            documents, [f"report-{index}" for index in range(len(documents))]
        )
        self.assertEqual(consolidated[0]["status"], expected)
        self.assertEqual(
            [attempt["status"] for attempt in consolidated[0]["attempts"]],
            [status for status in statuses if status != "NOT RUN"],
        )

    def test_pass_then_fail_is_fail(self) -> None:
        self.assert_sequence(["PASS", "FAIL"], "FAIL")

    def test_fail_then_pass_is_pass(self) -> None:
        self.assert_sequence(["FAIL", "PASS"], "PASS")

    def test_pass_then_blocked_is_blocked(self) -> None:
        self.assert_sequence(["PASS", "BLOCKED"], "BLOCKED")

    def test_pass_then_not_run_remains_pass(self) -> None:
        self.assert_sequence(["PASS", "NOT RUN"], "PASS")

    def test_reconsolidation_preserves_flat_history(self) -> None:
        initial = consolidate.consolidate_results(
            [document(result("PASS", "pass.log")), document(result("FAIL", "fail.log"))],
            ["pass-report", "fail-report"],
        )
        repeated = consolidate.consolidate_results(
            [document(initial[0]), document(result("PASS", "recovery.log"))],
            ["consolidated-report", "recovery-report"],
        )

        self.assertEqual(repeated[0]["status"], "PASS")
        self.assertEqual(
            [attempt["status"] for attempt in repeated[0]["attempts"]],
            ["PASS", "FAIL", "PASS"],
        )
        self.assertEqual(
            [attempt["summary"] for attempt in repeated[0]["attempts"]],
            ["pass-report", "fail-report", "recovery-report"],
        )
        self.assertTrue(all("attempts" not in attempt for attempt in repeated[0]["attempts"]))


if __name__ == "__main__":
    unittest.main()
