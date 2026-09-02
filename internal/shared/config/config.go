package config

import (
	"net/netip"
	"os"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
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
	HTTPAddr string `mapstructure:"http_addr"`
	GRPCAddr string `mapstructure:"grpc_addr"`
	TLSCert  string `mapstructure:"tls_cert"`
	TLSKey   string `mapstructure:"tls_key"`
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
	CertFile string        `mapstructure:"cert_file"`
	KeyFile  string        `mapstructure:"key_file"`
	CertTTL  time.Duration `mapstructure:"cert_ttl"`
}

type AdapterConfig struct {
	WireGuard WireGuardConfig `mapstructure:"wireguard"`
	Xray      XrayConfig      `mapstructure:"xray"`
}

type WireGuardConfig struct {
	InterfacePrefix string   `mapstructure:"interface_prefix"`
	SubnetV4        netip.Prefix `mapstructure:"subnet_v4"`
	SubnetV6        netip.Prefix `mapstructure:"subnet_v6"`
	DNS             []string `mapstructure:"dns"`
	MTU             int      `mapstructure:"mtu"`
	Keepalive       int      `mapstructure:"keepalive"`
}

type XrayConfig struct {
	LogLevel string `mapstructure:"log_level"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type AgentConfig struct {
	NodeName         string        `mapstructure:"node_name"`
	ControlPlane     string        `mapstructure:"control_plane"`
	CACert           string        `mapstructure:"ca_cert"`
	CertFile         string        `mapstructure:"cert_file"`
	KeyFile          string        `mapstructure:"key_file"`
	SyncInterval     time.Duration `mapstructure:"sync_interval"`
	MetricsInterval  time.Duration `mapstructure:"metrics_interval"`
	WireGuard        WireGuardConfig `mapstructure:"wireguard"`
	Xray             XrayConfig      `mapstructure:"xray"`
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/vpnbuilder")
	v.AutomaticEnv()
	v.SetEnvPrefix("VPNBUILDER")

	setDefaults(v)

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
		v.AddConfigPath(configPath)
	}
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/vpnbuilder")
	v.AutomaticEnv()
	v.SetEnvPrefix("VPNBUILDER_AGENT")

	setDefaults(v)

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
	v.SetDefault("server.http_addr", ":8080")
	v.SetDefault("server.grpc_addr", ":9090")
	v.SetDefault("server.tls_cert", "")
	v.SetDefault("server.tls_key", "")

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

	v.SetDefault("adapter.xray.log_level", "warning")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
}