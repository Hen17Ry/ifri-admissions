"""Real six-case reference/S3/Redis/worker/confirmation journey; synthetic fixtures only."""

import json
import sys
import tempfile
from decimal import Decimal
from http.cookiejar import CookieJar
from pathlib import Path
from time import monotonic, sleep
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor, Request, build_opener

from dev import ROOT, environment


def main():
    env = environment()
    if env["APP_ENV"] != "development":
        raise RuntimeError("OCR smoke is development-only")
    origin = env["FRONTEND_ORIGIN"]
    api = f"http://127.0.0.1:{env['API_PORT']}"
    base = "/api/v1/applications/drafts"
    client = build_opener(HTTPCookieProcessor(CookieJar()))

    def request(method, path, data=None, expected=200, version=None, anonymous=False, raw=False):
        headers = {"Origin": origin}
        if raw:
            headers["Content-Type"] = "multipart/form-data; boundary=ifri-ocr-smoke"
        else:
            headers["Content-Type"] = "application/json"
            if data is not None:
                data = json.dumps(data).encode()
        if version is not None:
            headers["X-Application-Version"] = str(version)
        try:
            response = (build_opener() if anonymous else client).open(
                Request(api + path, data=data, method=method, headers=headers),
                timeout=30,
            )
        except HTTPError as error:
            response = error
        with response:
            if response.status != expected:
                raise RuntimeError(f"{method} {path}: expected {expected}, got {response.status}")
            body = response.read()
            return json.loads(body) if body and expected != 404 else None

    def upload(kind, filename, version):
        body = (
            b"--ifri-ocr-smoke\r\nContent-Disposition: form-data; "
            b'name="file"; filename="synthetic.pdf"\r\nContent-Type: application/pdf\r\n\r\n'
            + filename.read_bytes()
            + b"\r\n--ifri-ocr-smoke--\r\n"
        )
        return request("POST", base + "/current/documents/" + kind, body, 201, version, raw=True)

    def wait_analysis(terminal_failure=False):
        deadline = monotonic() + 150
        transitions = []
        while monotonic() < deadline:
            analysis = request("GET", base + "/current/transcript-analysis")
            if not transitions or transitions[-1] != analysis["status"]:
                transitions.append(analysis["status"])
            if analysis["status"] not in {"QUEUED", "PROCESSING"}:
                if terminal_failure:
                    return analysis
                if analysis["status"] != "SUCCEEDED":
                    raise RuntimeError(
                        "Extraction failed safely: "
                        f"{analysis['status']} {analysis.get('failureCode', '')}"
                    )
                print("Observed analysis states: " + " -> ".join(transitions))
                return analysis
            sleep(1)
        raise RuntimeError("Analysis wait timed out")

    fixtures = ROOT / "services/ocr/tests/fixtures"
    cases = [
        ("GL", "BAC_C"),
        ("GL", "BAC_E"),
        ("GL", "DT_DWM"),
        ("SI", "DT_IMI"),
        ("IM", "DT_PM"),
        ("SEIOT", "DT_EAP"),
    ]
    required_fixtures = [
        fixtures / f"{program}-{qualification}-text.pdf" for program, qualification in cases
    ]
    required_fixtures.append(fixtures / "SEIOT-DT_EAP-scan.pdf")
    if any(not path.is_file() for path in required_fixtures):
        raise RuntimeError(
            "Fixtures OCR absentes : exécutez d'abord le profil Docker ocr-tests avec succès. "
            "Aucun brouillon créé."
        )
    first_id = None
    draft = request("POST", base, {"campaign": "DEV_LOCAL"}, 201)
    draft = request(
        "PATCH",
        base + "/current/profile",
        {
            "version": draft["version"],
            "profile": {
                "lastName": "Validation",
                "firstNames": "OCR synthétique",
                "email": "ocr-smoke@example.invalid",
                "phone": "+33123456789",
            },
        },
    )
    for index, (program, qualification) in enumerate(cases):
        draft = request(
            "PATCH",
            base + "/current/academic-background",
            {
                "version": draft["version"],
                "qualification": qualification,
                "graduationYear": 2025,
            },
        )
        draft = request(
            "PATCH",
            base + "/current/program",
            {"version": draft["version"], "program": program},
        )
        version = draft["version"]
        if index == 0:
            for kind in ("REQUEST_LETTER", "IDENTITY_DOCUMENT", "PARENTAL_COMMITMENT"):
                listing = upload(
                    kind,
                    ROOT / "services/api/internal/modules/documents/testdata/valid.pdf",
                    version,
                )
                version = listing["applicationVersion"]
        started = monotonic()
        listing = upload("TRANSCRIPT", fixtures / f"{program}-{qualification}-text.pdf", version)
        version = listing["applicationVersion"]
        current = wait_analysis()
        reference = request(
            "GET",
            f"/api/v1/expected-subjects?campaign=DEV_LOCAL&program={program}"
            f"&qualification={qualification}",
        )
        if [s["code"] for s in current["subjects"]] != [
            s["code"] for s in reference["expectedSubjects"]
        ]:
            raise RuntimeError("Extraction subjects differ from backend reference")
        if [s["gradeValue"] for s in current["subjects"]] != [
            "15.50",
            "13.75",
            "14",
        ] or [s["coefficientValue"] for s in current["subjects"]] != ["2.5", "2", "3"]:
            raise RuntimeError("Extracted decimal values differ from synthetic PDF")
        if index == 0:
            first_id = current["id"]
            confirmed = request(
                "POST",
                base + "/current/transcript-analysis/confirm",
                {"analysisId": first_id, "version": version},
            )
            if (
                not confirmed["verificationReady"]
                or request("GET", base + "/current/transcript-analysis")["candidateConfirmation"]
                != "CONFIRMED"
            ):
                raise RuntimeError("Confirmation/restoration failed")
        print(
            f"{qualification} + {program}: native extraction and reference PASS; "
            f"observed total {monotonic() - started:.3f}s (includes polling)"
        )
        draft = request("GET", base + "/current")
    rejected = request(
        "POST",
        base + "/current/transcript-analysis/reject",
        {"analysisId": current["id"], "version": draft["version"]},
    )
    if rejected["candidateConfirmation"] != "REJECTED" or rejected["verificationReady"]:
        raise RuntimeError("Rejection did not gate verification")
    request(
        "POST",
        base + "/current/transcript-analysis/confirm",
        {"analysisId": current["id"], "version": draft["version"]},
        409,
    )
    request(
        "POST",
        base + "/current/transcript-analysis/confirm",
        {"analysisId": first_id, "version": draft["version"]},
        409,
    )
    listing = upload("TRANSCRIPT", fixtures / "SEIOT-DT_EAP-scan.pdf", draft["version"])
    replacement = wait_analysis()
    if (
        replacement["id"] == current["id"]
        or replacement["documentVersionId"] == current["documentVersionId"]
        or replacement["candidateConfirmation"] != "PENDING"
    ):
        raise RuntimeError("Replacement reuses old analysis/confirmation")
    request("GET", base + "/current/transcript-analysis", expected=401, anonymous=True)
    request("POST", "/internal/ocr/" + replacement["id"] + "/result", {}, 404)
    print(
        "Rejection, scanned replacement, fresh analysis, stale confirmation 409, "
        "missing session 401 and private callback route 404: PASS"
    )
    bac_cell_path(request, upload, wait_analysis, base)
    print("Phase 5 HTTP: PASS. Synthetic draft retained; no average or submission.")


