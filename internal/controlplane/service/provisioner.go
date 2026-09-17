package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/netip"
	"sync"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func randRange(max int64) int32 {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return int32(n.Int64())
}

func generateRandomAWGParams() (jc, jmin, jmax, s1, s2 int32, h1, h2, h3, h4 int64) {
	// Jc: junk packet count (3 to 8)
	jc = 3 + randRange(6)
	// Jmin: junk packet min size (40 to 80 bytes)
	jmin = 40 + randRange(41)
	// Jmax: junk packet max size (400 to 1050 bytes) to mimic TLS/QUIC packet histograms
	jmax = 400 + randRange(651)
	// S1, S2: init/response junk padding (20 to 150 bytes)
	s1 = 20 + randRange(131)
	s2 = 20 + randRange(131)
	// H1..H4: 4 strictly unique positive 32-bit headers (10,000 to 2,147,483,647)
	genHeader := func() int64 {
		return 10000 + int64(randRange(2147400000))
	}
	h1 = genHeader()
	h2 = genHeader()
	for h2 == h1 {
		h2 = genHeader()
	}
	h3 = genHeader()
	for h3 == h1 || h3 == h2 {
		h3 = genHeader()
	}
	h4 = genHeader()
	for h4 == h1 || h4 == h2 || h4 == h3 {
		h4 = genHeader()
	}
	return
}

func allocateNextClientIP(occupied map[string]bool) (netip.Addr, error) {
	// Allocate next available IPv4 in 10.8.0.0/16 subnet, skipping 10.8.0.1 (server/gateway)
	for b2 := 0; b2 <= 255; b2++ {
		start := 1
		if b2 == 0 {
			start = 2
		}
		for b3 := start; b3 <= 254; b3++ {
			candidate := fmt.Sprintf("10.8.%d.%d", b2, b3)
			if !occupied[candidate] {
				occupied[candidate] = true
				return netip.ParseAddr(candidate)
			}
		}
	}
	return netip.Addr{}, fmt.Errorf("ip pool exhausted for node")
}

