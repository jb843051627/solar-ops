FROM golang:1.22-bookworm

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
ENV GOTOOLCHAIN=local

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /usr/local/bin/solar-ops ./...

CMD ["solar-ops"]
