FROM golang:1.27.3 AS build
WORKDIR /workspace/doctorchart-backend-go
COPY --from=media / /workspace/bycode-image-email-service-go/pkg/media
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /doctorchart ./cmd/api \
    && mkdir -p /storage/studies /runtime-tmp \
    && chmod 700 /storage/studies && chmod 1777 /runtime-tmp

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /doctorchart /doctorchart
COPY --from=build --chown=65532:65532 /storage /storage
# Multipart uploads larger than 1 MiB spill to os.TempDir().
COPY --from=build --chmod=1777 /runtime-tmp /tmp
USER 65532:65532
WORKDIR /app
ENV PORT=8083 STUDIES_STORAGE_DIR=/storage/studies
EXPOSE 8083
ENTRYPOINT ["/doctorchart"]
