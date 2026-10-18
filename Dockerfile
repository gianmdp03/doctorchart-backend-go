FROM golang:1.27.2 AS build
WORKDIR /workspace/doctorchart-backend-go
COPY --from=media / /workspace/bycode-image-email-service-go/pkg/media
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /doctorchart ./cmd/api \
    && mkdir -p /storage/studies && chmod 700 /storage/studies

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /doctorchart /doctorchart
COPY --from=build --chown=65532:65532 /storage /storage
USER 65532:65532
WORKDIR /app
ENV PORT=8083 STUDIES_STORAGE_DIR=/storage/studies
EXPOSE 8083
ENTRYPOINT ["/doctorchart"]
