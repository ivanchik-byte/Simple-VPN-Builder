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
	repos *store.Repositories,
	jwtManager *auth.JWTManager,
	apiKeyManager *auth.APIKeyManager,
	passwordManager *auth.PasswordManager,
	cfg *config.Config,
) *Services {
	return &Services{
		NodeService:       NewNodeService(repos.NodeRepo),
		UserService:       NewUserService(repos.UserRepo),
		PlanService:       NewPlanService(repos.PlanRepo),
		CredentialService: NewCredentialService(repos.CredentialRepo),
		AuthService:       NewAuthService(repos.AdminRepo, jwtManager, apiKeyManager, passwordManager),
	}
}

type NodeService struct {
	repo store.NodeRepository
}

func NewNodeService(repo store.NodeRepository) *NodeService {
	return &NodeService{repo: repo}
}

type UserService struct {
	repo store.UserRepository
}

func NewUserService(repo store.UserRepository) *UserService {
	return &UserService{repo: repo}
}

type PlanService struct {
	repo store.PlanRepository
}

func NewPlanService(repo store.PlanRepository) *PlanService {
	return &PlanService{repo: repo}
}

type CredentialService struct {
	repo store.CredentialRepository
}

func NewCredentialService(repo store.CredentialRepository) *CredentialService {
	return &CredentialService{repo: repo}
}

type AuthService struct {
	adminRepo      store.AdminRepository
	jwtManager     *auth.JWTManager
	apiKeyManager  *auth.APIKeyManager
	passwordManager *auth.PasswordManager
}

func NewAuthService(
	adminRepo store.AdminRepository,
	jwtManager *auth.JWTManager,
	apiKeyManager *auth.APIKeyManager,
	passwordManager *auth.PasswordManager,
) *AuthService {
	return &AuthService{
		adminRepo:       adminRepo,
		jwtManager:      jwtManager,
		apiKeyManager:   apiKeyManager,
		passwordManager: passwordManager,
	}
}