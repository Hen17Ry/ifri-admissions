FROM golang:1.27.2-alpine AS build
RUN apk add --no-cache qpdf ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /api ./cmd/api && CGO_ENABLED=0 go build -trimpath -o /db ./cmd/db

FROM build AS test
CMD ["go", "test", "-tags=integration", "-count=1", "-v", "./..."]

FROM alpine:3.24.2
RUN apk add --no-cache qpdf ca-certificates
RUN addgroup -S app && adduser -S -G app -u 10001 app
COPY --from=build /api /usr/local/bin/api
COPY --from=build /db /usr/local/bin/db
USER app
EXPOSE 8080
ENTRYPOINT ["api"]
