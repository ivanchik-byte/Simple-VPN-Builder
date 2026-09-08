package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type NodeCredential struct {
	Cred store.Credential
	Node store.Node
}

type SubscriptionService struct {
	userRepo store.UserRepository
	credRepo store.CredentialRepository
	nodeRepo store.NodeRepository
}

func NewSubscriptionService(
	userRepo store.UserRepository,
	credRepo store.CredentialRepository,
	nodeRepo store.NodeRepository,
) *SubscriptionService {
	return &SubscriptionService{
		userRepo: userRepo,
		credRepo: credRepo,
		nodeRepo: nodeRepo,
	}
}

type UserSubscriptionInfo struct {
	UploadBytes   int64
	DownloadBytes int64
	TotalLimit    int64
	ExpireEpoch   int64
	Status        string
}

// GetUserSubscriptionInfo retrieves traffic quota and expiration info for a user.
func (s *SubscriptionService) GetUserSubscriptionInfo(ctx context.Context, token uuid.UUID) (*UserSubscriptionInfo, error) {
	user, err := s.userRepo.GetBySubscriptionToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("user not found by subscription token: %w", err)
	}

	info := &UserSubscriptionInfo{
		Status: user.Status.String,
	}

	if user.TrafficUsed.Valid {
		info.DownloadBytes = user.TrafficUsed.Int64 // Aggregate traffic used
	}
	if user.TrafficLimit.Valid {
		info.TotalLimit = user.TrafficLimit.Int64
	}
	if user.ExpiresAt.Valid {
		info.ExpireEpoch = user.ExpiresAt.Time.Unix()
	}

	return info, nil
}

// GenerateSubscriptionContent generates configs formatted for specific client applications.
func (s *SubscriptionService) GenerateSubscriptionContent(ctx context.Context, token uuid.UUID, format string) ([]byte, string, error) {
	user, err := s.userRepo.GetBySubscriptionToken(ctx, token)
	if err != nil {
		return nil, "", fmt.Errorf("user not found: %w", err)
	}

	if user.Status.Valid && user.Status.String != "active" {
		return nil, "", fmt.Errorf("subscription is inactive or suspended")
	}

	if user.ExpiresAt.Valid && user.ExpiresAt.Time.Before(time.Now()) {
		return nil, "", fmt.Errorf("subscription has expired")
	}

	// Quota enforcement (MAJ-01)
	if user.TrafficLimit.Valid && user.TrafficLimit.Int64 > 0 &&
		user.TrafficUsed.Valid && user.TrafficUsed.Int64 >= user.TrafficLimit.Int64 {
		return nil, "", fmt.Errorf("subscription traffic limit exceeded")
	}

	creds, err := s.credRepo.ListByUser(ctx, user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("list user credentials: %w", err)
	}

	nodeCreds := make([]NodeCredential, 0, len(creds))
	for _, c := range creds {
		if c.Status.Valid && c.Status.String != "active" {
			continue
		}
		node, err := s.nodeRepo.GetByID(ctx, c.NodeID)
		if err != nil {
			continue
		}
		nodeCreds = append(nodeCreds, NodeCredential{Cred: c, Node: node})
	}

	switch strings.ToLower(format) {
	case "wireguard", "wg":
		out, err := s.formatWireGuardConf(nodeCreds, false)
		return out, "text/plain; charset=utf-8", err

	case "amneziawg", "awg", "amnezia":
		out, err := s.formatWireGuardConf(nodeCreds, true)
		return out, "text/plain; charset=utf-8", err

	case "singbox", "sing-box":
		out, err := s.formatSingbox(nodeCreds)
		return out, "application/json", err

	case "clash", "clash-meta", "mihomo":
		out, err := s.formatClashMeta(nodeCreds)
		return out, "application/x-yaml", err

	case "json":
		out, err := s.formatRawJSON(nodeCreds)
		return out, "application/json", err

	default: // base64 (canonical V2Ray/Shadowrocket/Nekobox format)
		out, err := s.formatBase64(nodeCreds)
		return out, "text/plain; charset=utf-8", err
	}
}

func parseNodeHostPort(endpoint, defaultPort string) (string, string) {
	if endpoint == "" {
		return "", defaultPort
	}
	h, p, err := net.SplitHostPort(endpoint)
	if err == nil {
		return h, p
	}
	// If net.SplitHostPort failed, check if endpoint is a bare IPv6 address
	if ip := net.ParseIP(endpoint); ip != nil && ip.To4() == nil {
		return fmt.Sprintf("[%s]", endpoint), defaultPort
	}
	return endpoint, defaultPort
}

