FROM golang:1.20 AS build

WORKDIR /app

COPY go.mod go.sum .

RUN go mod download

COPY . .

RUN go build -o /mog /app/cmd/mog/.

FROM debian:trixie-20231120-slim

WORKDIR /app

RUN DEBIAN_FRONTEND=noninteractive apt update && apt install -y ffmpeg && rm -rf /var/lib/apt/lists/*

COPY --from=build /mog .

CMD ["/app/mog"]