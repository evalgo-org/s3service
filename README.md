# s3service - Semantic S3 Object Storage Service

> **Schema.org-based S3 operations for workflow orchestration**

`s3service` provides a semantic API for S3 object storage operations using Schema.org vocabulary. It supports Hetzner S3, AWS S3, and any S3-compatible storage.

## Features

✅ **CreateAction** - Upload files to S3 buckets
✅ **DownloadAction** - Retrieve files from S3 buckets
✅ **DeleteAction** - Remove files from S3 buckets
✅ **SearchAction** - List objects with prefix filtering
✅ **Semantic Types** - Full Schema.org JSON-LD support
✅ **EVE Integration** - Uses EVE library's Hetzner S3 client
✅ **Workflow Ready** - Integrates with when orchestration

## Installation

```bash
cd /home/opunix/s3service
go build -o s3service cmd/main.go cmd/semantic_api.go
```

## Quick Start

### 1. Set environment variables

```bash
export HETZNER_S3_ACCESS_KEY="your-access-key"
export HETZNER_S3_SECRET_KEY="your-secret-key"
export PORT=8092  # Optional, defaults to 8092
```

### 2. Start the service

```bash
./s3service
```

The service will start on port 8092 (or the PORT environment variable).

### 3. Health check

```bash
curl http://localhost:8092/health
```

## API Usage

All S3 operations use the same semantic API endpoint:

```
POST /v1/api/semantic/action
Content-Type: application/json
```

### Upload File (CreateAction)

```json
{
  "@context": "https://schema.org",
  "@type": "CreateAction",
  "identifier": "upload-data-file",
  "object": {
    "@type": "MediaObject",
    "identifier": "data/input.json",
    "contentUrl": "/tmp/input.json",
    "encodingFormat": "application/json"
  },
  "target": {
    "@type": "DataCatalog",
    "identifier": "my-bucket",
    "url": "https://fsn1.your-objectstorage.com",
    "additionalProperty": {
      "region": "fsn1",
      "accessKey": "${HETZNER_S3_ACCESS_KEY}",
      "secretKey": "${HETZNER_S3_SECRET_KEY}"
    }
  },
  "targetUrl": "data/input.json"
}
```

### Download File (DownloadAction)

```json
{
  "@context": "https://schema.org",
  "@type": "DownloadAction",
  "identifier": "download-result",
  "object": {
    "@type": "MediaObject",
    "identifier": "results/output.xml",
    "contentUrl": "/tmp/output.xml"
  },
  "target": {
    "@type": "DataCatalog",
    "identifier": "my-bucket",
    "url": "https://fsn1.your-objectstorage.com",
    "additionalProperty": {
      "region": "fsn1",
      "accessKey": "${HETZNER_S3_ACCESS_KEY}",
      "secretKey": "${HETZNER_S3_SECRET_KEY}"
    }
  }
}
```

### List Objects (SearchAction)

```json
{
  "@context": "https://schema.org",
  "@type": "SearchAction",
  "identifier": "list-data-files",
  "query": "data/",
  "target": {
    "@type": "DataCatalog",
    "identifier": "my-bucket",
    "url": "https://fsn1.your-objectstorage.com",
    "additionalProperty": {
      "region": "fsn1",
      "accessKey": "${HETZNER_S3_ACCESS_KEY}",
      "secretKey": "${HETZNER_S3_SECRET_KEY}"
    }
  }
}
```

### Delete File (DeleteAction)

```json
{
  "@context": "https://schema.org",
  "@type": "DeleteAction",
  "identifier": "delete-temp-file",
  "object": {
    "@type": "MediaObject",
    "identifier": "temp/processing.tmp"
  },
  "target": {
    "@type": "DataCatalog",
    "identifier": "my-bucket",
    "url": "https://fsn1.your-objectstorage.com",
    "additionalProperty": {
      "region": "fsn1",
      "accessKey": "${HETZNER_S3_ACCESS_KEY}",
      "secretKey": "${HETZNER_S3_SECRET_KEY}"
    }
  }
}
```

