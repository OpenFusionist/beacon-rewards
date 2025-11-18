package server

import (
	"os"
	"strings"

	"endurance-rewards/internal/dora"

	"gopkg.in/yaml.v3"
)

func loadDepositorLabels(path string) (map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	raw := make(map[string]string)
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	labels := make(map[string]string, len(raw))
	for addr, label := range raw {
		addr = strings.TrimSpace(addr)
		if addr == "" || label == "" {
			continue
		}
		labels[strings.ToLower(addr)] = label
	}

	return labels, nil
}

func (s *Server) applyDepositorLabels(stats []dora.DepositorStat) {
	if len(stats) == 0 || len(s.depositorLabels) == 0 {
		return
	}

	for i := range stats {
		if label, ok := s.lookupDepositorLabel(stats[i].DepositorAddress); ok {
			stats[i].DepositorLabel = label
		}
	}
}

func (s *Server) lookupDepositorLabel(address string) (string, bool) {
	if len(s.depositorLabels) == 0 || strings.TrimSpace(address) == "" {
		return "", false
	}
	label, ok := s.depositorLabels[strings.ToLower(address)]
	return label, ok
}
