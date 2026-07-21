FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/api ./cmd/api

FROM golang:1.25-alpine AS goose-build
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

FROM alpine:3.21 AS migrator
COPY --from=goose-build /go/bin/goose /bin/goose
COPY migrations /migrations
ENV GOOSE_MIGRATION_DIR=/migrations
ENTRYPOINT ["/bin/goose"]
CMD ["up"]

FROM alpine:3.21

RUN adduser -D -u 10001 app
USER app
COPY --from=build /bin/api /bin/api
EXPOSE 8080
ENTRYPOINT ["/bin/api"]
