FROM golang:1.26.9-alpine3.23 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/taskflow-api ./cmd/api

FROM alpine:3.23

WORKDIR /app
COPY --from=build /out/taskflow-api /app/taskflow-api

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/taskflow-api"]
