FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /api ./cmd/api

FROM alpine:3.24.2
RUN addgroup -S app && adduser -S -G app -u 10001 app
COPY --from=build /api /usr/local/bin/api
USER app
EXPOSE 8080
ENTRYPOINT ["api"]
