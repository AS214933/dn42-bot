package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bingxin666/dn42-bot/agent-v2/internal/config"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/model"
	"github.com/bingxin666/dn42-bot/agent-v2/internal/service"
)

type BirdStatusTuple struct {
	State  string
	Info   string
	Routes map[string]string
}

func (b BirdStatusTuple) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{b.State, b.Info, b.Routes})
}

func (b *BirdStatusTuple) UnmarshalJSON(data []byte) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	if len(arr) < 3 {
		return fmt.Errorf("bird status tuple must have 3 elements, got %d", len(arr))
	}
	if err := json.Unmarshal(arr[0], &b.State); err != nil {
		return err
	}
	if err := json.Unmarshal(arr[1], &b.Info); err != nil {
		return err
	}
	b.Routes = make(map[string]string)
	if err := json.Unmarshal(arr[2], &b.Routes); err != nil {
		return err
	}
	return nil
}

type InfoResponse struct {
	Port            string                       `json:"port"`
	MTU             int                          `json:"mtu"`
	V6              string                       `json:"v6"`
	V4              string                       `json:"v4"`
	Clearnet        string                       `json:"clearnet"`
	PublicKey       string                       `json:"pubkey"`
	PresharedKey    string                       `json:"psk"`
	Description     string                       `json:"desc"`
	Session         string                       `json:"session"`
	SessionName     []string                     `json:"session_name"`
	MyV6            string                       `json:"my_v6"`
	MyV4            string                       `json:"my_v4"`
	MyPublicKey     string                       `json:"my_pubkey"`
	WGLastHandshake int64                        `json:"wg_last_handshake"`
	WGTransfer      []int64                      `json:"wg_transfer"`
	BirdStatus      map[string]BirdStatusTuple   `json:"bird_status"`
	NetSupport      model.NetSupport             `json:"net_support"`
	LLA             string                       `json:"lla"`
}

type InfoHandler struct {
	Cfg         *config.Config
	WGConfDir   string
	BirdConfDir string
	RunCmd      CmdRunner
}

