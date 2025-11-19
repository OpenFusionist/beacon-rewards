# Endurance Rewards Service

A high-performance validator reward statistics service for Ethereum validators. This service continuously tracks and caches validator rewards using the eth-rewards library, providing a RESTful API for querying reward data.

## Features

- **Real-time Reward Tracking**: Continuously processes epochs and updates validator reward data
- **In-Memory Caching**: Fast access to reward statistics with automatic cache management
- **Daily Cache Reset**: Automatic cache cleanup every 24 hours to manage memory
- **RESTful API**: Simple HTTP endpoint to query validator rewards
- **Configurable**: Environment-based configuration for flexible deployment
- **Production Ready**: Includes logging, graceful shutdown, and error handling

## Architecture

The service is structured into several components:

- **Beacon Client**: Connects to Ethereum beacon chain nodes
- **Rewards Service**: Manages reward data collection and caching
- **HTTP Server**: Provides RESTful API endpoints
- **Configuration**: Centralized configuration management

## Prerequisites

- Go 1.23 or higher
- Access to an Ethereum beacon node (e.g., Lighthouse, Prysm, Teku)
- Access to an Ethereum execution node (e.g., Geth, Nethermind, Besu)
- Docker

## Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd endurance-rewards
```

2. Install dependencies:
```bash
make deps
```

3. Configure environment variables:
```bash
cp .env.example .env
# Edit .env with your node URLs
```

## Configuration

The service can be configured using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_ADDRESS` | HTTP server bind address | `0.0.0.0` |
| `SERVER_PORT` | HTTP server port | `8080` |
| `REQUEST_TIMEOUT` | Timeout for DB-backed HTTP handlers | `10s` |
| `DEFAULT_API_LIMIT` | Default result size for deposit endpoints | `100` |
| `DEPOSITOR_LABELS_FILE` | Path to a YAML file mapping depositor addresses to labels | `depositor-name.yaml` |
| `BEACON_NODE_URL` | Beacon chain node URL | `http://localhost:5052` |
| `EXECUTION_NODE_URL` | Execution layer node URL | `http://localhost:8545` |
| `DORA_PG_URL` | Dora Postgres URL (DSN) | `postgres://postgres:postgres@127.0.0.1:5432/dora?sslmode=disable` |
| `START_EPOCH` | Epoch to backfill from (0 disables backfill) | `0` |
| `EPOCH_CHECK_INTERVAL` | Interval between live epoch check | `12s` |
| `BACKFILL_CONCURRENCY` | Number of workers for epoch backfill | `16` |
| `CACHE_RESET_INTERVAL` | How often the in-memory cache is cleared | `24h` |

## Building

Build the application:

```bash
make build
```

This creates an executable at `bin/rewards`.

## Running

### Using Make

```bash
make run
```

### Using the Binary

```bash
./bin/rewards
```

### With Custom Configuration

```bash
export BEACON_NODE_URL=https://your-beacon-node.com
export EXECUTION_NODE_URL=https://your-execution-node.com
export SERVER_PORT=9090
make run
```

### Using Docker

You can build and run the application using Docker.

#### Build the Image

```bash
docker build -t endurance-rewards .
```

#### Run the Container


```bash
docker run -p 8080:8080 \
  --env-file $(pwd)/.env \
  -v $(pwd)/data:/app/data \
  --restart=unless-stopped \
  --name endurance-rewards \
  -d \
  endurance-rewards


```

> **Note**: Ensure the `data` directory exists on your host machine and has appropriate write permissions if you encounter issues, though Docker usually handles this.

