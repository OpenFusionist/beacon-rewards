# Endurance Rewards Service

A high-performance validator reward statistics service for Ethereum validators. Tracks and caches validator rewards, providing a RESTful API for querying reward data and deposit analytics.

## Quick Start

### Installation

```bash
git clone <repository-url>
cd endurance-rewards
make deps
```

### Configuration

Set environment variables (or create `.env` file):

```bash
BEACON_NODE_URL=http://localhost:5052
EXECUTION_NODE_URL=http://localhost:8545
DORA_PG_URL=postgres://postgres:postgres@127.0.0.1:5432/dora?sslmode=disable
```

### Run

```bash
make run
```

Or with Docker:

```bash
docker build -t endurance-rewards .
docker run -p 8080:8080 --env-file .env endurance-rewards
docker run -p 8080:8080   --env-file $(pwd)/.env   -v $(pwd)/data:/app/data   --restart=unless-stopped   --name endurance-rewards   -d   endurance-rewards
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_PORT` | HTTP server port | `8080` |
| `BEACON_NODE_URL` | Beacon chain node URL | `http://localhost:5052` |
| `EXECUTION_NODE_URL` | Execution layer node URL | `http://localhost:8545` |
| `DORA_PG_URL` | Dora Postgres URL (required for deposit endpoints) | `postgres://postgres:postgres@127.0.0.1:5432/dora?sslmode=disable` |
| `DEPOSITOR_LABELS_FILE` | Path to YAML file mapping addresses to labels | `depositor-name.yaml` |
| `START_EPOCH` | Epoch to backfill from (0 = cache window start) | `0` |
| `BACKFILL_CONCURRENCY` | Number of workers for epoch backfill | `16` |
| `LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |

## API Endpoints

Swagger documentation: `http://localhost:8080/swagger/index.html`

### Health Check
```
GET /health
```

### Get Validator Rewards
```
POST /rewards
Body: {"validators": [1, 2, 3]}
```

### Get Network Rewards
```
GET /rewards/network
```

### Get Address Rewards
```
POST /rewards/by-address?include_validator_indices=false
Body: {"address": "0xabc123..."}
```

### Top Withdrawal Addresses
```
GET /deposits/top-withdrawals?limit=100&sort_by=total_deposit&order=desc
```

### Top Depositor Addresses
```
GET /deposits/top-deposits?limit=100&sort_by=total_deposit&order=desc
```

## Development

```bash
make test    # Run tests
make lint    # Run linter
make build   # Build binary
make clean   # Clean artifacts
```
