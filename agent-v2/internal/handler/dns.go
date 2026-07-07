package handler

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
)

type IPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type addrResolver interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

type dnsResolver struct {
	servers []string
	next    atomic.Uint64
}

func NewDNSResolver(servers []string) IPResolver {
	if len(servers) == 0 {
		return nil
	}
	copied := make([]string, 0, len(servers))
	for _, server := range servers {
		if server != "" {
			copied = append(copied, server)
		}
	}
	if len(copied) == 0 {
		return nil
	}
	return &dnsResolver{servers: copied}
}

func (r *dnsResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r == nil || len(r.servers) == 0 {
		return net.DefaultResolver.LookupIPAddr(ctx, host)
	}

	start := int(r.next.Add(1)-1) % len(r.servers)
	var lastErr error
	for i := 0; i < len(r.servers); i++ {
		resolver := resolverForServer(r.servers[(start+i)%len(r.servers)])
		addrs, err := resolver.LookupIPAddr(ctx, host)
		if err == nil {
			return addrs, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DNS servers configured")
	}
	return nil, lastErr
}

func (r *dnsResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	if r == nil || len(r.servers) == 0 {
		return net.DefaultResolver.LookupAddr(ctx, addr)
	}

	start := int(r.next.Add(1)-1) % len(r.servers)
	var lastErr error
	for i := 0; i < len(r.servers); i++ {
		resolver := resolverForServer(r.servers[(start+i)%len(r.servers)])
		names, err := resolver.LookupAddr(ctx, addr)
		if err == nil {
			return names, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DNS servers configured")
	}
	return nil, lastErr
}

func resolverForServer(server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, server)
		},
	}
}

func optionalResolver(resolvers []IPResolver) IPResolver {
	if len(resolvers) == 0 {
		return nil
	}
	return resolvers[0]
}

func lookupIPAddrs(ctx context.Context, resolver IPResolver, host string) ([]net.IPAddr, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return resolver.LookupIPAddr(ctx, host)
}
