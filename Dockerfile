# Build stage: compile all three binaries once.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

# Runtime stage: tiny image, non-root user.
FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/ /usr/local/bin/
EXPOSE 8080
CMD ["api"]
