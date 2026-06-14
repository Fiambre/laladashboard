FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /laladashboard .

FROM alpine:3.21
RUN apk add --no-cache tzdata ca-certificates
ARG TARGETARCH=amd64
RUN wget -O /usr/local/bin/go2rtc \
    "https://github.com/AlexxIT/go2rtc/releases/latest/download/go2rtc_linux_${TARGETARCH}" \
    && chmod +x /usr/local/bin/go2rtc
WORKDIR /app
COPY --from=builder /laladashboard .
EXPOSE 8080
ENTRYPOINT ["/app/laladashboard"]
