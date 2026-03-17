FROM golang:1.22-bookworm AS builder

RUN apt-get update && apt-get install -y gcc libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o claude-controller .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/claude-controller .

RUN mkdir -p /data

ENV BIND_HOST=0.0.0.0
ENV PORT=8080

EXPOSE ${PORT}

ENTRYPOINT ["./claude-controller", "--db", "/data/data.db"]
