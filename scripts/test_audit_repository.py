"""Check the local scanner with constructed, non-secret test credentials."""

from contextlib import redirect_stdout
from io import BytesIO, StringIO
import unittest
from unittest.mock import Mock, patch

from audit_repository import findings, main


class CredentialPatternTests(unittest.TestCase):
    def test_detects_supported_credential_shapes(self):
        samples = {
            "private-key": b"-----BEGIN " + b"PRIVATE KEY-----",
            "aws-access-key": b"AKIA" + b"A" * 16,
            "github-token": b"ghp_" + b"A" * 36,
            "slack-token": b"xoxb-" + b"A" * 24,
            "service-api-key": b"sk-proj-" + b"A" * 40,
            "signed-jwt": b"eyJ" + b"A" * 20 + b"." + b"B" * 20 + b"." + b"C" * 20,
        }
        for rule, value in samples.items():
            with self.subTest(rule=rule):
                self.assertIn(rule, findings(b"prefix " + value + b" suffix"))

    def test_does_not_flag_documented_placeholders(self):
        for value in [b"<signed JWT>", b"local_example_only", b"ghp_demo", b"password_hash", b"taskflow:v1:project:1"]:
            with self.subTest(value=value):
                self.assertEqual([], findings(value))


class RepositoryAuditTests(unittest.TestCase):
    def test_deleted_tracked_file_does_not_skip_working_tree_or_history(self):
        credential = b"sk-proj-" + b"A" * 40
        for working_data, historical_data, expected_result in [
            (b"safe", b"safe", 0),
            (credential, b"safe", 1),
            (b"safe", credential, 1),
        ]:
            with self.subTest(working_tree_match=working_data == credential,
                              historical_match=historical_data == credential):
                responses = {
                    ("rev-parse", "--is-shallow-repository"): b"false\n",
                    ("log", "--all", "--format=", "--name-only", "--", ".env"): b"",
                    ("ls-files", "-z"): b"deleted.txt\0present.txt\0",
                    ("rev-list", "--objects", "--all"): b"abc deleted.txt\n",
                }
                process = Mock(
                    stdin=BytesIO(),
                    stdout=BytesIO(b"abc blob " + str(len(historical_data)).encode()
                                   + b"\n" + historical_data + b"\n"),
                )
                process.wait.return_value = 0
                output = StringIO()
                with patch("audit_repository.Path.read_bytes", side_effect=[FileNotFoundError, working_data]), \
                        patch("audit_repository.git", side_effect=lambda *args: responses[args]), \
                        patch("audit_repository.subprocess.Popen", return_value=process), \
                        redirect_stdout(output):
                    self.assertEqual(expected_result, main())
                if expected_result == 0:
                    self.assertIn("1 tracked files, 1 historical blobs", output.getvalue())
                elif working_data == credential:
                    self.assertIn("service-api-key, working tree, present.txt", output.getvalue())
                else:
                    self.assertIn("service-api-key, blob abc, deleted.txt", output.getvalue())

    def test_working_tree_read_errors_are_not_ignored(self):
        with patch("audit_repository.git", side_effect=[b"false\n", b"", b"file.txt\0"]), \
                patch("audit_repository.Path.read_bytes", side_effect=PermissionError):
            with self.assertRaises(PermissionError):
                main()

    def test_deleted_tracked_environment_files_and_artifacts_are_reported(self):
        for name in [".env", ".env.local", "build.exe", ".idea/config.xml"]:
            with self.subTest(name=name):
                process = Mock(stdin=BytesIO(), stdout=BytesIO())
                process.wait.return_value = 0
                output = StringIO()
                with patch("audit_repository.git", side_effect=[b"false\n", b"", name.encode() + b"\0", b""]), \
                        patch("audit_repository.Path.read_bytes", side_effect=FileNotFoundError), \
                        patch("audit_repository.subprocess.Popen", return_value=process), \
                        redirect_stdout(output):
                    self.assertEqual(1, main())
                self.assertIn(name, output.getvalue())


if __name__ == "__main__":
    unittest.main()
