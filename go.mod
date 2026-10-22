module github.com/syntah/doctorchart-backend-go

go 1.27.3

require (
	github.com/ALTree/bigfloat v0.3.0
	github.com/aws/aws-sdk-go-v2 v1.47.2
	github.com/aws/aws-sdk-go-v2/credentials v1.20.8
	github.com/aws/aws-sdk-go-v2/service/s3 v1.114.2
	github.com/bycode/bycode-image-converter-go/pkg/media v0.0.0
	github.com/go-pdf/fpdf v0.9.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/joho/godotenv v1.5.1
	github.com/pdfcpu/pdfcpu v0.16.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	golang.org/x/image v0.46.0
	golang.org/x/text v0.42.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.5 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.5 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.20 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.5 // indirect
	github.com/aws/smithy-go v1.28.4 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/hhrutter/tiff v1.0.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
)

replace github.com/bycode/bycode-image-converter-go/pkg/media => ../bycode-image-email-service-go/pkg/media
