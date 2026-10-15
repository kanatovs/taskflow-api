"""Local, redacted pattern scan of tracked files and all reachable Git blobs.

This is a limited review aid, not proof that no credential has ever existed.
It never reads ignored environment files or sends repository data over a network.
"""

from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
RULES = {
    "private-key": rb"-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----",
    "aws-access-key": rb"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b",
    "github-token": rb"\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{50,})\b",
    "slack-token": rb"\bxox[baprs]-[A-Za-z0-9-]{24,}\b",
    "openai-key": rb"\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{32,}\b",
    "signed-jwt": rb"\beyJ[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\b",
}
PATTERNS = {name: re.compile(pattern) for name, pattern in RULES.items()}


def git(*arguments):
    return subprocess.check_output(["git", *arguments], cwd=ROOT)


def findings(data):
    return [name for name, pattern in PATTERNS.items() if pattern.search(data)]


def main():
    failed = False
    if git("rev-parse", "--is-shallow-repository").strip() == b"true":
        print("History audit requires a full clone; this repository is shallow")
        return 1
    if git("log", "--all", "--format=", "--name-only", "--", ".env").strip():
        print("A .env file exists in reachable Git history [contents not displayed]")
        failed = True
    files = git("ls-files", "-z").decode("utf-8").split("\0")
    for name in filter(None, files):
        path = Path(name)
        if path.name == ".env" or (path.name.startswith(".env.") and path.name != ".env.example"):
            print(f"Tracked environment file: {name}")
            failed = True
        if path.suffix in {".exe", ".dll", ".test", ".out"} or any(
            part in {".idea", ".vscode", "bin", "dist"} for part in path.parts
        ):
            print(f"Unexpected tracked artifact: {name}")
            failed = True
        data = (ROOT / path).read_bytes()
        if len(data) > 1024 * 1024:
            print(f"Tracked file exceeds 1 MiB: {name}")
            failed = True
        for rule in findings(data):
            print(f"Pattern match [REDACTED]: {rule}, working tree, {name}")
            failed = True

    objects = git("rev-list", "--objects", "--all").splitlines()
    process = subprocess.Popen(
        ["git", "cat-file", "--batch"], cwd=ROOT,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
    )
    blobs = 0
    try:
        for entry in objects:
            oid, _, raw_path = entry.partition(b" ")
            process.stdin.write(oid + b"\n")
            process.stdin.flush()
            header = process.stdout.readline().split()
            if len(header) != 3:
                raise RuntimeError("Could not read Git object")
            data = process.stdout.read(int(header[2]))
            if process.stdout.read(1) != b"\n":
                raise RuntimeError("Invalid Git object boundary")
            if header[1] != b"blob":
                continue
            blobs += 1
            for rule in findings(data):
                print(f"Pattern match [REDACTED]: {rule}, blob {oid.decode()}, "
                      f"{raw_path.decode('utf-8', errors='replace')}")
                failed = True
    finally:
        process.stdin.close()
        process.stdout.close()
        if process.wait() != 0:
            raise RuntimeError("Git object scan failed")
    if failed:
        return 1
    print(f"Repository audit passed: {len(list(filter(None, files)))} tracked files, "
          f"{blobs} historical blobs; no configured credential patterns found")
    return 0


if __name__ == "__main__":
    sys.exit(main())
