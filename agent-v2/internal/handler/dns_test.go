package handler

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNewDNSResolverUsesConfiguredServer(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen dns: %v", err)
	}
	defer conn.Close()

	queries := make(chan string, 4)
	go serveTestDNS(conn, queries)

	resolver := NewDNSResolver([]string{conn.LocalAddr().String()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addrs, err := resolver.LookupIPAddr(ctx, "custom.test")
	if err != nil {
		t.Fatalf("LookupIPAddr: %v", err)
	}
	if len(addrs) == 0 {
		t.Fatal("expected DNS answers")
	}

	gotAnswer := false
	for _, addr := range addrs {
		if addr.IP.String() == "203.0.113.7" || addr.IP.String() == "2001:db8::7" {
			gotAnswer = true
			break
		}
	}
	if !gotAnswer {
		t.Fatalf("answers = %v, want response from configured DNS server", addrs)
	}

	select {
	case host := <-queries:
		if host != "custom.test" {
			t.Fatalf("query host = %q, want custom.test", host)
		}
	case <-ctx.Done():
		t.Fatal("configured DNS server was not queried")
	}
}

func serveTestDNS(conn net.PacketConn, queries chan<- string) {
	buf := make([]byte, 512)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		query := append([]byte(nil), buf[:n]...)
		host, qType, questionEnd, ok := parseTestDNSQuestion(query)
		if !ok {
			continue
		}
		select {
		case queries <- host:
		default:
		}

		response := buildTestDNSResponse(query, questionEnd, qType)
		_, _ = conn.WriteTo(response, addr)
	}
}

func parseTestDNSQuestion(query []byte) (string, uint16, int, bool) {
	if len(query) < 17 {
		return "", 0, 0, false
	}
	pos := 12
	labels := make([]string, 0, 4)
	for {
		if pos >= len(query) {
			return "", 0, 0, false
		}
		labelLen := int(query[pos])
		pos++
		if labelLen == 0 {
			break
		}
		if pos+labelLen > len(query) {
			return "", 0, 0, false
		}
		labels = append(labels, string(query[pos:pos+labelLen]))
		pos += labelLen
	}
	if pos+4 > len(query) {
		return "", 0, 0, false
	}
	qType := uint16(query[pos])<<8 | uint16(query[pos+1])
	return stringsJoin(labels, "."), qType, pos + 4, true
}

func buildTestDNSResponse(query []byte, questionEnd int, qType uint16) []byte {
	response := make([]byte, 0, questionEnd+32)
	response = append(response, query[0], query[1], 0x81, 0x80)
	response = append(response, 0x00, 0x01)
	response = append(response, 0x00, 0x01)
	response = append(response, 0x00, 0x00, 0x00, 0x00)
	response = append(response, query[12:questionEnd]...)
	response = append(response, 0xc0, 0x0c)
	if qType == 28 {
		response = append(response, 0x00, 0x1c, 0x00, 0x01)
		response = append(response, 0x00, 0x00, 0x00, 0x3c)
		response = append(response, 0x00, 0x10)
		response = append(response, net.ParseIP("2001:db8::7").To16()...)
		return response
	}
	response = append(response, 0x00, 0x01, 0x00, 0x01)
	response = append(response, 0x00, 0x00, 0x00, 0x3c)
	response = append(response, 0x00, 0x04)
	response = append(response, net.ParseIP("203.0.113.7").To4()...)
	return response
}

func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, part := range parts[1:] {
		result += sep + part
	}
	return result
}
