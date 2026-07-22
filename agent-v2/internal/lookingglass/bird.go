package lookingglass

import (
	"context"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/birdctl"
)

type birdClient struct {
	socketPath     string
	maxOutputBytes int
}

func (c birdClient) Query(ctx context.Context, query string) (string, error) {
	return birdctl.Client{
		SocketPath:     c.socketPath,
		MaxOutputBytes: c.maxOutputBytes,
		Restrict:       true,
	}.Query(ctx, query)
}
