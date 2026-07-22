package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/birdctl"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
)

// BirdCommand runs a BIRD control command string (without birdc flags).
type BirdCommand func(ctx context.Context, command string) (string, error)

func DefaultBirdCommand(socketPath string) BirdCommand {
	return func(ctx context.Context, command string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return birdctl.Query(ctx, socketPath, command)
	}
}

func TopologyHandler(cfg *config.Config, bird BirdCommand) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bird == nil {
			bird = DefaultBirdCommand(cfg.BirdCtlPath)
		}
		result := buildTopology(r.Context(), bird)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}

func buildTopology(ctx context.Context, bird BirdCommand) model.IGPTopologyResult {
	result := model.IGPTopologyResult{
		Protocol:   "babel",
		Interfaces: []model.BabelInterface{},
		Neighbors:  []model.BabelNeighbor{},
		Errors:     []string{},
	}

	ifaceOut, err := bird(ctx, "show babel interfaces")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("show babel interfaces failed: %v", err))
	} else {
		result.Interfaces = ParseBabelInterfaces(ifaceOut)
	}

	neighOut, err := bird(ctx, "show babel neighbors")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("show babel neighbors failed: %v", err))
	} else {
		result.Neighbors = ParseBabelNeighbors(neighOut)
	}

	return result
}

func ParseBabelNeighbors(output string) []model.BabelNeighbor {
	neighbors := make([]model.BabelNeighbor, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		address := net.ParseIP(parts[0])
		if address == nil {
			continue
		}
		cost, err := strconv.Atoi(parts[2])
		if err != nil {
			cost = 65535
		}
		neighbors = append(neighbors, model.BabelNeighbor{
			Interface: parts[1],
			Cost:      cost,
			Address:   address.String(),
		})
	}
	return neighbors
}

func ParseBabelInterfaces(output string) []model.BabelInterface {
	interfaces := make([]model.BabelInterface, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 8 || strings.ToLower(parts[0]) == "interface" {
			continue
		}
		iface := parts[0]
		nextHopV4Raw := parts[len(parts)-2]
		nextHopV6Raw := parts[len(parts)-1]

		var nextHopV4, nextHopV6 *string
		if ip := net.ParseIP(nextHopV4Raw); ip != nil {
			s := ip.String()
			nextHopV4 = &s
		}
		if ip := net.ParseIP(nextHopV6Raw); ip != nil {
			s := ip.String()
			nextHopV6 = &s
		}

		interfaces = append(interfaces, model.BabelInterface{
			Interface: iface,
			NextHopV4: nextHopV4,
			NextHopV6: nextHopV6,
		})
	}
	return interfaces
}
