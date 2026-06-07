FROM golang:1.25-alpine AS build

WORKDIR /app

COPY go.mod ./
COPY main.go ./
COPY public ./public

RUN go build -o /out/local-web-nav .

FROM alpine:3.22

WORKDIR /app

ENV PORT=3210
ENV DATA_DIR=/data

COPY --from=build /out/local-web-nav /app/local-web-nav

RUN adduser -D -H -u 10001 appuser \
    && mkdir -p /data \
    && chown -R appuser:appuser /app /data

USER appuser

EXPOSE 3210

CMD ["/app/local-web-nav"]
