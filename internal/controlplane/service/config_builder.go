package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	agentv1 "github.com/ivanchik-byte/Simple-VPN-Builder/pkg/proto/agent/v1"
)

// WireGuardCredentialPayload encapsulates the serialized WireGuard/AmneziaWG peer parameters.
type WireGuardCredentialPayload struct {
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key,omitempty"`
	IPv4         string `json:"ipv4,omitempty"`
	IPv6         string `json:"ipv6,omitempty"`
	AllowedIPs   string `json:"allowed_ips,omitempty"`
	AwgJc        int32  `json:"awg_jc,omitempty"`
	AwgJmin      int32  `json:"awg_jmin,omitempty"`
	AwgJmax      int32  `json:"awg_jmax,omitempty"`
	AwgS1        int32  `json:"awg_s1,omitempty"`
	AwgS2        int32  `json:"awg_s2,omitempty"`
	AwgH1        int64  `json:"awg_h1,omitempty"`
	AwgH2        int64  `json:"awg_h2,omitempty"`
	AwgH3        int64  `json:"awg_h3,omitempty"`
	AwgH4        int64  `json:"awg_h4,omitempty"`
}

// ConfigBuilder constructs protobuf ConfigUpdate payloads for exit nodes.
type ConfigBuilder struct {
	nodeRepo store.NodeRepository
	credRepo store.CredentialRepository
	userRepo store.UserRepository
}

// NewConfigBuilder creates an initialized ConfigBuilder instance.
func NewConfigBuilder(
	nodeRepo store.NodeRepository,
	credRepo store.CredentialRepository,
	userRepo store.UserRepository,
) *ConfigBuilder {
	return &ConfigBuilder{
		nodeRepo: nodeRepo,
		credRepo: credRepo,
		userRepo: userRepo,
	}
}

// BuildConfig produces a full or delta ConfigUpdate protobuf message for the target node.
func (b *ConfigBuilder) BuildConfig(ctx context.Context, nodeID uuid.UUID, version int64, isFull bool) (*agentv1.ConfigUpdate, error) {
	node, err := b.nodeRepo.GetByID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("fetch node %s: %w", nodeID, err)
	}

	nodeConfig := &agentv1.NodeConfig{
		WireguardInterface: "wg0",
		WireguardSubnetV4:  "10.8.0.0/24",
		WireguardSubnetV6:  "fd00::/64",
		WireguardMtu:       1420,
		WireguardKeepalive: 25,
		DnsServers: map[string]string{
			"primary":   "1.1.1.1",
			"secondary": "8.8.8.8",
		},
	}

	creds, err := b.credRepo.ListActiveByNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list active credentials for node %s: %w", nodeID, err)
	}

	// Group credentials by user ID
	userCredMap := make(map[uuid.UUID][]store.Credential)
	for _, cred := range creds {
		userCredMap[cred.UserID] = append(userCredMap[cred.UserID], cred)
	}

	userConfigs := make([]*agentv1.NodeUserConfig, 0, len(userCredMap))
	for userID, userCreds := range userCredMap {
		user, err := b.userRepo.GetByID(ctx, userID)
		if err != nil {
			// Skip users that no longer exist
			continue
		}

		if !user.Status.Valid || user.Status.String != "active" {
			continue
		}

		if user.ExpiresAt.Valid && time.Now().After(user.ExpiresAt.Time) {
			continue
		}

		if user.TrafficLimit.Valid && user.TrafficLimit.Int64 > 0 &&
			user.TrafficUsed.Valid && user.TrafficUsed.Int64 >= user.TrafficLimit.Int64 {
			continue
		}

		credConfigs := make([]*agentv1.CredentialConfig, 0, len(userCreds))
		for _, c := range userCreds {
			payload := WireGuardCredentialPayload{
				PublicKey:    c.PublicKey.String,
				PresharedKey: c.PresharedKey.String,
			}
			if c.Ipv4 != nil {
				payload.IPv4 = c.Ipv4.String()
				payload.AllowedIPs = c.Ipv4.String() + "/32"
			}
			if c.Ipv6 != nil {
				payload.IPv6 = c.Ipv6.String()
				if payload.AllowedIPs != "" {
					payload.AllowedIPs += "," + c.Ipv6.String() + "/128"
				} else {
					payload.AllowedIPs = c.Ipv6.String() + "/128"
				}
			}

			if c.Protocol == "amneziawg" {
				if c.AwgJc.Valid {
					payload.AwgJc = c.AwgJc.Int32
				}
				if c.AwgJmin.Valid {
					payload.AwgJmin = c.AwgJmin.Int32
				}
				if c.AwgJmax.Valid {
					payload.AwgJmax = c.AwgJmax.Int32
				}
				if c.AwgS1.Valid {
					payload.AwgS1 = c.AwgS1.Int32
				}
				if c.AwgS2.Valid {
					payload.AwgS2 = c.AwgS2.Int32
				}
				if c.AwgH1.Valid {
					payload.AwgH1 = c.AwgH1.Int64
				}
				if c.AwgH2.Valid {
					payload.AwgH2 = c.AwgH2.Int64
				}
				if c.AwgH3.Valid {
					payload.AwgH3 = c.AwgH3.Int64
				}
				if c.AwgH4.Valid {
					payload.AwgH4 = c.AwgH4.Int64
				}
			}

			rawJSON, marshalErr := json.Marshal(payload)
			if marshalErr != nil {
				continue
			}

			credConfigs = append(credConfigs, &agentv1.CredentialConfig{
				CredentialId: c.ID.String(),
				Protocol:     c.Protocol,
				ConfigBytes:  rawJSON,
			})
		}

		userConfigs = append(userConfigs, &agentv1.NodeUserConfig{
			UserId:      userID.String(),
			Credentials: credConfigs,
		})
	}

	_ = node // reserved for future node-specific overrides

	return &agentv1.ConfigUpdate{
		ConfigVersion: version,
		IsFull:        isFull,
		Users:         userConfigs,
		NodeConfig:    nodeConfig,
	}, nil
}
