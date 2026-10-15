"""Check the local scanner with constructed, non-secret test credentials."""

import unittest

from audit_repository import findings


class CredentialPatternTests(unittest.TestCase):
    def test_detects_supported_credential_shapes(self):
        samples = {
            "private-key": b"-----BEGIN " + b"PRIVATE KEY-----",
            "aws-access-key": b"AKIA" + b"A" * 16,
            "github-token": b"ghp_" + b"A" * 36,
            "slack-token": b"xoxb-" + b"A" * 24,
            "openai-key": b"sk-proj-" + b"A" * 40,
            "signed-jwt": b"eyJ" + b"A" * 20 + b"." + b"B" * 20 + b"." + b"C" * 20,
        }
        for rule, value in samples.items():
            with self.subTest(rule=rule):
                self.assertIn(rule, findings(b"prefix " + value + b" suffix"))

    def test_does_not_flag_documented_placeholders(self):
        for value in [b"<signed JWT>", b"local_example_only", b"ghp_demo", b"password_hash", b"taskflow:v1:project:1"]:
            with self.subTest(value=value):
                self.assertEqual([], findings(value))


if __name__ == "__main__":
    unittest.main()
