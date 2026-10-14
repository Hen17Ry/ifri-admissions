# Opt-in validation image: reuses the freshly built worker and its OCR runtime.
FROM ocr-runtime
USER root
WORKDIR /app
RUN uv sync --locked
COPY services/ocr/tests ./tests
COPY infra/scripts/ocr_fixtures.py ./ocr_fixtures.py
COPY infra/scripts/ocr_bac_benchmark.py ./ocr_bac_benchmark.py
COPY infra/scripts/ocr_private_eval.py ./ocr_private_eval.py
USER 1000:1000
# Generated PDFs and reports stay in the container's tmpfs: tracked fixtures are only compared.
ENV UV_CACHE_DIR=/tmp/uv-cache OCR_FIXTURE_OUTPUT=/tmp/ocr-artifacts OCR_REFERENCE_URL=http://api:8080
ENTRYPOINT ["/bin/sh", "-ec"]
CMD ["python -m pytest -o cache_dir=/tmp/pytest-cache tests && python ocr_fixtures.py && python ocr_bac_benchmark.py"]
