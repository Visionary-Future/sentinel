package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Database    DatabaseConfig    `mapstructure:"database"`
	Redis       RedisConfig       `mapstructure:"redis"`
	Alert       AlertConfig       `mapstructure:"alert_sources"`
	DataSources DataSourcesConfig `mapstructure:"data_sources"`
	Embedding   EmbeddingConfig   `mapstructure:"embedding"`
	LLM         LLMConfig         `mapstructure:"llm"`
	Notify      NotifyConfig      `mapstructure:"notification"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

type DatabaseConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Name         string `mapstructure:"name"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	SSLMode      string `mapstructure:"sslmode"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		d.Host, d.Port, d.Name, d.User, d.Password, d.SSLMode,
	)
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type AlertConfig struct {
	Slack   SlackSourceConfig   `mapstructure:"slack"`
	Webhook WebhookSourceConfig `mapstructure:"webhook"`
	Outlook OutlookSourceConfig `mapstructure:"outlook"`
}

// OutlookSourceConfig is kept for backwards compatibility but the email source
// now uses IMAP and is configured via EmailSourceConfig below.
type OutlookSourceConfig = EmailSourceConfig

type EmailSourceConfig struct {
	Enabled      bool              `mapstructure:"enabled"`
	IMAPHost     string            `mapstructure:"imap_host"`     // e.g. imap.163.com
	IMAPPort     int               `mapstructure:"imap_port"`     // default: 993
	Username     string            `mapstructure:"username"`
	Password     string            `mapstructure:"password"`      // for QQ/163 use auth code, not login password
	Folder       string            `mapstructure:"folder"`        // default: INBOX
	TLS          bool              `mapstructure:"tls"`           // default: true
	PollInterval string            `mapstructure:"poll_interval"` // default: 30s
	Filters      EmailFilterConfig `mapstructure:"filters"`
}

type EmailFilterConfig struct {
	Subjects []string `mapstructure:"subjects"` // substring match on Subject
	Keywords []string `mapstructure:"keywords"` // substring match on Subject or Body preview
	Senders  []string `mapstructure:"senders"`  // allowed sender addresses or domains
}

type SlackSourceConfig struct {
	Enabled       bool            `mapstructure:"enabled"`
	BotToken      string          `mapstructure:"bot_token"`
	AppToken      string          `mapstructure:"app_token"`
	SigningSecret string          `mapstructure:"signing_secret"`
	Channels      []ChannelConfig `mapstructure:"channels"`
}

type ChannelConfig struct {
	ID      string              `mapstructure:"id"`
	Name    string              `mapstructure:"name"`
	Filters ChannelFilterConfig `mapstructure:"filters"`
}

type ChannelFilterConfig struct {
	BotUsers []string `mapstructure:"bot_users"`
	Keywords []string `mapstructure:"keywords"`
}

type WebhookSourceConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
	Secret  string `mapstructure:"secret"`
}

type EmbeddingConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Provider string `mapstructure:"provider"`  // "openai" | "tongyi"
	APIKey   string `mapstructure:"api_key"`
	Model    string `mapstructure:"model"`
	BaseURL  string `mapstructure:"base_url"`
	Dims     int    `mapstructure:"dims"`
}

type DataSourcesConfig struct {
	AliyunSLS AliyunSLSConfig `mapstructure:"aliyun_sls"`
	AliyunCMS AliyunCMSConfig `mapstructure:"aliyun_cms"`
}

type AliyunCMSConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	Region          string `mapstructure:"region"`
}

type AliyunSLSConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	Endpoint         string `mapstructure:"endpoint"`
	AccessKeyID      string `mapstructure:"access_key_id"`
	AccessKeySecret  string `mapstructure:"access_key_secret"`
	Project          string `mapstructure:"project"`
	DefaultLogstore  string `mapstructure:"default_logstore"`
}

type LLMConfig struct {
	DefaultProvider string                 `mapstructure:"default_provider"`
	Providers       map[string]LLMProvider `mapstructure:"providers"`
}

type LLMProvider struct {
	APIKey string `mapstructure:"api_key"`
	Model  string `mapstructure:"model"`
}

type NotifyConfig struct {
	Slack    SlackNotifyConfig    `mapstructure:"slack"`
	WeCom    WeComNotifyConfig    `mapstructure:"wecom"`
	DingTalk DingTalkNotifyConfig `mapstructure:"dingtalk"`
}

type SlackNotifyConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	BotToken       string `mapstructure:"bot_token"`
	DefaultChannel string `mapstructure:"default_channel"`
	ReplyInThread  bool   `mapstructure:"reply_in_thread"`
}

type WeComNotifyConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`
}

type DingTalkNotifyConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`
	Secret     string `mapstructure:"secret"`
}

// Load reads configuration from file and environment variables.
// Environment variables override file values (e.g. SENTINEL_DATABASE_PASSWORD).
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("SENTINEL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
