#!/usr/bin/python3 -I
"""Project supported access tokens into an isolated scratch session."""

import base64
import datetime
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
import time


MAX_CREDENTIAL_BYTES = 1024 * 1024
PAYLOAD_ENV = "YOLOBOX_SCRATCH_LOGIN"
CREDENTIAL_PATHS = {
    "claude": (".claude", ".credentials.json"),
    "codex": (".codex", "auth.json"),
}
TOKEN = re.compile(r"[A-Za-z0-9._~+/=-]+\Z")
JWT = re.compile(r"[A-Za-z0-9_-]+\.([A-Za-z0-9_-]+)\.[A-Za-z0-9_-]+\Z")


def read_credentials(home, tool):
    directory, filename = CREDENTIAL_PATHS[tool]
    directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    home_fd = os.open(home, directory_flags)
    try:
        config_fd = os.open(directory, directory_flags, dir_fd=home_fd)
        try:
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            source_fd = os.open(filename, flags, dir_fd=config_fd)
            with os.fdopen(source_fd, "rb") as source:
                if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
                    raise ValueError("not a regular file")
                data = source.read(MAX_CREDENTIAL_BYTES + 1)
        finally:
            os.close(config_fd)
    finally:
        os.close(home_fd)
    if len(data) > MAX_CREDENTIAL_BYTES:
        raise ValueError("credential document too large")
    document = json.loads(data)
    if not isinstance(document, dict):
        raise ValueError("invalid credential document")
    return document


def fresh_access_token(token, expires_at, now):
    if not isinstance(token, str) or len(token) > 16384 or not TOKEN.fullmatch(token):
        raise ValueError("invalid token")
    if type(expires_at) not in (int, float) or not math.isfinite(expires_at) or not expires_at > now:
        raise ValueError("expired token")
    return token


def export_login(home, tool, now):
    document = read_credentials(home, tool)
    if tool == "claude":
        oauth = document["claudeAiOauth"]
        expiry = oauth["expiresAt"]
        if type(expiry) not in (int, float):
            raise ValueError("invalid expiry")
        expires_at = expiry / 1000
        access_token = fresh_access_token(oauth["accessToken"], expires_at, now)
        return {"tool": tool, "access_token": access_token, "expires_at": expires_at}

    if document.get("auth_mode") not in (None, "chatgpt", "chatgptAuthTokens"):
        raise ValueError("unsupported auth mode")
    tokens = document["tokens"]
    candidate = tokens["access_token"]
    jwt = JWT.fullmatch(candidate) if isinstance(candidate, str) and len(candidate) <= 16384 else None
    if not jwt:
        raise ValueError("invalid access JWT")
    encoded_claims = jwt.group(1)
    claims = json.loads(base64.urlsafe_b64decode(encoded_claims + "=" * (-len(encoded_claims) % 4)))
    access_token = fresh_access_token(candidate, claims["exp"], now)
    auth_claims = claims.get("https://api.openai.com/auth", {})
    account_id = tokens.get("account_id") or auth_claims.get("chatgpt_account_id")
    if not isinstance(account_id, str) or len(account_id) > 256 or not TOKEN.fullmatch(account_id):
        raise ValueError("missing account ID")
    return {"tool": tool, "access_token": access_token, "account_id": account_id, "expires_at": claims["exp"]}


def import_login(home, payload, now):
    tool = payload["tool"]
    token = fresh_access_token(payload["access_token"], payload["expires_at"], now)
    if tool == "claude":
        # The native environment-token mode has no refresh credentials.
        return token
    if tool != "codex":
        raise ValueError("unsupported login tool")

    account_id = payload["account_id"]
    if not isinstance(account_id, str) or len(account_id) > 256 or not TOKEN.fullmatch(account_id):
        raise ValueError("missing account ID")
    config = Path(home) / ".codex"
    config.mkdir(mode=0o700, exist_ok=True)
    directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    config_fd = os.open(config, directory_flags)
    try:
        # Codex's external-token mode uses the access JWT for identity claims.
        # TokenData requires a refresh_token field, so keep it empty.
        auth = {
            "auth_mode": "chatgptAuthTokens",
            "tokens": {
                "id_token": token,
                "access_token": token,
                "account_id": account_id,
                "refresh_token": "",
            },
            "last_refresh": datetime.datetime.fromtimestamp(now, datetime.timezone.utc).isoformat(),
        }
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW
        fd = os.open("auth.json", flags, 0o600, dir_fd=config_fd)
        with os.fdopen(fd, "w") as target:
            json.dump(auth, target)
    finally:
        os.close(config_fd)
    return ""


def main(args):
    if len(args) == 2 and args[0] == "export" and args[1] in CREDENTIAL_PATHS:
        try:
            print(json.dumps(export_login("/loan-home", args[1], time.time()), separators=(",", ":")))
        except (KeyError, AttributeError, TypeError, ValueError, OverflowError, OSError):
            # The host reports a stable message. Never include credential data.
            return 3
    elif args == ["import"]:
        try:
            payload = json.loads(os.environ[PAYLOAD_ENV])
            token = import_login("/home/yolo", payload, time.time())
            if token:
                print(token)
        except (KeyError, AttributeError, TypeError, ValueError, OverflowError, OSError):
            print("yolobox: could not install the scratch session login", file=sys.stderr)
            return 1
    else:
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
