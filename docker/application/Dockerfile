FROM golang:1.26-alpine AS builder

ARG APP=server
ARG GOPROXY=direct
ENV GOPROXY=${GOPROXY}
ENV GOTOOLCHAIN=local

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${APP}

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

COPY --from=builder /out/app /usr/local/bin/app

USER app
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/app"]
