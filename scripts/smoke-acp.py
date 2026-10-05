#!/usr/bin/env python3
"""Check ACP versions and protocol startup through yolobox, without credentials."""

import argparse
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import tempfile
import time


def initialize(binary):
    request = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": 1,
            "clientCapabilities": {},
            "clientInfo": {"name": "yolobox-smoke", "version": "1"},
        },
    }
    with tempfile.TemporaryFile() as errors:
        process = subprocess.Popen(
            [binary],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=errors,
            start_new_session=True,
        )
        try:
            process.stdin.write((json.dumps(request) + "\n").encode())
            process.stdin.flush()
            pending = b""
            deadline = time.monotonic() + 30
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                while True:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or not selector.select(remaining):
                        raise RuntimeError(f"{binary}: ACP initialization timed out")
                    chunk = os.read(process.stdout.fileno(), 65536)
                    if not chunk:
                        errors.seek(0)
                        raise RuntimeError(
                            f"{binary}: exited before ACP response: "
                            + errors.read().decode(errors="replace")
                        )
                    pending += chunk
                    while b"\n" in pending:
                        line, pending = pending.split(b"\n", 1)
                        message = json.loads(line)
                        if message.get("id") != request["id"]:
                            continue
                        result = message.get("result", {})
                        if (
                            message.get("jsonrpc") != "2.0"
                            or result.get("protocolVersion") != 1
                            or not isinstance(result.get("agentCapabilities"), dict)
                            or "error" in message
                        ):
                            raise RuntimeError(f"{binary}: invalid ACP response: {message}")
                        return
        finally:
            process.stdin.close()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
            process.stdout.close()


def check_adapters():
    for binary in ("codex-acp", "claude-agent-acp"):
        version = subprocess.run(
            [binary, "--version"],
            capture_output=True,
            text=True,
            check=True,
            timeout=30,
        ).stdout.strip()
        if not version:
            raise RuntimeError(f"{binary}: --version returned no version")
        initialize(binary)
        print(f"  ✓ {binary}: {version}; ACP initialize passed", flush=True)


def main():
    if sys.argv[1:] == ["--inside-container"]:
        check_adapters()
        return
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--yolobox", default="./yolobox")
    parser.add_argument("run_args", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    run_args = args.run_args
    if run_args[:1] == ["--"]:
        run_args = run_args[1:]
    command = [
        str(Path(args.yolobox).resolve()),
        "run",
        *run_args,
        "--scratch",
        "--no-project",
        "--no-network",
        "--no-ssh-agent",
        "--no-env-passthrough",
        "python3",
        "-",
        "--inside-container",
    ]
    # Keep personal global settings, config sync, and credentials out of the smoke.
    with tempfile.TemporaryDirectory(prefix="yolobox-acp-config-") as config_home:
        env = dict(os.environ, XDG_CONFIG_HOME=config_home)
        process = subprocess.Popen(
            command,
            stdin=subprocess.PIPE,
            text=True,
            env=env,
            cwd=config_home,
            start_new_session=True,
        )
        try:
            process.communicate(Path(__file__).read_text(), timeout=120)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate()
            raise
        if process.returncode:
            raise subprocess.CalledProcessError(process.returncode, command)


if __name__ == "__main__":
    main()