func (s *SubscriptionService) formatBase64(items []NodeCredential) ([]byte, error) {
	var lines []string

	for _, item := range items {
		c := item.Cred
		n := item.Node
		host, port := parseNodeHostPort(n.Endpoint, "443")
		if host == "" {
			host = n.Name
		}

		switch c.Protocol {
		case "vless":
			clientUUID := ""
			if c.Uuid.Valid {
				id, _ := uuid.FromBytes(c.Uuid.Bytes[:])
				clientUUID = id.String()
			}
			flow := "xtls-rprx-vision"
			if c.Flow.Valid && c.Flow.String != "" {
				flow = c.Flow.String
			}
			sni := "swdist.apple.com"
			pbk := "pL3XYZ1234567890abcdefghijklmnopqrstuvwxyz="
			if c.PublicKey.Valid && c.PublicKey.String != "" {
				pbk = c.PublicKey.String
			}
			sid := "0123456789abcdef"

			remark := url.QueryEscape(fmt.Sprintf("%s | VLESS Reality", n.Name))
			uri := fmt.Sprintf("vless://%s@%s:%s?encryption=none&flow=%s&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp#%s",
				clientUUID, host, port, flow, sni, pbk, sid, remark)
			lines = append(lines, uri)

		case "wireguard", "amneziawg":
			remark := url.QueryEscape(fmt.Sprintf("%s | %s", n.Name, strings.ToUpper(c.Protocol)))
			privKey := c.PrivateKey.String
			peerIP := ""
			if c.Ipv4 != nil {
				peerIP = c.Ipv4.String()
			}
			pubKey := n.PublicKey
			_, wgPort := parseNodeHostPort(n.Endpoint, "51820")

			uri := fmt.Sprintf("wireguard://%s@%s:%s?address=%s/32&publickey=%s",
				privKey, host, wgPort, peerIP, pubKey)
			if c.Protocol == "amneziawg" {
				if c.AwgJc.Valid {
					uri += fmt.Sprintf("&jc=%d", c.AwgJc.Int32)
				}
				if c.AwgJmin.Valid {
					uri += fmt.Sprintf("&jmin=%d", c.AwgJmin.Int32)
				}
				if c.AwgJmax.Valid {
					uri += fmt.Sprintf("&jmax=%d", c.AwgJmax.Int32)
				}
				if c.AwgS1.Valid {
					uri += fmt.Sprintf("&s1=%d", c.AwgS1.Int32)
				}
				if c.AwgS2.Valid {
					uri += fmt.Sprintf("&s2=%d", c.AwgS2.Int32)
				}
				if c.AwgH1.Valid {
					uri += fmt.Sprintf("&h1=%d", c.AwgH1.Int64)
				}
				if c.AwgH2.Valid {
					uri += fmt.Sprintf("&h2=%d", c.AwgH2.Int64)
				}
				if c.AwgH3.Valid {
					uri += fmt.Sprintf("&h3=%d", c.AwgH3.Int64)
				}
				if c.AwgH4.Valid {
					uri += fmt.Sprintf("&h4=%d", c.AwgH4.Int64)
				}
			}
			uri += "#" + remark
			lines = append(lines, uri)
		}
	}

	payload := strings.Join(lines, "\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	return []byte(encoded), nil
}

func (s *SubscriptionService) formatSingbox(items []NodeCredential) ([]byte, error) {
	var outbounds []map[string]any

	for _, item := range items {
		c := item.Cred
		n := item.Node
		host, portStr := parseNodeHostPort(n.Endpoint, "443")
		if host == "" {
			host = n.Name
		}
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)
		if port <= 0 {
			port = 443
		}

		if c.Protocol == "vless" {
			clientUUID := ""
			if c.Uuid.Valid {
				id, _ := uuid.FromBytes(c.Uuid.Bytes[:])
				clientUUID = id.String()
			}
			pbk := "pL3XYZ1234567890abcdefghijklmnopqrstuvwxyz="
			if c.PublicKey.Valid && c.PublicKey.String != "" {
				pbk = c.PublicKey.String
			}

			outbounds = append(outbounds, map[string]any{
				"type":        "vless",
				"tag":         fmt.Sprintf("%s | Reality", n.Name),
				"server":      host,
				"server_port": port,
				"uuid":        clientUUID,
				"flow":        "xtls-rprx-vision",
				"network":     "tcp",
				"tls": map[string]any{
					"enabled":     true,
					"server_name": "swdist.apple.com",
					"utls": map[string]any{
						"enabled":     true,
						"fingerprint": "chrome",
					},
					"reality": map[string]any{
						"enabled":    true,
						"public_key": pbk,
						"short_id":   "0123456789abcdef",
					},
				},
			})
		} else if c.Protocol == "wireguard" || c.Protocol == "amneziawg" {
			peerIP := ""
			if c.Ipv4 != nil {
				peerIP = c.Ipv4.String()
			}
			_, wgPortStr := parseNodeHostPort(n.Endpoint, "51820")
			var wgPort int
			_, _ = fmt.Sscanf(wgPortStr, "%d", &wgPort)
			if wgPort <= 0 {
				wgPort = 51820
			}

			outbounds = append(outbounds, map[string]any{
				"type":            "wireguard",
				"tag":             fmt.Sprintf("%s | %s", n.Name, strings.ToUpper(c.Protocol)),
				"server":          host,
				"server_port":     wgPort,
				"local_address":   []string{peerIP + "/32"},
				"private_key":     c.PrivateKey.String,
				"peer_public_key": n.PublicKey,
			})
		}
	}

	outbounds = append(outbounds, map[string]any{
		"type": "direct",
		"tag":  "direct",
	}, map[string]any{
		"type": "block",
		"tag":  "block",
	}, map[string]any{
		"type": "dns",
		"tag":  "dns-out",
	})

	var proxyTags []string
	for _, o := range outbounds {
		if tag, ok := o["tag"].(string); ok && tag != "direct" && tag != "block" && tag != "dns-out" {
			proxyTags = append(proxyTags, tag)
		}
	}

	if len(proxyTags) > 0 {
		var allSelect []string
		allSelect = append(allSelect, "Auto-Latency")
		allSelect = append(allSelect, proxyTags...)
		allSelect = append(allSelect, "direct")

		selectorGroup := map[string]any{
			"type":      "selector",
			"tag":       "Proxy-Select",
			"outbounds": allSelect,
			"default":   "Auto-Latency",
		}
		urlTestGroup := map[string]any{
			"type":      "urltest",
			"tag":       "Auto-Latency",
			"outbounds": proxyTags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "3m",
			"tolerance": 50,
		}
		outbounds = append([]map[string]any{selectorGroup, urlTestGroup}, outbounds...)
	}

	cfg := map[string]any{
		"version": 1,
		"log": map[string]any{
			"level":     "warn",
			"timestamp": true,
		},
		"dns": map[string]any{
			"servers": []map[string]any{
				{
					"tag":              "remote-dns",
					"address":          "https://1.1.1.1/dns-query",
					"address_resolver": "local-dns",
					"detour":           "Proxy-Select",
				},
				{
					"tag":     "local-dns",
					"address": "local",
					"detour":  "direct",
				},
				{
					"tag":     "fakeip-dns",
					"address": "fakeip",
				},
			},
			"rules": []map[string]any{
				{
					"outbound":      "any",
					"server":        "local-dns",
					"disable_cache": true,
				},
				{
					"query_type": []string{"A", "AAAA"},
					"server":     "fakeip-dns",
				},
			},
			"fakeip": map[string]any{
				"enabled":     true,
				"inet4_range": "198.18.0.0/15",
			},
			"independent_cache": true,
		},
		"inbounds": []map[string]any{
			{
				"type":                       "tun",
				"tag":                        "tun-in",
				"interface_name":             "tun0",
				"inet4_address":              "172.19.0.1/30",
				"auto_route":                 true,
				"strict_route":               true,
				"stack":                      "mixed",
				"sniff":                      true,
				"sniff_override_destination": true,
			},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": []map[string]any{
				{
					"protocol": "dns",
					"outbound": "dns-out",
				},
				{
					"ip_is_private": true,
					"outbound":      "direct",
				},
				{
					"geosite": []string{"category-gov-ru", "ru"},
					"outbound": "direct",
				},
				{
					"geoip": []string{"ru"},
					"outbound": "direct",
				},
			},
			"auto_detect_interface": true,
			"final":                 "Proxy-Select",
		},
	}

	return json.MarshalIndent(cfg, "", "  ")
}

