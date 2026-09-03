package service

import (
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/config"
)

type Services struct {
	NodeService       *NodeService
	UserService       *UserService
	PlanService       *PlanService
	CredentialService *CredentialService
	AuthService       *AuthService
}

func NewServices(
	queries *store.Queries,
	jwtManager *auth.JWTManager,
	apiKeyManager *auth.APIKeyManager,
	passwordManager *auth.PasswordManager,
	cfg *config.Config,
) *Services {
	return &Services{
		NodeService:       NewNodeService(queries),
		UserService:       NewUserService(queries),
		PlanService:       NewPlanService(queries),
		CredentialService: NewCredentialService(queries),
		AuthService:       NewAuthService(queries, jwtManager, apiKeyManager, passwordManager),
	}
}

type NodeService struct {
	queries *store.Queries
}

func NewNodeService(queries *store.Queries) *NodeService {
	return &NodeService{queries: queries}
}

type UserService struct {
	queries *store.Queries
}

func NewUserService(queries *store.Queries) *UserService {
	return &UserService{queries: queries}
}

type PlanService struct {
	queries *store.Queries
}

func NewPlanService(queries *store.Queries) *PlanService {
	return &PlanService{queries: queries}
}

type CredentialService struct {
	queries *store.Queries
}

func NewCredentialService(queries *store.Queries) *CredentialService {
	return &CredentialService{queries: queries}
}

type AuthService struct {
	queries         *store.Queries
	jwtManager      *auth.JWTManager
	apiKeyManager   *auth.APIKeyManager
	passwordManager *auth.PasswordManager
}

func NewAuthService(
	queries *store.Queries,
	jwtManager *auth.JWTManager,
	apiKeyManager *auth.APIKeyManager,
	passwordManager *auth.PasswordManager,
) *AuthService {
	return &AuthService{
		queries:         queries,
		jwtManager:      jwtManager,
		apiKeyManager:   apiKeyManager,
		passwordManager: passwordManager,
	}
}
