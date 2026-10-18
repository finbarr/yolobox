#!/usr/bin/env python3
"""Check OpenCode RTK startup in a real terminal without sending any input."""

import argparse
import errno
import os
from pathlib import Path
import pty
import re
import select
import signal
import subprocess
import tempfile
import time
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--yolobox", default="./yolobox")
    parser.add_argument("--timeout", type=float, default=60)
    parser.add_argument("run_args", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    run_args = args.run_args
    if run_args[:1] == ["--"]:
        run_args = run_args[1:]
    name = "yolobox-rtk-smoke-" + uuid.uuid4().hex[:12]
    command = [
        str(Path(args.yolobox).resolve()), "run", *run_args,
        "--runtime", "docker", "--scratch", "--no-project", "--no-network",
        "--no-ssh-agent", "--no-env-passthrough", "--rtk",
        "--name", name, "--runtime-arg=-it",
        "--runtime-arg=--hostname=yolobox-rtk-smoke",
        "--runtime-arg=--add-host=yolobox-rtk-smoke:127.0.0.1",
        "opencode", "--version",
    ]
    output = bytearray()
    with tempfile.TemporaryDirectory(prefix="yolobox-rtk-config-") as config_home:
        master, slave = pty.openpty()
        process = None
        try:
            process = subprocess.Popen(
                command, stdin=slave, stdout=slave, stderr=slave,
                env=dict(os.environ, XDG_CONFIG_HOME=config_home),
                cwd=config_home, start_new_session=True,
            )
            os.close(slave)
            slave = None
            deadline = time.monotonic() + args.timeout
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise RuntimeError("OpenCode RTK startup timed out without input")
                if select.select([master], [], [], min(remaining, 0.25))[0]:
                    try:
                        chunk = os.read(master, 65536)
                    except OSError as error:
                        if error.errno != errno.EIO:
                            raise
                        break
                    if not chunk:
                        break
                    output.extend(chunk)
            process.wait(timeout=max(0.1, deadline - time.monotonic()))
            text = output.decode(errors="replace")
            if process.returncode or "RTK command compression enabled for opencode" not in text:
                raise RuntimeError("OpenCode exited without successful RTK setup")
            if not re.search(r"(?m)^\d+\.\d+\.\d+[^\r\n]*\r?$", text):
                raise RuntimeError("OpenCode did not report its version")
            print("  ✓ opencode: RTK initialized and CLI launched in a TTY without input")
        except Exception:
            print(output.decode(errors="replace"))
            raise
        finally:
            if process is not None and process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
            subprocess.run(
                ["docker", "rm", "-f", name], stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL, timeout=15,
            )
            os.close(master)
            if slave is not None:
                os.close(slave)


if __name__ == "__main__":
    main()