func (s *SubscriptionService) formatClashMeta(items []NodeCredential) ([]byte, error) {
	var proxies []map[string]any
	var proxyNames []string

	for _, item := range items {
		c := item.Cred
		n := item.Node
		host, portStr := parseNodeHostPort(n.Endpoint, "443")
		if host == "" {
			host = n.Name
		}
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)
		if port <= 0 {
			port = 443
		}

		if c.Protocol == "vless" {
			clientUUID := ""
			if c.Uuid.Valid {
				id, _ := uuid.FromBytes(c.Uuid.Bytes[:])
				clientUUID = id.String()
			}
			pbk := "pL3XYZ1234567890abcdefghijklmnopqrstuvwxyz="
			if c.PublicKey.Valid && c.PublicKey.String != "" {
				pbk = c.PublicKey.String
			}

			proxyName := fmt.Sprintf("%s | Reality", n.Name)
			proxies = append(proxies, map[string]any{
				"name":               proxyName,
				"type":               "vless",
				"server":             host,
				"port":               port,
				"uuid":               clientUUID,
				"network":            "tcp",
				"udp":                true,
				"tls":                true,
				"flow":               "xtls-rprx-vision",
				"servername":         "swdist.apple.com",
				"client-fingerprint": "chrome",
				"reality-opts": map[string]any{
					"public-key": pbk,
					"short-id":   "0123456789abcdef",
				},
			})
			proxyNames = append(proxyNames, proxyName)
		} else if c.Protocol == "wireguard" || c.Protocol == "amneziawg" {
			peerIP := ""
			if c.Ipv4 != nil {
				peerIP = c.Ipv4.String()
			}
			_, wgPortStr := parseNodeHostPort(n.Endpoint, "51820")
			var wgPort int
			_, _ = fmt.Sscanf(wgPortStr, "%d", &wgPort)
			if wgPort <= 0 {
				wgPort = 51820
			}

			proxyName := fmt.Sprintf("%s | %s", n.Name, strings.ToUpper(c.Protocol))
			proxies = append(proxies, map[string]any{
				"name":        proxyName,
				"type":        "wireguard",
				"server":      host,
				"port":        wgPort,
				"ip":          peerIP,
				"public-key":  n.PublicKey,
				"private-key": c.PrivateKey.String,
				"udp":         true,
			})
			proxyNames = append(proxyNames, proxyName)
		}
	}

	var proxyGroups []map[string]any
	if len(proxyNames) > 0 {
		var selectProxies []string
		selectProxies = append(selectProxies, "AUTO")
		selectProxies = append(selectProxies, proxyNames...)
		selectProxies = append(selectProxies, "DIRECT")

		proxyGroups = []map[string]any{
			{
				"name":    "PROXY",
				"type":    "select",
				"proxies": selectProxies,
			},
			{
				"name":      "AUTO",
				"type":      "url-test",
				"url":       "https://www.gstatic.com/generate_204",
				"interval":  300,
				"tolerance": 50,
				"proxies":   proxyNames,
			},
		}
	}

	wrapper := map[string]any{
		"port":                7890,
		"socks-port":          7891,
		"mixed-port":          7892,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "warning",
		"ipv6":                false,
		"unified-delay":       true,
		"tcp-concurrent":      true,
		"find-process-mode":   "strict",
		"proxies":             proxies,
		"proxy-groups":        proxyGroups,
		"rules": []string{
			"GEOIP,private,DIRECT,no-resolve",
			"GEOIP,LAN,DIRECT,no-resolve",
			"GEOSITE,category-gov-ru,DIRECT",
			"GEOSITE,ru,DIRECT",
			"GEOIP,RU,DIRECT,no-resolve",
			"MATCH,PROXY",
		},
		"sniffer": map[string]any{
			"enable": true,
			"sniff": map[string]any{
				"TLS": map[string]any{
					"ports": []int{443, 8443},
				},
				"HTTP": map[string]any{
					"ports":                []int{80, 8080, 8880},
					"override-destination": true,
				},
			},
		},
		"tun": map[string]any{
			"enable":                true,
			"stack":                 "mixed",
			"dns-hijack":            []string{"tcp://any:53", "udp://any:53"},
			"auto-route":            true,
			"auto-detect-interface": true,
		},
		"dns": map[string]any{
			"enable":             true,
			"listen":             "0.0.0.0:1053",
			"enhanced-mode":      "fake-ip",
			"fake-ip-range":      "198.18.0.1/16",
			"nameserver":         []string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"},
			"default-nameserver": []string{"1.1.1.1", "8.8.8.8"},
		},
	}

	return yaml.Marshal(wrapper)
}

