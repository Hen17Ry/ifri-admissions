"""Read the actual phase-2 HTTP API; never substitutes fixtures for responses."""

import json
import sys
from urllib.error import HTTPError
from urllib.request import urlopen

from dev import environment


def main():
    env = environment()
    base = f"http://127.0.0.1:{env['API_PORT']}/api/v1"

    def request(path, status=200):
        try:
            response = urlopen(base + path, timeout=5)
        except HTTPError as error:
            response = error
        with response:
            if response.status != status:
                raise RuntimeError(f"{path}: expected {status}, got {response.status}")
            body = json.load(response)
        return body

    campaign = "campaign=DEV_LOCAL"
    if request(f"/campaigns/current?{campaign}")["code"] != "DEV_LOCAL":
        raise RuntimeError("Wrong campaign")
    programs = request(f"/programs?{campaign}")
    if {p["code"] for p in programs} != {"IA", "SI", "GL", "IM", "SEIOT"}:
        raise RuntimeError("Missing programs")
    if any(p["availableSeats"] is not None for p in programs):
        raise RuntimeError("Demo quotas should remain undefined")
    if len(request("/origin-qualifications")) != 7:
        raise RuntimeError("Missing qualifications")
    qualifications = request(f"/programs/GL/eligible-qualifications?{campaign}")
    if {q["code"] for q in qualifications} != {"BAC_C", "BAC_D", "BAC_E", "DT_IMI", "DT_DWM"}:
        raise RuntimeError("Wrong GL eligibility list")
    accessible = request(f"/programs?{campaign}&qualification=DT_DWM")
    if {p["code"] for p in accessible} != {"GL", "IM"}:
        raise RuntimeError("Wrong programs for DT/DWM")
    cases = [
        ("GL", "BAC_C", ["MATH", "FRENCH", "ENGLISH"]),
        ("GL", "BAC_E", ["MATH", "FRENCH", "MANUFACTURING_STUDY"]),
        ("GL", "DT_IMI", ["APPLIED_MATH", "FRENCH", "INFORMATION_SYSTEMS_TECH"]),
        ("GL", "DT_DWM", ["APPLIED_MATH", "FRENCH", "WEB_SITES_APPS"]),
        ("IM", "DT_PM", ["IT_PROJECT", "FRENCH", "EDITING"]),
        ("SEIOT", "DT_EAP", ["APPLIED_SCIENCES", "FRENCH", "ELECTRONICS_STUDY"]),
        ("SI", "DT_IMI", ["APPLIED_MATH", "FRENCH", "INFORMATION_SYSTEMS_TECH"]),
    ]
    for program, qualification, expected in cases:
        query = f"?{campaign}&program={program}&qualification={qualification}"
        for endpoint in ("/eligibility", "/expected-subjects"):
            body = request(endpoint + query)
            subjects = body["expectedSubjects"]
            if not body["eligible"] or [s["code"] for s in subjects] != expected:
                raise RuntimeError(f"Wrong matrix for {program}/{qualification}")
            for position, subject in enumerate(subjects, 1):
                if set(subject) != {"code", "name", "position"} or subject["position"] != position:
                    raise RuntimeError("Unexpected subject fields or positions")
        if (program, qualification) in {("GL", "DT_DWM"), ("GL", "BAC_E")}:
            print(json.dumps(body, ensure_ascii=False, indent=2))
    refused = request(f"/eligibility?{campaign}&program=IA&qualification=DT_DWM")
    if refused["eligible"] or refused["reason"] != "RULE_NOT_DEFINED":
        raise RuntimeError("Undefined combination must not be eligible")
    for path, code in [
        (f"/eligibility?{campaign}&program=UNKNOWN&qualification=BAC_C", "PROGRAM_NOT_FOUND"),
        (f"/eligibility?{campaign}&program=GL&qualification=UNKNOWN", "QUALIFICATION_NOT_FOUND"),
        ("/campaigns/current?campaign=UNKNOWN", "CAMPAIGN_NOT_FOUND"),
    ]:
        if request(path, 404)["code"] != code:
            raise RuntimeError("Incorrect not-found semantics")
    request("/eligibility?program=GL", 400)
    request("/programs?campaign=bad", 400)
    print("Phase 2 HTTP: PASS (all six endpoints, matrix, filters and error semantics).")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, RuntimeError, KeyError) as error:
        print(f"Phase 2 HTTP: FAIL: {error}", file=sys.stderr)
        raise SystemExit(1) from None
