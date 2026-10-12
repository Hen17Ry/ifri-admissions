FROM python:3.14.8-slim-bookworm
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
WORKDIR /app
COPY pyproject.toml ./
COPY src ./src
RUN pip install --no-cache-dir . && useradd --uid 10001 --create-home app
USER app
ENTRYPOINT ["python", "-m", "ifri_ocr.worker"]
