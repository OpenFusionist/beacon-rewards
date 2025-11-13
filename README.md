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
| `BEACON_NODE_URL` | Beacon chain node URL | `http://localhost:5052` |
| `EXECUTION_NODE_URL` | Execution layer node URL | `http://localhost:8545` |

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

Query validator rewards:

```bash
GET /rewards?validators=1,2,3
```

**Query Parameters:**
- `validators` (optional): Comma-separated list of validator indices
  - If omitted, returns all cached rewards
  - Example: `validators=100,200,300`

**Response:**
```json
{
  "count": 3,
  "requested": 3,
  "rewards": {
    "1": {
      "TotalClRewards": 1000000,
      "AttestationReward": 500000,
      "ProposerReward": 500000,
      "SyncCommitteeReward": 0,
      "SlashingReward": 0,
      "TotalElRewards": 100000
    },
    "2": { ... },
    "3": { ... }
  }
}
```

**Example Requests:**

```bash
# Get rewards for specific validators
curl "http://localhost:8080/rewards?validators=1,2,3"

# Get all cached rewards
curl "http://localhost:8080/rewards"

# Get rewards for a single validator
curl "http://localhost:8080/rewards?validators=12345"
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
