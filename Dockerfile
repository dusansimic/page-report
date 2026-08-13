FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Declared after `go mod download` so a version change does not invalidate the
# dependency layer. Defaults keep `docker compose up --build` working with no
# build args.
ARG VERSION=dev
ARG COMMIT=""
ARG DATE=""
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/pr-server ./cmd/pr-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pr-server /pr-server
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/pr-server"]
