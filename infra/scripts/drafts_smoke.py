"""Exercise the real cookie-authenticated draft API with synthetic data only."""

import hashlib
import json
import subprocess
import sys
from datetime import datetime
from http.cookiejar import CookieJar
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor, Request, build_opener
from uuid import UUID

from dev import ROOT, environment


def main():
    env = environment()
    if env["APP_ENV"] != "development":
        raise RuntimeError("Draft smoke requires APP_ENV=development")
    base = f"http://127.0.0.1:{env['API_PORT']}/api/v1/applications/drafts"
    jar = CookieJar()
    client = build_opener(HTTPCookieProcessor(jar))

    def request(method, path, payload=None, status=200, origin=True, opener=client):
        headers = {"Content-Type": "application/json"}
        if origin:
            headers["Origin"] = env["FRONTEND_ORIGIN"]
        data = json.dumps(payload).encode() if payload is not None else None
        req = Request(base + path, data=data, headers=headers, method=method)
        try:
            response = opener.open(req, timeout=5)
        except HTTPError as error:
            response = error
        with response:
            if response.status != status:
                raise RuntimeError(f"{method} {path}: expected {status}, got {response.status}")
            return json.load(response)

    request("GET", "/current", status=401)
    draft = request("POST", "", {"campaign": "DEV_LOCAL"}, 201)
    cookies = list(jar)
    if len(cookies) != 1:
        raise RuntimeError("Missing session cookie")
    cookie = cookies[0]
    if (
        cookie.name != "ifri_draft"
        or cookie.path != "/api/v1/applications/drafts"
        or not cookie.has_nonstandard_attr("HttpOnly")
        or cookie.get_nonstandard_attr("SameSite") != "Lax"
        or cookie.expires is None
    ):
        raise RuntimeError("Session cookie flags invalid")
    if cookie.value in json.dumps(draft) or "secret_hash" in json.dumps(draft):
        raise RuntimeError("Session secret exposed in JSON")
    sql = (
        "SELECT encode(secret_hash,'hex') FROM application_draft_sessions "
        f"WHERE application_id='{UUID(draft['id'])}'::uuid;"
    )
    result = subprocess.run(
        [
            "docker",
            "compose",
            "exec",
            "-T",
            "postgres",
            "sh",
            "-ec",
            'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 '
            '-U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1',
        ],
        cwd=ROOT,
        input=sql,
        capture_output=True,
        text=True,
        check=True,
    )
    if result.stdout.strip() != hashlib.sha256(cookie.value.encode()).hexdigest():
        raise RuntimeError("Database session hash does not match cookie")
    print(
        "Session: HttpOnly, SameSite=Lax, limited path, expiry "
        "and SHA-256 storage verified (secret omitted)."
    )
    if request("POST", "", {"campaign": "DEV_LOCAL"})["id"] != draft["id"]:
        raise RuntimeError("Repeated creation duplicated a draft")
    profile = {
        "lastName": "Validation",
        "firstNames": "Test technique",
        "email": " PHASE3@EXAMPLE.INVALID ",
        "phone": "+33 1 23 45 67 89",
    }
    draft = request("PATCH", "/current/profile", {"version": draft["version"], "profile": profile})
    if draft["profile"]["email"] != "phase3@example.invalid":
        raise RuntimeError("Email normalization failed")
    year = datetime.now().year - 1
    for qualification in ("BAC_E", "DT_DWM"):
        draft = request(
            "PATCH",
            "/current/academic-background",
            {
                "version": draft["version"],
                "qualification": qualification,
                "graduationYear": year,
            },
        )
        draft = request("PATCH", "/current/program", {"version": draft["version"], "program": "GL"})
        if draft["program"]["code"] != "GL":
            raise RuntimeError("Eligible GL selection failed")
        print(f"{qualification} + GL: eligible and persisted.")
    draft = request(
        "PATCH",
        "/current/academic-background",
        {
            "version": draft["version"],
            "qualification": "DT_EAP",
            "graduationYear": year,
        },
    )
    if draft["program"] is not None:
        raise RuntimeError("GL was not removed after qualification change")
    refusal = request(
        "PATCH", "/current/program", {"version": draft["version"], "program": "GL"}, 422
    )
    if refusal["error"]["code"] != "APPLICATION_NOT_ELIGIBLE":
        raise RuntimeError("Ineligible combination error invalid")
    restored = request("GET", "/current")
    if restored["id"] != draft["id"] or restored["qualification"]["code"] != "DT_EAP":
        raise RuntimeError("Draft restoration failed")
    other = build_opener()
    request("GET", "/current", status=401, opener=other)
    request(
        "PATCH",
        "/current/program",
        {"version": draft["version"], "program": "SEIOT"},
        403,
        origin=False,
    )
    request("PATCH", "/current/program", {"version": 1, "program": "SEIOT"}, 409)
    print("DT_EAP + GL: HTTP 422; previous GL cleared; reload restores the same draft.")
    print("Missing session 401, missing Origin 403, stale version 409: PASS.")
    print("Phase 3 HTTP: PASS. Synthetic development draft retained; no submission performed.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, KeyError, subprocess.CalledProcessError) as error:
        print(f"Phase 3 HTTP: FAIL: {error}", file=sys.stderr)
        raise SystemExit(1) from None
