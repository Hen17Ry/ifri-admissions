# Synthetic OCR fixtures

Generic synthetic PDFs (six native, six scanned, plus GL/DT_DWM edge cases: accents, missing
subject/coefficient, ambiguous row, alternate layout, skew, wrong subjects, degraded scan). Subject
names come from the real Go reference API; printed values belong to the fixtures only and are
never worker defaults or admission rules. No personal information or real transcript belongs here.

The generator (`infra/scripts/ocr_fixtures.py`) is deterministic: fixed metadata and trailer ID.
`docker compose --profile test run ocr-tests` regenerates them in the container's tmpfs and fails
if any byte differs from these committed copies; it never writes here. Refresh them deliberately
with `make fixtures` after changing the generator, and commit the result.

Realistic BAC general transcripts (colored bands, watermark, grey rows without rulings, written +
EPS + oral tables, photo perspective) are generated on the fly by
`tests/support/synthetic_bac.py` and are not stored. Benchmark results live in
`docs/OCR_BENCHMARK.md`, not in this directory.
