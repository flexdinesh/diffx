FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY packages/api ./packages/api
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -o /diffx-server ./cmd/diffx-server

FROM build AS fetcher-build
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -o /diffx-fetcher ./cmd/diffx-fetcher

# GitHub pull request fetcher: `docker build --target fetcher .`. Git 2.44+
# keeps collection offline; the server image below stays the default target.
FROM alpine:3.22 AS fetcher
RUN apk add --no-cache git ca-certificates && mkdir /mirrors && chown 10001:10001 /mirrors
COPY --from=fetcher-build /diffx-fetcher /diffx-fetcher
USER 10001:10001
ENV DIFFX_FETCHER_MIRRORS=/mirrors
VOLUME ["/mirrors"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["/diffx-fetcher"]

FROM build AS console-build
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -o /diffx-console ./cmd/diffx-console

# Admin console: `docker build --target console .`. Stateless like the server,
# over the same DIFFX_CONSOLE_DATABASE_URL Postgres, on its own origin.
FROM scratch AS console
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=console-build /diffx-console /diffx-console
USER 10001:10001
ENV DIFFX_CONSOLE_HOST=0.0.0.0
ENV DIFFX_CONSOLE_PORT=7970
EXPOSE 7970
STOPSIGNAL SIGTERM
ENTRYPOINT ["/diffx-console"]

# The server is stateless: all state lives in DIFFX_SERVER_DATABASE_URL's
# Postgres. CA roots let it verify TLS to Postgres, GitHub and Google.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /diffx-server /diffx-server
USER 10001:10001
ENV DIFFX_SERVER_HOST=0.0.0.0
ENV DIFFX_SERVER_PORT=7980
EXPOSE 7980
STOPSIGNAL SIGTERM
ENTRYPOINT ["/diffx-server"]