def bac_cell_path(request, upload, wait_analysis, base):
    """Realistic synthetic BAC photo through API, Redis, worker cell path and Go validation."""
    sys.path.insert(0, str(ROOT / "services/ocr/tests"))
    from support.synthetic_bac import TARGET_LABELS, fixture_bytes, variant

    draft = request("GET", base + "/current")
    draft = request(
        "PATCH",
        base + "/current/academic-background",
        {"version": draft["version"], "qualification": "BAC_D", "graduationYear": 2025},
    )
    draft = request(
        "PATCH", base + "/current/program", {"version": draft["version"], "program": "GL"}
    )
    version = draft["version"]
    with tempfile.TemporaryDirectory(prefix="ifri-smoke-") as directory:
        for name, expected_status, expected_failure in (
            ("d-photo", "SUCCEEDED", ""),
            ("c-declared-d", "FAILED", "SERIES_MISMATCH"),
        ):
            path = Path(directory) / f"{name}.pdf"
            path.write_bytes(fixture_bytes(name))
            started = monotonic()
            version = upload("TRANSCRIPT", path, version)["applicationVersion"]
            analysis = wait_analysis(terminal_failure=True)
            if (analysis["status"], analysis.get("failureCode", "")) != (
                expected_status,
                expected_failure,
            ):
                raise RuntimeError(
                    f"BAC {name}: {analysis['status']} {analysis.get('failureCode', '')}"
                )
            transcript, _ = variant(name)
            oracle = transcript.oracle(TARGET_LABELS)
            for subject in analysis["subjects"]:
                values = (Decimal(subject["gradeValue"]), Decimal(subject["coefficientValue"]))
                if values != tuple(map(Decimal, oracle[subject["code"]])):
                    raise RuntimeError(f"BAC {name}: FALSE VALUE ACCEPTED")
                if subject["evidence"].get("validation") != "CELL_ARITHMETIC":
                    raise RuntimeError(f"BAC {name}: cell arithmetic evidence missing")
            print(
                f"BAC cell path {name}: {analysis['status']} {analysis.get('failureCode', '')} "
                f"via API/worker/Go in {monotonic() - started:.3f}s (includes polling)"
            )


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError) as error:
        raise SystemExit(f"OCR smoke failed: {error}") from None