type CredentialProvisioner struct {
	credRepo store.CredentialRepository
	nodeRepo store.NodeRepository
	userRepo store.UserRepository
	// pusher refreshes node config after credential changes.
	// Offline nodes are skipped; they resync on reconnect.
	pusher func(ctx context.Context, nodeID uuid.UUID) error
	// allocMu serializes IP allocation per node within this instance.
	// Cross-instance races additionally need a DB advisory lock (see TxManager).
	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func (p *CredentialProvisioner) lockNode(nodeID uuid.UUID) func() {
	p.mu.Lock()
	if p.locks == nil {
		p.locks = make(map[uuid.UUID]*sync.Mutex)
	}
	mu, ok := p.locks[nodeID]
	if !ok {
		mu = &sync.Mutex{}
		p.locks[nodeID] = mu
	}
	p.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// SetConfigPusher installs the node config refresh callback.
func (p *CredentialProvisioner) SetConfigPusher(fn func(ctx context.Context, nodeID uuid.UUID) error) {
	p.pusher = fn
}

func (p *CredentialProvisioner) pushNodes(ctx context.Context, nodeIDs []uuid.UUID) {
	if p.pusher == nil {
		return
	}
	seen := make(map[uuid.UUID]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := p.pusher(ctx, id); err != nil {
			logger.WarnContext(ctx, "node config push failed, node resyncs on reconnect",
				"node_id", id, "error", err)
		}
	}
}

// PushNode refreshes one node config, ignoring offline nodes.
func (p *CredentialProvisioner) PushNode(ctx context.Context, nodeID uuid.UUID) {
	p.pushNodes(ctx, []uuid.UUID{nodeID})
}

// RevokeUser deletes all user credentials and pushes node updates.
func (p *CredentialProvisioner) RevokeUser(ctx context.Context, userID uuid.UUID) error {
	creds, err := p.credRepo.ListByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list user credentials: %w", err)
	}
	if err := p.credRepo.DeleteByUser(ctx, userID); err != nil {
		return fmt.Errorf("delete user credentials: %w", err)
	}
	nodeIDs := make([]uuid.UUID, 0, len(creds))
	for _, c := range creds {
		nodeIDs = append(nodeIDs, c.NodeID)
	}
	p.pushNodes(ctx, nodeIDs)
	return nil
}

// RevokeCredential deletes one credential and pushes a node update.
func (p *CredentialProvisioner) RevokeCredential(ctx context.Context, credID uuid.UUID) error {
	cred, err := p.credRepo.GetByID(ctx, credID)
	if err != nil {
		return err
	}
	if err := p.credRepo.Delete(ctx, credID); err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	p.pushNodes(ctx, []uuid.UUID{cred.NodeID})
	return nil
}

func NewCredentialProvisioner(
	credRepo store.CredentialRepository,
	nodeRepo store.NodeRepository,
	userRepo store.UserRepository,
) *CredentialProvisioner {
	return &CredentialProvisioner{
		credRepo: credRepo,
		nodeRepo: nodeRepo,
		userRepo: userRepo,
	}
}

// ProvisionUser generates default protocol credentials across all active nodes for a user.
func (p *CredentialProvisioner) ProvisionUser(ctx context.Context, userID uuid.UUID) error {
	nodes, err := p.nodeRepo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active nodes: %w", err)
	}

	for _, node := range nodes {
		unlock := p.lockNode(node.ID)
		// 1. Provision AmneziaWG / WireGuard credential
		priv, err := wgtypes.GeneratePrivateKey()
		if err == nil {
			// Query existing active credentials on this node to avoid IP collisions
			existingCreds, _ := p.credRepo.ListActiveByNode(ctx, node.ID)
			occupied := make(map[string]bool, len(existingCreds))
			for _, c := range existingCreds {
				if c.Ipv4 != nil && c.Ipv4.IsValid() {
					occupied[c.Ipv4.String()] = true
				}
			}

			clientIP, allocErr := allocateNextClientIP(occupied)
			if allocErr != nil {
				unlock()
				return fmt.Errorf("allocate client ip on node %s: %w", node.ID, allocErr)
			}

			jc, jmin, jmax, s1, s2, h1, h2, h3, h4 := generateRandomAWGParams()
			_, _ = p.credRepo.Create(ctx, store.CreateCredentialParams{
				UserID:     userID,
				NodeID:     node.ID,
				Protocol:   "amneziawg",
				PrivateKey: pgtype.Text{String: priv.String(), Valid: true},
				PublicKey:  pgtype.Text{String: priv.PublicKey().String(), Valid: true},
				Ipv4:       &clientIP,
				Status:     pgtype.Text{String: "active", Valid: true},
				AwgJc:      pgtype.Int4{Int32: jc, Valid: true},
				AwgJmin:    pgtype.Int4{Int32: jmin, Valid: true},
				AwgJmax:    pgtype.Int4{Int32: jmax, Valid: true},
				AwgS1:      pgtype.Int4{Int32: s1, Valid: true},
				AwgS2:      pgtype.Int4{Int32: s2, Valid: true},
				AwgH1:      pgtype.Int8{Int64: h1, Valid: true},
				AwgH2:      pgtype.Int8{Int64: h2, Valid: true},
				AwgH3:      pgtype.Int8{Int64: h3, Valid: true},
				AwgH4:      pgtype.Int8{Int64: h4, Valid: true},
			})
		}

		// 2. Provision VLESS Reality credential
		clientUUID := uuid.New()
		_, _ = p.credRepo.Create(ctx, store.CreateCredentialParams{
			UserID:   userID,
			NodeID:   node.ID,
			Protocol: "vless",
			Uuid:     pgtype.UUID{Bytes: clientUUID, Valid: true},
			Flow:     pgtype.Text{String: "xtls-rprx-vision", Valid: true},
			Status:   pgtype.Text{String: "active", Valid: true},
		})
		unlock()
	}

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}
	p.pushNodes(ctx, nodeIDs)

	return nil
}

// RotateUserCredentials rotates the subscription token and regenerates all protocol credentials.
func (p *CredentialProvisioner) RotateUserCredentials(ctx context.Context, userID uuid.UUID) (*store.User, error) {
	updatedUser, err := p.userRepo.RotateSubscriptionToken(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("rotate subscription token: %w", err)
	}

	// Remove old credentials
	_ = p.credRepo.DeleteByUser(ctx, userID)

	// Re-provision fresh credentials across active nodes
	if err := p.ProvisionUser(ctx, userID); err != nil {
		return nil, fmt.Errorf("re-provision credentials: %w", err)
	}

	return &updatedUser, nil
}
