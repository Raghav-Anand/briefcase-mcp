FROM golang:1.23-alpine AS builder
WORKDIR /app

# Dependencies are vendored by the CI workflow before docker build.
# This avoids needing network access or local replace directives at image build time.
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o /mcp-server .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /mcp-server /mcp-server
EXPOSE 8080
CMD ["/mcp-server"]
