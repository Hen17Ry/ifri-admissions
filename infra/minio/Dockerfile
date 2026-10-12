# Community MinIO is source-only. Never depend on a removed Docker Hub image.
FROM golang:1.27.2-alpine AS server-build
RUN GOBIN=/out CGO_ENABLED=0 go install github.com/minio/minio@RELEASE.2025-10-15T17-29-55Z

FROM golang:1.27.2-alpine AS client-build
RUN GOBIN=/out CGO_ENABLED=0 go install github.com/minio/mc@RELEASE.2025-08-13T08-35-41Z

FROM alpine:3.24.2 AS server
RUN addgroup -S app && adduser -S -G app -u 10001 app && mkdir /data && chown app:app /data
COPY --from=server-build /out/minio /usr/local/bin/minio
USER app
ENTRYPOINT ["minio"]

FROM alpine:3.24.2 AS client
RUN addgroup -S app && adduser -S -G app -u 10001 app
COPY --from=client-build /out/mc /usr/local/bin/mc
USER app
ENTRYPOINT ["/bin/sh"]
