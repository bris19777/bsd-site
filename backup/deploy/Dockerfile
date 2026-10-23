# Build from the backup/ directory:  docker build -f deploy/Dockerfile -t bsd-backup-server .
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bsd-backup-server ./cmd/bsd-backup-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bsd-backup-server /bsd-backup-server
VOLUME /data
EXPOSE 8080
# BSD_ADMIN_KEY must be supplied at runtime (docker run -e / a secret store).
ENTRYPOINT ["/bsd-backup-server", "-addr", ":8080", "-data", "/data/store.json"]