func (h *InfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	asn, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	wgPath := filepath.Join(h.WGConfDir, fmt.Sprintf("dn42-%d.conf", asn))
	birdPath := filepath.Join(h.BirdConfDir, fmt.Sprintf("%d.conf", asn))
	wgExist := fileExists(wgPath)
	birdExist := fileExists(birdPath)

	if !wgExist && !birdExist {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if wgExist && !birdExist {
		http.Error(w, "wg only", http.StatusInternalServerError)
		return
	}
	if !wgExist && birdExist {
		http.Error(w, "bird only", http.StatusInternalServerError)
		return
	}

	wgContent, err := os.ReadFile(wgPath)
	if err != nil {
		http.Error(w, "Failed to read WG config", http.StatusInternalServerError)
		return
	}
	birdContent, err := os.ReadFile(birdPath)
	if err != nil {
		http.Error(w, "Failed to read BIRD config", http.StatusInternalServerError)
		return
	}

	wgParsed, err := service.ParseConfig(asn, string(wgContent))
	if err != nil {
		http.Error(w, "wg error", http.StatusInternalServerError)
		return
	}

	mtu := wgParsed.MTU
	if mtu == 0 {
		mtu = h.Cfg.DefaultMTU
	}

	var v6, myV6 string
	if wgParsed.PeerLLA != "" {
		v6 = wgParsed.PeerLLA
		myV6 = wgParsed.MyLLA
	} else if wgParsed.PeerULA != "" {
		v6 = wgParsed.PeerULA
		myV6 = wgParsed.MyULA
	} else {
		v6 = ""
		myV6 = wgParsed.MyULA
	}

	var myV4 string
	if wgParsed.PeerIPv4 != "" {
		myV4 = h.Cfg.MyDN42IPv4Address.String()
	}

	sessionNames, sessionDesc, birdErr := h.parseBirdSessions(asn, string(birdContent))
	if birdErr != "" {
		http.Error(w, birdErr, http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	iface := fmt.Sprintf("dn42-%d", asn)

	handshakeOutput, _ := h.RunCmd(ctx, "wg", []string{"show", iface, "latest-handshakes"}, 10*time.Second)
	wgLastHandshake, _ := service.ParseHandshake(handshakeOutput)

	transferOutput, _ := h.RunCmd(ctx, "wg", []string{"show", iface, "transfer"}, 10*time.Second)
	wgRX, wgTX, _ := service.ParseTransfer(transferOutput)

	birdStatus := make(map[string]BirdStatusTuple)
	for _, sessionName := range sessionNames {
		statusOutput, err := h.RunCmd(ctx, "birdc", []string{"-s", h.Cfg.BirdCtlPath, "show", "protocols", sessionName}, 10*time.Second)
		if err != nil {
			birdStatus[sessionName] = BirdStatusTuple{State: "N/A", Info: "", Routes: map[string]string{}}
			continue
		}

		parsed, err := service.ParseBirdStatus(sessionName, statusOutput)
		if err != nil {
			birdStatus[sessionName] = BirdStatusTuple{State: "N/A", Info: "", Routes: map[string]string{}}
			continue
		}

		tuple := BirdStatusTuple{
			State:  parsed.State,
			Info:   parsed.Info,
			Routes: map[string]string{},
		}

		if parsed.State == "Established" {
			allOutput, err := h.RunCmd(ctx, "birdc", []string{"-s", h.Cfg.BirdCtlPath, "show", "protocols", "all", sessionName}, 10*time.Second)
			if err == nil {
				channels, err := service.ParseBirdAll(sessionName, allOutput)
				if err == nil {
					for chName, props := range channels {
						if props["State"] == "UP" && props["Output filter"] == "(unnamed)" {
							if strings.HasPrefix(chName, "ipv") && len(chName) >= 4 {
								tuple.Routes[chName[3:]] = props["Routes"]
							}
						}
					}
				}
			}
		}

		birdStatus[sessionName] = tuple
	}

	resp := InfoResponse{
		Port:            strconv.Itoa(wgParsed.Port),
		MTU:             mtu,
		V6:              v6,
		V4:              wgParsed.PeerIPv4,
		Clearnet:        wgParsed.Clearnet,
		PublicKey:       wgParsed.PublicKey,
		PresharedKey:    wgParsed.PresharedKey,
		Description:     sessionDesc,
		Session:         sessionNamesToSessionType(sessionNames, string(birdContent), asn),
		SessionName:     sessionNames,
		MyV6:            myV6,
		MyV4:            myV4,
		MyPublicKey:     h.Cfg.MyWGPublicKey,
		WGLastHandshake: wgLastHandshake,
		WGTransfer:      []int64{wgRX, wgTX},
		BirdStatus:      birdStatus,
		NetSupport: model.NetSupport{
			IPv4:    h.Cfg.NetSupport.IPv4,
			IPv6:    h.Cfg.NetSupport.IPv6,
			IPv4NAT: h.Cfg.NetSupport.IPv4NAT,
			CN:      h.Cfg.NetSupport.CN,
		},
		LLA:             h.Cfg.MyDN42LinkLocalAddress.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *InfoHandler) parseBirdSessions(asn int, content string) ([]string, string, string) {
	var sessionNames []string
	desc := "N.A."

	v6Regex := regexp.MustCompile(`(?m)protocol bgp DN42_` + strconv.Itoa(asn) + `_v6 from dn42_peers \{\n(?:(?: +.*?\n)*?.*\n)+?^}`)
	v4Regex := regexp.MustCompile(`(?m)protocol bgp DN42_` + strconv.Itoa(asn) + `_v4 from dn42_peers \{\n(?:(?: +.*?\n)*?.*\n)+?^}`)
	descRegex := regexp.MustCompile(`(?m)^ {4}description "(.*)";$`)

	if v6Regex.MatchString(content) {
		sessionNames = append(sessionNames, fmt.Sprintf("DN42_%d_v6", asn))
		if m := descRegex.FindStringSubmatch(v6Regex.FindString(content)); m != nil {
			desc = m[1]
		}
	}
	if v4Regex.MatchString(content) {
		sessionNames = append(sessionNames, fmt.Sprintf("DN42_%d_v4", asn))
		if m := descRegex.FindStringSubmatch(v4Regex.FindString(content)); m != nil {
			desc = m[1]
		}
	}

	if len(sessionNames) == 0 {
		return nil, desc, "no session"
	}

	return sessionNames, desc, ""
}

func sessionNamesToSessionType(sessionNames []string, content string, asn int) string {
	v6OnlyRegex := regexp.MustCompile(
		` {4}ipv4 \{\n` +
			`(?: {4,}.*?\n)*?` +
			`(?:(?: {8}import none;\n(?: {4,}.*?\n)*? {8}export none;)|(?: {8}export none;\n(?: {4,}.*?\n)*? {8}import none;))\n` +
			`(?: {4,}.*?\n)*?` +
			` {4}\};`)
	v4OnlyRegex := regexp.MustCompile(
		` {4}ipv6 \{\n` +
			`(?: {4,}.*?\n)*?` +
			`(?:(?: {8}import none;\n(?: {4,}.*?\n)*? {8}export none;)|(?: {8}export none;\n(?: {4,}.*?\n)*? {8}import none;))\n` +
			`(?: {4,}.*?\n)*?` +
			` {4}\};`)

	hasV6 := false
	hasV4 := false
	v6Only := false
	v4Only := false

	for _, name := range sessionNames {
		if strings.HasSuffix(name, "_v6") {
			hasV6 = true
			v6BlockRegex := regexp.MustCompile(`(?m)protocol bgp DN42_` + strconv.Itoa(asn) + `_v6 from dn42_peers \{\n(?:(?: +.*?\n)*?.*\n)+?^}`)
			block := v6BlockRegex.FindString(content)
			if block != "" && v6OnlyRegex.MatchString(block) {
				v6Only = true
			}
		}
		if strings.HasSuffix(name, "_v4") {
			hasV4 = true
			v4BlockRegex := regexp.MustCompile(`(?m)protocol bgp DN42_` + strconv.Itoa(asn) + `_v4 from dn42_peers \{\n(?:(?: +.*?\n)*?.*\n)+?^}`)
			block := v4BlockRegex.FindString(content)
			if block != "" && v4OnlyRegex.MatchString(block) {
				v4Only = true
			}
		}
	}

	if hasV6 && hasV4 {
		if v6Only && v4Only {
			return "IPv6 & IPv4 Session with their own channels"
		}
		if v6Only {
			return "IPv6 Session with IPv6 channel only"
		}
		if v4Only {
			return "IPv4 Session with IPv6 & IPv4 Channels"
		}
		return "IPv6 Session with IPv6 & IPv4 Channels"
	}
	if hasV6 {
		if v6Only {
			return "IPv6 Session with IPv6 channel only"
		}
		return "IPv6 Session with IPv6 & IPv4 Channels"
	}
	if hasV4 {
		if v4Only {
			return "IPv4 Session with IPv4 channel only"
		}
		return "IPv4 Session with IPv6 & IPv4 Channels"
	}

	return ""
}
