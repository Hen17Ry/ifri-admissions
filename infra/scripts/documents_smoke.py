"""Real private PDF upload/replacement/read scenario; synthetic development files only."""

import json
import sys
from http.cookiejar import CookieJar
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor, Request, build_opener

from dev import ROOT, environment


def main():
    env = environment()
    if env["APP_ENV"] != "development":
        raise RuntimeError("Document smoke requires APP_ENV=development")
    base = f"http://127.0.0.1:{env['API_PORT']}/api/v1/applications/drafts"
    client = build_opener(HTTPCookieProcessor(CookieJar()))

    def request(
        method,
        path,
        body=None,
        content_type="application/json",
        status=200,
        version=None,
        opener=client,
        origin=True,
    ):
        headers = {"Content-Type": content_type}
        if origin:
            headers["Origin"] = env["FRONTEND_ORIGIN"]
        if version is not None:
            headers["X-Application-Version"] = str(version)
        req = Request(base + path, data=body, headers=headers, method=method)
        try:
            response = opener.open(req, timeout=70)
        except HTTPError as error:
            response = error
        with response:
            if response.status != status:
                raise RuntimeError(f"{method} {path}: expected {status}, got {response.status}")
            data = response.read()
            if "/content" in path and status == 200:
                if response.headers.get_content_type() != "application/pdf":
                    raise RuntimeError("PDF content type invalid")
                if response.headers.get("X-Content-Type-Options") != "nosniff":
                    raise RuntimeError("PDF nosniff missing")
                if not response.headers.get("Content-Disposition", "").startswith("inline"):
                    raise RuntimeError("PDF disposition invalid")
                return data
            return json.loads(data)

    fixtures = ROOT / "services/api/internal/modules/documents/testdata"
    pdf = (fixtures / "valid.pdf").read_bytes()
    request("POST", "", json.dumps({"campaign": "DEV_LOCAL"}).encode(), status=201)
    listing = request("GET", "/current/documents")
    if listing["complete"] or len(listing["documents"]) != 4:
        raise RuntimeError("Initial checklist invalid")

    def upload(code, data=pdf, expected=201, version=None):
        boundary = "ifri-synthetic-document-boundary"
        body = (
            (
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; '
                'filename="synthetic.pdf"\r\nContent-Type: application/pdf\r\n\r\n'
            ).encode()
            + data
            + f"\r\n--{boundary}--\r\n".encode()
        )
        return request(
            "POST",
            "/current/documents/" + code,
            body,
            "multipart/form-data; boundary=" + boundary,
            expected,
            listing["applicationVersion"] if version is None else version,
        )

    upload("TRANSCRIPT", (fixtures / "fake.pdf").read_bytes(), 422)
    upload("TRANSCRIPT", (fixtures / "corrupt.pdf").read_bytes(), 422)
    upload("TRANSCRIPT", b"", 422)
    upload("TRANSCRIPT", pdf + b"0" * (listing["maxBytes"] + 1 - len(pdf)), 413)
    for document in listing["documents"]:
        listing = upload(document["type"])
    if not listing["complete"] or not all(d["uploaded"] for d in listing["documents"]):
        raise RuntimeError("Required documents incomplete")
    listing = upload("TRANSCRIPT")
    transcript = next(d for d in listing["documents"] if d["type"] == "TRANSCRIPT")
    if transcript["version"] != 2:
        raise RuntimeError("Transcript replacement did not create version 2")
    content = request("GET", "/current/documents/TRANSCRIPT/content")
    if content != pdf:
        raise RuntimeError("Current preview bytes differ")
    if "storage_key" in json.dumps(listing) or "storageKey" in json.dumps(listing):
        raise RuntimeError("Storage key exposed")
    request("GET", "/current/documents", status=401, opener=build_opener())
    other = build_opener(HTTPCookieProcessor(CookieJar()))
    request("POST", "", b'{"campaign":"DEV_LOCAL"}', status=201, opener=other)
    request("GET", "/current/documents/TRANSCRIPT/content", status=404, opener=other)
    upload("TRANSCRIPT", expected=409, version=1)
    request("POST", "/current/documents/TRANSCRIPT", b"", status=403, origin=False)
    try:
        with build_opener().open(f"{env['MINIO_ENDPOINT']}/{env['MINIO_BUCKET']}", timeout=5):
            raise RuntimeError("Bucket anonymously accessible")
    except HTTPError as error:
        if error.code != 403:
            raise RuntimeError("Bucket privacy check invalid") from None
    print("Four PDF uploads: complete=true; TRANSCRIPT replacement: version=2.")
    print("Authorized preview PDF bytes/headers: PASS; missing session 401; other draft 404.")
    print(
        "Fake/corrupt/empty PDFs rejected; stale version 409; "
        "missing Origin 403; private bucket 403."
    )
    print(
        "Phase 4 HTTP: PASS. Synthetic draft and versioned objects retained; no OCR or submission."
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, KeyError) as error:
        print(f"Phase 4 HTTP: FAIL: {error}", file=sys.stderr)
        raise SystemExit(1) from None
