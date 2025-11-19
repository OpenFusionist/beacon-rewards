package rewards

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/gobitfly/eth-rewards/beacon"
	"github.com/gobitfly/eth-rewards/types"
)

// NodePool manages multiple beacon clients for load balancing
type NodePool struct {
	clients []*beacon.Client
	counter uint64
}

// NewNodePool creates a new NodePool from a comma-separated list of URLs
func NewNodePool(urls string, timeout time.Duration) *NodePool {
	urlList := strings.Split(urls, ",")
	clients := make([]*beacon.Client, 0, len(urlList))
	for _, u := range urlList {
		u = strings.TrimSpace(u)
		if u != "" {
			clients = append(clients, beacon.NewClient(u, timeout))
		}
	}

	// Ensure at least one client (even if invalid URL, to avoid nil panics on empty config)
	if len(clients) == 0 {
		clients = append(clients, beacon.NewClient("", timeout))
	}

	return &NodePool{
		clients: clients,
	}
}

func (p *NodePool) getClient() *beacon.Client {
	if len(p.clients) == 0 {
		return nil
	}
	// Round robin
	idx := atomic.AddUint64(&p.counter, 1) % uint64(len(p.clients))
	return p.clients[idx]
}

// ProposerAssignments delegates to a client in the pool
func (p *NodePool) ProposerAssignments(epoch uint64) (*types.EpochProposerAssignmentsApiResponse, error) {
	return p.getClient().ProposerAssignments(epoch)
}

// AttestationRewards delegates to a client in the pool
func (p *NodePool) AttestationRewards(epoch uint64) (*types.AttestationRewardsApiResponse, error) {
	return p.getClient().AttestationRewards(epoch)
}

// ExecutionBlockNumber delegates to a client in the pool
func (p *NodePool) ExecutionBlockNumber(slot uint64) (uint64, error) {
	return p.getClient().ExecutionBlockNumber(slot)
}

// SyncCommitteeRewards delegates to a client in the pool
func (p *NodePool) SyncCommitteeRewards(slot uint64) (*types.SyncCommitteeRewardsApiResponse, error) {
	return p.getClient().SyncCommitteeRewards(slot)
}

// BlockRewards delegates to a client in the pool
func (p *NodePool) BlockRewards(slot uint64) (*types.BlockRewardsApiResponse, error) {
	return p.getClient().BlockRewards(slot)
}
