import hashlib
import json
from threading import Event

import pymupdf

from ifri_ocr.jobs import process


def native_job():
    doc = pymupdf.open()
    page = doc.new_page()
    expected = [
        {"id": f"fixture-{i}", "name": name, "position": i + 1}
        for i, name in enumerate(["Alpha", "Beta", "Gamma"])
    ]
    for x, text in [(40, "Matière"), (350, "Note"), (460, "Coef")]:
        page.insert_text((x, 100), text)
    for i, subject in enumerate(expected):
        for x, text in [(40, subject["name"]), (350, "15,50"), (460, "2.5")]:
            page.insert_text((x, 140 + i * 40), text)
    content = doc.tobytes()
    doc.close()
    return content, {
        "contractVersion": 1,
        "analysisId": "fixture-analysis",
        "documentSha256": hashlib.sha256(content).hexdigest(),
        "expectedSubjects": expected,
    }


class API:
    def __init__(self, content):
        self.content = content
        self.results = []
        self.actions = []

    def request(self, identity, action, data=None):
        self.actions.append(action)
        if action == "claim":
            return json.dumps({"status": "PROCESSING", "attemptNumber": 1}).encode()
        if action == "document":
            return self.content
        self.results.append(data)
        return b'{"status":"accepted"}'


def test_native_job_is_bound_to_hash_and_attempt():
    content, job = native_job()
    api = API(content)
    assert process(job, api, 30, 0.85)
    assert api.results[0]["status"] == "SUCCEEDED"
    assert api.results[0]["attemptNumber"] == 1
    assert len(api.results[0]["subjects"]) == 3


def test_mismatched_private_object_fails_safely():
    _, job = native_job()
    api = API(b"different object")
    assert process(job, api, 30, 0.85)
    assert api.results[0]["failureCode"] == "PDF_UNREADABLE"
    assert api.results[0]["subjects"] == []


def test_shutdown_retains_job_for_replay():
    content, job = native_job()
    api = API(content)
    stop = Event()
    stop.set()
    assert not process(job, api, 30, 0.85, stop)
    assert api.results == []


def test_terminal_replay_does_not_rerun_document():
    _, job = native_job()

    class Terminal:
        def request(self, identity, action, data=None):
            assert action == "claim"
            return b'{"status":"SKIP"}'

    assert process(job, Terminal(), 30, 0.85)


def test_process_timeout_is_bounded_and_reports_safe_code(monkeypatch):
    import subprocess
    import sys

    from ifri_ocr import jobs

    content, job = native_job()
    api = API(content)
    original = subprocess.Popen

    def delayed_runner(command, **kwargs):
        return original([sys.executable, "-c", "import time; time.sleep(5)"], **kwargs)

    monkeypatch.setattr(jobs.subprocess, "Popen", delayed_runner)
    assert process(job, api, 0.05, 0.85)
    assert api.results[0]["failureCode"] == "ANALYSIS_TIMEOUT"
    assert api.results[0]["subjects"] == []
