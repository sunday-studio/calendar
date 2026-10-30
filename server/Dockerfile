# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=local go mod download
COPY . .
ARG VERSION=dev
RUN mkdir -p /data-seed && CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/calendard ./cmd/calendard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/calendard /usr/local/bin/calendard
# a new named volume inherits this ownership, so the non-root user can write /data
COPY --from=build --chown=nonroot:nonroot /data-seed /data
ENV CALENDAR_ADDR=0.0.0.0:8787 CALENDAR_DB=/data/calendar.db
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["calendard"]
CMD ["serve"]