```

## API Endpoints

### Health Check

Check if the service is running:

```bash
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "time": 1699999999
}
```

### Get Rewards

Query validator rewards (returns the sum of EL+CL rewards for each validator):

```bash
POST /rewards
```

### Top Withdrawal Addresses (from deposits)

Aggregates `deposits.amount` by normalized withdrawal address (0x01/0x02 credentials share the same 20-byte EL address) and returns the top results.

```bash
GET /deposits/top-withdrawals?limit=100
```

Parameters:
- `limit` (optional, default 100): number of addresses to return

Connection string example:
```bash
export DORA_PG_URL="postgres://user:pass@host:5432/dbname?sslmode=disable"
```

Response:
```json
{
  "limit": 100,
  "results": [
    {"address": "0xabc123...", "total_amount": 123456789},
    {"address": "0xdef456...", "total_amount": 987654321}
  ]
}
```

**Request Body:**
```json
{
  "validators": [1, 2, 3]
}
```

**Parameters:**
- `validators` (required): Array of validator indices (uint64)

**Response:**
```json
{
  "validator_count": 3,
  "rewards": {
    "1": {
      "validator_index": 1,
      "cl_rewards": 1000000,
      "el_rewards_gwei": 5000000000,
      "total_rewards_gwei": 6000001000
    },
    "2": {
      "validator_index": 2,
      "cl_rewards": 950000,
      "el_rewards_gwei": 3000000000,
      "total_rewards_gwei": 3950000950
    },
    "3": { ... }
  }
}
```

**Response Fields:**
- `validator_index`: The validator index
- `cl_rewards_gwei`: Consensus Layer rewards in gwei
- `el_rewards_gwei`: Execution Layer rewards in gwei
- `total_rewards_gwei`: Total rewards (CL + EL) in gwei

**Example Requests:**

```bash
# Get rewards for a single validator
curl -X POST http://localhost:8080/rewards \
  -H "Content-Type: application/json" \
  -d '{"validators": [12345]}'

# Get rewards for multiple validators
curl -X POST http://localhost:8080/rewards \
  -H "Content-Type: application/json" \
  -d '{"validators": [100, 200, 300, 400]}'
```

### Get Address Rewards (by depositor)

Aggregate the rewards for every validator funded by the provided depositor (tx sender) addresses. This endpoint requires the Dora Postgres database to resolve validators for each address.

```bash
POST /rewards/by-address
```

**Request Body:**
```json
{
  "addresses": ["0xabc123...", "0xdef456..."]
}
```

**Response:**
```json
{
  "address_count": 2,
  "results": {
    "0xabc123...": {
      "address": "0xabc123...",
      "depositor_label": "Example Staker",
      "validator_count": 4,
      "cl_rewards_gwei": 123456,
      "el_rewards_gwei": 98765,
      "total_rewards_gwei": 222221,
      "total_effective_balance_gwei": 128000
    }
  }
}
```


## How It Works

### Reward Collection

1. **Epoch Processing**: The service runs a continuous loop that processes each epoch
2. **Reward Fetching**: For each epoch, it calls `eth_rewards.GetRewardsForEpoch()` to fetch reward data
3. **Cache Updates**: Validator rewards are stored in an in-memory cache using validator indices as keys
4. **Data Structure**: Uses `ValidatorEpochIncome` from the eth-rewards library

### Cache Management

- **Storage**: In-memory map with validator index as key
- **Updates**: Updated every epoch (~6.4 minutes on mainnet)
- **Reset**: Automatically cleared every 24 hours to manage memory
- **Thread-Safe**: Uses RWMutex for concurrent access

### Performance

- Fast in-memory lookups (O(1) access time)
- Efficient concurrent access with read-write locks
- Automatic memory management with periodic resets

## Project Structure

```
endurance-rewards/
├── cmd/
│   └── rewards/
│       └── main.go           # Application entry point
├── internal/
│   ├── beacon/
│   │   └── client.go         # Beacon chain client
│   ├── config/
│   │   └── config.go         # Configuration management
│   ├── rewards/
│   │   └── service.go        # Rewards service logic
│   └── server/
│       └── server.go         # HTTP server
├── go.mod                     # Go module definition
├── Makefile                   # Build commands
└── README.md                  # This file
```

## Development

### Running Tests

```bash
make test
```

### Linting

```bash
make lint
```

### Cleaning Build Artifacts

```bash
make clean
```

## Troubleshooting

### Connection Issues

If you encounter connection errors:

1. Verify your beacon and execution nodes are running and accessible
2. Check the URLs in your configuration
3. Ensure your nodes are fully synced
4. Check firewall rules and network connectivity

### Memory Usage

If memory usage is high:

1. Reduce the cache reset interval in configuration
2. Monitor the number of validators being tracked
3. Consider implementing additional cache size limits

### Epoch Processing Delays

If epoch processing is slow:

1. Verify network latency to your nodes
2. Check node performance and sync status
3. Ensure adequate system resources

## Contributing

Contributions are welcome! Please follow these steps:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License.

## Support

For issues, questions, or contributions, please open an issue on GitHub.
