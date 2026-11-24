# syntax=docker/dockerfile:1
#
# One Dockerfile for all services:
#   docker build --build-arg SERVICE=<storefront|catalog|cart|checkout|payments-mock> .

FROM golang:1.27-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN test -n "$SERVICE" || { echo "build with --build-arg SERVICE=<name>"; exit 1; }
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# distroless: no shell or package manager, runs as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
