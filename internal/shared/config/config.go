package config

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Env      string         `mapstructure:"env"`
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Auth     AuthConfig     `mapstructure:"auth"`
	CA       CAConfig       `mapstructure:"ca"`
	Adapter  AdapterConfig  `mapstructure:"adapter"`
	Log      LogConfig      `mapstructure:"log"`
	Agent    AgentConfig    `mapstructure:"agent"`
}

type ServerConfig struct {
	HTTPAddr           string   `mapstructure:"http_addr"`
	GRPCAddr           string   `mapstructure:"grpc_addr"`
	TLSCert            string   `mapstructure:"tls_cert"`
	TLSKey             string   `mapstructure:"tls_key"`
	CORSAllowedOrigins []string `mapstructure:"cors_allowed_origins"`
}

type DatabaseConfig struct {
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type AuthConfig struct {
	JWTSecret     string        `mapstructure:"jwt_secret"`
	JWTAccessTTL  time.Duration `mapstructure:"jwt_access_ttl"`
	JWTRefreshTTL time.Duration `mapstructure:"jwt_refresh_ttl"`
	BcryptCost    int           `mapstructure:"bcrypt_cost"`
}

type CAConfig struct {
	CertTTL  time.Duration `mapstructure:"cert_ttl"`
	CertFile string        `mapstructure:"cert_file"`
	KeyFile  string        `mapstructure:"key_file"`
}

type AdapterConfig struct {
	WireGuard WireGuardConfig `mapstructure:"wireguard"`
	Xray      XrayConfig      `mapstructure:"xray"`
}

type WireGuardConfig struct {
	InterfacePrefix string   `mapstructure:"interface_prefix"`
	SubnetV4        string   `mapstructure:"subnet_v4"`
	SubnetV6        string   `mapstructure:"subnet_v6"`
	DNS             []string `mapstructure:"dns"`
	MTU             int      `mapstructure:"mtu"`
	Keepalive       int      `mapstructure:"keepalive"`
}

type XrayConfig struct {
	BinaryPath string `mapstructure:"binary_path"`
	ConfigDir  string `mapstructure:"config_dir"`
	APIPort    int    `mapstructure:"api_port"`
	LogLevel   string `mapstructure:"log_level"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type AgentConfig struct {
	NodeName        string          `mapstructure:"node_name"`
	Region          string          `mapstructure:"region"`
	Country         string          `mapstructure:"country"`
	City            string          `mapstructure:"city"`
	PublicIP        string          `mapstructure:"public_ip"`
	Tags            []string        `mapstructure:"tags"`
	ControlPlane    string          `mapstructure:"control_plane"`
	CACert          string          `mapstructure:"ca_cert"`
	CertFile        string          `mapstructure:"cert_file"`
	KeyFile         string          `mapstructure:"key_file"`
	SyncInterval    time.Duration   `mapstructure:"sync_interval"`
	MetricsInterval time.Duration   `mapstructure:"metrics_interval"`
	WireGuard       WireGuardConfig `mapstructure:"wireguard"`
	Xray            XrayConfig      `mapstructure:"xray"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	if configPath != "" {
		stat, err := os.Stat(configPath)
		if err == nil && !stat.IsDir() {
			v.SetConfigFile(configPath)
		} else {
			v.AddConfigPath(configPath)
		}
	}
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/vpnbuilder")
	v.AutomaticEnv()
	v.SetEnvPrefix("VPNBUILDER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults(v)
	bindControlPlaneEnvs(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func LoadAgent(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	if configPath != "" {
		stat, err := os.Stat(configPath)
		if err == nil && !stat.IsDir() {
			v.SetConfigFile(configPath)
		} else {
			v.AddConfigPath(configPath)
		}
	}
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/vpnbuilder")
	v.AutomaticEnv()
	v.SetEnvPrefix("VPNBUILDER_AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults(v)
	bindAgentEnvs(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if cfg.Agent.NodeName == "" {
		hostname, _ := os.Hostname()
		cfg.Agent.NodeName = hostname
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("env", "dev")
	v.SetDefault("server.http_addr", ":8110")
	v.SetDefault("server.grpc_addr", ":9090")
	v.SetDefault("server.tls_cert", "")
	v.SetDefault("server.tls_key", "")
	v.SetDefault("server.cors_allowed_origins", []string{"http://localhost:3000", "http://localhost:8080"})

	v.SetDefault("database.dsn", "postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")
	v.SetDefault("database.conn_max_idle_time", "1m")

	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	v.SetDefault("auth.jwt_access_ttl", "15m")
	v.SetDefault("auth.jwt_refresh_ttl", "168h")
	v.SetDefault("auth.bcrypt_cost", 12)

	v.SetDefault("ca.cert_ttl", "720h")

	v.SetDefault("adapter.wireguard.interface_prefix", "wg")
	v.SetDefault("adapter.wireguard.subnet_v4", "10.8.0.0/16")
	v.SetDefault("adapter.wireguard.subnet_v6", "fd00::/64")
	v.SetDefault("adapter.wireguard.dns", []string{"1.1.1.1", "1.0.0.1"})
	v.SetDefault("adapter.wireguard.mtu", 1280)
	v.SetDefault("adapter.wireguard.keepalive", 25)

	v.SetDefault("agent.wireguard.interface_prefix", "wg")
	v.SetDefault("agent.wireguard.subnet_v4", "10.8.0.0/16")
	v.SetDefault("agent.wireguard.subnet_v6", "fd00::/64")
	v.SetDefault("agent.wireguard.dns", []string{"1.1.1.1", "1.0.0.1"})
	v.SetDefault("agent.wireguard.mtu", 1280)
	v.SetDefault("agent.wireguard.keepalive", 25)
	v.SetDefault("agent.sync_interval", "30s")
	v.SetDefault("agent.metrics_interval", "30s")

	v.SetDefault("adapter.xray.log_level", "warning")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
}

func bindControlPlaneEnvs(v *viper.Viper) {
	_ = v.BindEnv("env", "VPNBUILDER_ENV")
	_ = v.BindEnv("server.http_addr", "VPNBUILDER_SERVER_HTTP_ADDR")
	_ = v.BindEnv("server.grpc_addr", "VPNBUILDER_SERVER_GRPC_ADDR")
	_ = v.BindEnv("server.tls_cert", "VPNBUILDER_SERVER_TLS_CERT")
	_ = v.BindEnv("server.tls_key", "VPNBUILDER_SERVER_TLS_KEY")
	_ = v.BindEnv("server.cors_allowed_origins", "VPNBUILDER_SERVER_CORS_ALLOWED_ORIGINS")
	_ = v.BindEnv("database.dsn", "VPNBUILDER_DATABASE_DSN")
	_ = v.BindEnv("database.max_open_conns", "VPNBUILDER_DATABASE_MAX_OPEN_CONNS")
	_ = v.BindEnv("database.max_idle_conns", "VPNBUILDER_DATABASE_MAX_IDLE_CONNS")
	_ = v.BindEnv("redis.addr", "VPNBUILDER_REDIS_ADDR")
	_ = v.BindEnv("redis.password", "VPNBUILDER_REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "VPNBUILDER_REDIS_DB")
	_ = v.BindEnv("auth.jwt_secret", "VPNBUILDER_AUTH_JWT_SECRET")
	_ = v.BindEnv("auth.jwt_access_ttl", "VPNBUILDER_AUTH_JWT_ACCESS_TTL")
	_ = v.BindEnv("auth.jwt_refresh_ttl", "VPNBUILDER_AUTH_JWT_REFRESH_TTL")
	_ = v.BindEnv("auth.bcrypt_cost", "VPNBUILDER_AUTH_BCRYPT_COST")
	_ = v.BindEnv("ca.cert_ttl", "VPNBUILDER_CA_CERT_TTL")
	_ = v.BindEnv("ca.cert_file", "VPNBUILDER_CA_CERT_FILE")
	_ = v.BindEnv("ca.key_file", "VPNBUILDER_CA_KEY_FILE")
	_ = v.BindEnv("log.level", "VPNBUILDER_LOG_LEVEL")
	_ = v.BindEnv("log.format", "VPNBUILDER_LOG_FORMAT")
}

func bindAgentEnvs(v *viper.Viper) {
	_ = v.BindEnv("agent.node_name", "VPNBUILDER_AGENT_NODE_NAME")
	_ = v.BindEnv("agent.control_plane", "VPNBUILDER_AGENT_CONTROL_PLANE")
	_ = v.BindEnv("agent.ca_cert", "VPNBUILDER_AGENT_CA_CERT")
	_ = v.BindEnv("agent.cert_file", "VPNBUILDER_AGENT_CERT_FILE")
	_ = v.BindEnv("agent.key_file", "VPNBUILDER_AGENT_KEY_FILE")
	_ = v.BindEnv("agent.sync_interval", "VPNBUILDER_AGENT_SYNC_INTERVAL")
	_ = v.BindEnv("agent.metrics_interval", "VPNBUILDER_AGENT_METRICS_INTERVAL")
	_ = v.BindEnv("agent.wireguard.interface_prefix", "VPNBUILDER_AGENT_WIREGUARD_INTERFACE_PREFIX")
	_ = v.BindEnv("agent.wireguard.subnet_v4", "VPNBUILDER_AGENT_WIREGUARD_SUBNET_V4")
	_ = v.BindEnv("agent.wireguard.subnet_v6", "VPNBUILDER_AGENT_WIREGUARD_SUBNET_V6")
	_ = v.BindEnv("agent.wireguard.dns", "VPNBUILDER_AGENT_WIREGUARD_DNS")
	_ = v.BindEnv("agent.wireguard.mtu", "VPNBUILDER_AGENT_WIREGUARD_MTU")
	_ = v.BindEnv("log.level", "VPNBUILDER_LOG_LEVEL")
	_ = v.BindEnv("log.format", "VPNBUILDER_LOG_FORMAT")
}
