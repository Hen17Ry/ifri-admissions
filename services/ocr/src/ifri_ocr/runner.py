import json
import os
import subprocess
import sys
from pathlib import Path

import pymupdf

from ifri_ocr.engines import production_engine
from ifri_ocr.extraction import analyze, failed


def main() -> None:
    try:
        context = json.loads(Path(sys.argv[2]).read_text())
        if isinstance(context, list):  # Older job file: expected subjects only.
            context = {"expectedSubjects": context}
        result = analyze(
            Path(sys.argv[1]),
            context["expectedSubjects"],
            production_engine(),
            float(sys.argv[3]),
            page_timeout=int(os.environ.get("OCR_PAGE_TIMEOUT_SECONDS", "30")),
            max_pages=int(os.environ.get("OCR_MAX_PAGES", "10")),
            qualification=context.get("qualificationCode"),
        )
    except subprocess.TimeoutExpired:
        result = failed("ANALYSIS_TIMEOUT")
    except pymupdf.FileDataError, pymupdf.EmptyFileError:
        result = failed("PDF_UNREADABLE")
    except Exception:
        # Parser and engine failures are never returned as raw exception text.
        result = failed("OCR_ENGINE_ERROR")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
