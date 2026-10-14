# Local entry points. `make check` is the CI of record until a Git remote exists.
.PHONY: check check-static check-stack check-browser fixtures ocr-private ocr-engines-benchmark

check:
	infra/scripts/check.sh all

check-static:
	infra/scripts/check.sh static

check-stack:
	infra/scripts/check.sh stack

check-browser:
	infra/scripts/check.sh browser

# Deliberately refresh the committed synthetic fixtures (the test profile only compares them).
fixtures:
	docker compose --profile test run --build --rm \
		--volume ./services/ocr/tests/fixtures:/app/tests/fixtures:rw \
		--env OCR_FIXTURE_MODE=write ocr-tests "python ocr_fixtures.py"

# Ignored private corpus (<name>.pdf + <name>.expected.json); ARGS=--keep-debug or
# ARGS="--numeric-engine tesseract" if needed.
ocr-private:
	uv run --project services/ocr --locked --no-sync python infra/scripts/ocr_private_run.py $(ARGS)

# Realistic synthetic BAC benchmark with each digit reader (RapidOCR = production default).
ocr-engines-benchmark:
	for enabled in true false; do \
		docker compose --profile test run --build --rm --env OCR_RAPIDOCR_ENABLED=$$enabled \
			ocr-tests "python ocr_bac_benchmark.py" || exit 1; \
	done
