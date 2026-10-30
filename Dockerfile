FROM --platform=$BUILDPLATFORM golang:1.27.3 AS build
ARG TARGETOS
ARG TARGETARCH
ARG SOURCE_REVISION=unrecorded
ARG SOURCE_DIRTY=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -o /kubecompat ./cmd/kubecompat
RUN go version -m /kubecompat > /build-info.txt && printf 'sourceRevision=%s\nsourceDirty=%s\nrevisionMeaning=baseline when dirty; not a digest of uncommitted files\n' "$SOURCE_REVISION" "$SOURCE_DIRTY" > /build-metadata.txt

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /kubecompat /kubecompat
COPY LICENSE THIRD-PARTY-NOTICES.md /usr/share/kubecompat/
COPY third_party/licenses /usr/share/kubecompat/licenses
COPY data/schemas/provenance.json /usr/share/kubecompat/schema-provenance.json
COPY data/LICENSE-APACHE-2.0 data/schemas/LICENSE /usr/share/kubecompat/data-notices/
COPY --from=build /build-info.txt /build-metadata.txt /usr/share/kubecompat/
USER 65532:65532
ENTRYPOINT ["/kubecompat"]
