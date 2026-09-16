package store

import (
	"context"
	"github.com/google/uuid"
)

type CredentialRepository interface {
	Create(ctx context.Context, params CreateCredentialParams) (Credential, error)
	GetByID(ctx context.Context, id uuid.UUID) (Credential, error)
	GetByUserNodeProtocol(ctx context.Context, userID, nodeID uuid.UUID, proto string) (Credential, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]Credential, error)
	ListByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error)
	ListActiveByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error)
	// ListAll returns every credential row, used by the web admin panel.
	ListAll(ctx context.Context) ([]Credential, error)
	Update(ctx context.Context, params UpdateCredentialParams) (Credential, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
}

type credentialRepo struct {
	q *Queries
}

func NewCredentialRepository(q *Queries) CredentialRepository {
	return &credentialRepo{q: q}
}

func (r *credentialRepo) Create(ctx context.Context, params CreateCredentialParams) (Credential, error) {
	return r.q.CreateCredential(ctx, params)
}

func (r *credentialRepo) GetByID(ctx context.Context, id uuid.UUID) (Credential, error) {
	return r.q.GetCredentialByID(ctx, id)
}

func (r *credentialRepo) GetByUserNodeProtocol(ctx context.Context, userID, nodeID uuid.UUID, proto string) (Credential, error) {
	return r.q.GetCredentialByUserNodeProtocol(ctx, GetCredentialByUserNodeProtocolParams{
		UserID:   userID,
		NodeID:   nodeID,
		Protocol: proto,
	})
}

func (r *credentialRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]Credential, error) {
	return r.q.ListCredentialsByUser(ctx, userID)
}

func (r *credentialRepo) ListByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error) {
	return r.q.ListCredentialsByNode(ctx, nodeID)
}

func (r *credentialRepo) ListActiveByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error) {
	return r.q.ListActiveCredentialsByNode(ctx, nodeID)
}

func (r *credentialRepo) Update(ctx context.Context, params UpdateCredentialParams) (Credential, error) {
	return r.q.UpdateCredential(ctx, params)
}

func (r *credentialRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteCredential(ctx, id)
}

func (r *credentialRepo) ListAll(ctx context.Context) ([]Credential, error) {
	const q = `SELECT id, user_id, node_id, protocol, private_key, public_key, preshared_key,
		uuid, password, email, flow, ipv4, ipv6, dns, mtu, keepalive, allowed_ips,
		status, expires_at, awg_jc, awg_jmin, awg_jmax, awg_s1, awg_s2,
		awg_h1, awg_h2, awg_h3, awg_h4, created_at, updated_at
		FROM credentials ORDER BY created_at DESC LIMIT 500`
	rows, err := r.q.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Credential{}
	for rows.Next() {
		var i Credential
		if err := rows.Scan(
			&i.ID, &i.UserID, &i.NodeID, &i.Protocol,
			&i.PrivateKey, &i.PublicKey, &i.PresharedKey,
			&i.Uuid, &i.Password, &i.Email, &i.Flow,
			&i.Ipv4, &i.Ipv6, &i.Dns, &i.Mtu, &i.Keepalive, &i.AllowedIps,
			&i.Status, &i.ExpiresAt,
			&i.AwgJc, &i.AwgJmin, &i.AwgJmax, &i.AwgS1, &i.AwgS2,
			&i.AwgH1, &i.AwgH2, &i.AwgH3, &i.AwgH4,
			&i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (r *credentialRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	return r.q.DeleteCredentialsByUser(ctx, userID)
}

// TrafficRepository defines traffic metrics persistence operations.