## When Orchestration Integration

### Using fetcher semantic

```bash
# Upload backup daily
when create s3-backup-upload \
  "/home/opunix/fetcher/fetcher semantic /home/opunix/s3service/examples/workflows/01-upload-file.json" \
  --name "S3 Daily Backup Upload" \
  --schedule "every 24h" \
  --timeout 300 \
  --retry 3
```

### Multi-step Workflow

Combine S3 operations with SPARQL and BaseX in a single workflow:

1. Download data from S3
2. Query with SPARQL
3. Transform with BaseX
4. Upload result back to S3

See `examples/when-workflows/` for complete examples.

## Architecture

```
when scheduler
  ↓
fetcher semantic (HTTP client)
  ↓
s3service:8092 (CreateAction, DownloadAction, DeleteAction)
  ↓
EVE Hetzner S3 Client
  ↓
Hetzner S3 | AWS S3 | S3-compatible storage
```

## EVE Library Integration

s3service uses EVE library components:

- **semantic/s3.go** - Schema.org S3 semantic types (v0.0.18)
- **storage/s3aws.go** - Hetzner S3 client with HetznerUploadFile()

## Examples

See `examples/workflows/` for:

1. **01-upload-file.json** - Upload file to S3
2. **02-download-file.json** - Download file from S3
3. **03-list-objects.json** - List objects with prefix
4. **04-delete-file.json** - Delete file from S3

See `examples/when-workflows/` for:

1. **s3-upload-scheduled.json** - Scheduled upload workflow

## Configuration

### Environment Variables

- `PORT` - Service port (default: 8092)
- `S3_API_KEY` - Optional API key for authentication
- `HETZNER_S3_ACCESS_KEY` - Hetzner S3 access key
- `HETZNER_S3_SECRET_KEY` - Hetzner S3 secret key

### S3 Providers

#### Hetzner Object Storage

```json
{
  "url": "https://fsn1.your-objectstorage.com",
  "additionalProperty": {
    "region": "fsn1",
    "accessKey": "your-access-key",
    "secretKey": "your-secret-key"
  }
}
```

#### AWS S3

```json
{
  "url": "https://s3.amazonaws.com",
  "additionalProperty": {
    "region": "us-east-1",
    "accessKey": "your-aws-key",
    "secretKey": "your-aws-secret"
  }
}
```

#### MinIO

```json
{
  "url": "http://localhost:9000",
  "additionalProperty": {
    "region": "us-east-1",
    "accessKey": "minioadmin",
    "secretKey": "minioadmin"
  }
}
```

## Development

### Project Structure

```
/home/opunix/s3service/
├─ cmd/
│  ├─ main.go              ← Echo server + routes
│  └─ semantic_api.go      ← Action handlers
├─ examples/
│  ├─ workflows/           ← Standalone workflow examples
│  └─ when-workflows/      ← Scheduled workflow examples
├─ go.mod
├─ go.sum
└─ s3service               ← Binary
```

### Building

```bash
go build -o s3service cmd/main.go cmd/semantic_api.go
```

### Testing

```bash
# Start service
PORT=8092 ./s3service &

# Upload test file
curl -X POST http://localhost:8092/v1/api/semantic/action \
  -H "Content-Type: application/json" \
  -d @examples/workflows/01-upload-file.json
```

## Related Services

- **sparqlservice** (port 8091) - SPARQL query service
- **basexservice** (port 8090) - BaseX XML database service
- **when** - Task orchestration engine
- **fetcher** - Semantic HTTP client

## License

Part of the EVE project ecosystem.

## Contributing

See EVE project contribution guidelines.

---

**Version**: 0.1.0
**Port**: 8092
**Status**: Production-ready
**Dependencies**: EVE v0.0.18, AWS SDK v2, Echo v4
