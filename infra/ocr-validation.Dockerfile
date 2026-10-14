# Opt-in validation image: reuses the freshly built worker and its OCR runtime.
FROM ocr-runtime
USER root
WORKDIR /app
RUN uv sync --locked
COPY services/ocr/tests ./tests
COPY infra/scripts/ocr_fixtures.py ./ocr_fixtures.py
COPY infra/scripts/ocr_fixture_diagnostic.py ./ocr_fixture_diagnostic.py
COPY infra/scripts/ocr_scan_benchmark.py ./ocr_scan_benchmark.py
COPY infra/scripts/ocr_private_calibration.py ./ocr_private_calibration.py
USER 1000:1000
ENV UV_CACHE_DIR=/tmp/uv-cache OCR_FIXTURE_OUTPUT=/artifacts OCR_REFERENCE_URL=http://api:8080
ENTRYPOINT ["/bin/sh", "-ec"]
CMD ["python -m pytest -o cache_dir=/tmp/pytest-cache tests && python ocr_fixtures.py && python ocr_scan_benchmark.py"]
