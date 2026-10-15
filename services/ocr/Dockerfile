FROM python:3.14.8-slim-bookworm
# The runtime filesystem is read-only: bytecode is compiled at build time (uv for the venv,
# compileall for the stdlib), otherwise every process, healthcheck included, recompiles its imports.
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 OMP_THREAD_LIMIT=1 UV_COMPILE_BYTECODE=1
# onnxruntime 1.31 Linux wheels ship 1DS telemetry (HTTPS to a Microsoft collector): disabled.
ENV ORT_DISABLE_TELEMETRY=1
WORKDIR /app
# fonts-dejavu-core: list exports (PDF) need a font covering French and Beninese names.
RUN apt-get update && apt-get install -y --no-install-recommends tesseract-ocr tesseract-ocr-fra tesseract-ocr-eng fonts-dejavu-core && rm -rf /var/lib/apt/lists/*
RUN python -m compileall -q -j 0 /usr/local/lib/python3.14
RUN python -m pip install --no-cache-dir uv==0.12.3
COPY pyproject.toml uv.lock ./
RUN uv sync --locked --no-dev --no-install-project
# RapidOCR recognition model for BAC numeric cells (ADR 078): fetched at build time only, checked
# against the pinned SHA-256 and alone in its directory; the worker never downloads at runtime.
COPY rapidocr-models.sha256 /opt/rapidocr-models/SHA256SUMS
RUN /app/.venv/bin/python -c "import numpy; from rapidocr import RapidOCR; RapidOCR(params={'Global.log_level': 'critical', 'Global.model_root_dir': '/opt/rapidocr-models'})(numpy.full((48, 160, 3), 255, numpy.uint8), use_det=False, use_cls=False, use_rec=True)" \
    && cd /opt/rapidocr-models && sha256sum -c SHA256SUMS && test "$(ls -A | wc -l)" = 2 && chmod -R a+rX,go-w /opt/rapidocr-models
COPY src ./src
RUN uv sync --locked --no-dev && useradd --uid 10001 --create-home app
ENV PATH="/app/.venv/bin:$PATH"
USER app
ENTRYPOINT ["python", "-m", "ifri_ocr.worker"]
