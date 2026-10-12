FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY packages/api ./packages/api
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -o /diffx-server ./cmd/diffx-server
RUN mkdir /data && chown 10001:10001 /data

FROM scratch
COPY --from=build /diffx-server /diffx-server
COPY --from=build --chown=10001:10001 /data /data
USER 10001:10001
ENV DIFFX_SERVER_HOST=0.0.0.0
ENV DIFFX_SERVER_PORT=7980
ENV DIFFX_SERVER_STATE=/data/state.db
VOLUME ["/data"]
EXPOSE 7980
STOPSIGNAL SIGTERM
ENTRYPOINT ["/diffx-server"]