func (s *SubscriptionService) formatRawJSON(items []NodeCredential) ([]byte, error) {
	type CleanCred struct {
		NodeName string `json:"node_name"`
		Host     string `json:"host"`
		Protocol string `json:"protocol"`
		UUID     string `json:"uuid,omitempty"`
		Flow     string `json:"flow,omitempty"`
	}

	list := make([]CleanCred, 0, len(items))
	for _, item := range items {
		uuidStr := ""
		if item.Cred.Uuid.Valid {
			id, _ := uuid.FromBytes(item.Cred.Uuid.Bytes[:])
			uuidStr = id.String()
		}
		list = append(list, CleanCred{
			NodeName: item.Node.Name,
			Host:     item.Node.Endpoint,
			Protocol: item.Cred.Protocol,
			UUID:     uuidStr,
			Flow:     item.Cred.Flow.String,
		})
	}
	return json.MarshalIndent(list, "", "  ")
}

// formatWireGuardConf generates standard RFC-compliant .conf for WireGuard,
// or extended obfuscated .conf for AmneziaWG (AWG) if allowAWG is true.
func (s *SubscriptionService) formatWireGuardConf(items []NodeCredential, allowAWG bool) ([]byte, error) {
	var target *NodeCredential
	for i := range items {
		p := strings.ToLower(items[i].Cred.Protocol)
		if allowAWG && p == "amneziawg" {
			target = &items[i]
			break
		}
		if !allowAWG && p == "wireguard" {
			target = &items[i]
			break
		}
		if p == "wireguard" || p == "amneziawg" {
			if target == nil {
				target = &items[i]
			}
		}
	}

	if target == nil {
		return nil, fmt.Errorf("no wireguard or amneziawg credentials found for this subscription")
	}

	c := target.Cred
	n := target.Node
	host, portStr := parseNodeHostPort(n.Endpoint, "51820")
	if host == "" {
		host = n.Name
	}

	if c.Ipv4 == nil || c.Ipv4.String() == "" {
		return nil, fmt.Errorf("credential %s has no allocated IPv4 address", c.ID)
	}
	peerIP := c.Ipv4.String()

	var sb strings.Builder
	sb.WriteString("# Configuration generated by Simple-VPN-Builder\n")
	sb.WriteString(fmt.Sprintf("# Node: %s | Protocol: %s\n\n", n.Name, strings.ToUpper(c.Protocol)))
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", c.PrivateKey.String))
	sb.WriteString(fmt.Sprintf("Address = %s/32\n", peerIP))
	sb.WriteString("DNS = 1.1.1.1, 8.8.8.8\n")
	sb.WriteString("MTU = 1420\n")

	// If AmneziaWG protocol and allowed, append obfuscation headers
	if allowAWG && c.Protocol == "amneziawg" {
		if c.AwgJc.Valid {
			sb.WriteString(fmt.Sprintf("Jc = %d\n", c.AwgJc.Int32))
		}
		if c.AwgJmin.Valid {
			sb.WriteString(fmt.Sprintf("Jmin = %d\n", c.AwgJmin.Int32))
		}
		if c.AwgJmax.Valid {
			sb.WriteString(fmt.Sprintf("Jmax = %d\n", c.AwgJmax.Int32))
		}
		if c.AwgS1.Valid {
			sb.WriteString(fmt.Sprintf("S1 = %d\n", c.AwgS1.Int32))
		}
		if c.AwgS2.Valid {
			sb.WriteString(fmt.Sprintf("S2 = %d\n", c.AwgS2.Int32))
		}
		if c.AwgH1.Valid {
			sb.WriteString(fmt.Sprintf("H1 = %d\n", c.AwgH1.Int64))
		}
		if c.AwgH2.Valid {
			sb.WriteString(fmt.Sprintf("H2 = %d\n", c.AwgH2.Int64))
		}
		if c.AwgH3.Valid {
			sb.WriteString(fmt.Sprintf("H3 = %d\n", c.AwgH3.Int64))
		}
		if c.AwgH4.Valid {
			sb.WriteString(fmt.Sprintf("H4 = %d\n", c.AwgH4.Int64))
		}
	}

	sb.WriteString("\n[Peer]\n")
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", n.PublicKey))
	sb.WriteString(fmt.Sprintf("Endpoint = %s:%s\n", host, portStr))
	sb.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	sb.WriteString("PersistentKeepalive = 25\n")

	return []byte(sb.String()), nil
}
