package config

import "strings"

// MainWebUIUsesHTTPS reports whether the main Web UI listens over HTTPS (consistent with the preconditions of internal/app.prepareMainServerTLS).
func MainWebUIUsesHTTPS(s *ServerConfig) bool {
	if s == nil {
		return false
	}
	if s.TLSEnabled {
		return true
	}
	if s.TLSAutoSelfSign {
		return true
	}
	cert := strings.TrimSpace(s.TLSCertPath)
	key := strings.TrimSpace(s.TLSKeyPath)
	return cert != "" && key != ""
}

// ServerHTTPRedirectEnabled: whether, when the main site enables HTTPS, plaintext HTTP requests are redirected to HTTPS (enabled by default).
func ServerHTTPRedirectEnabled(s *ServerConfig) bool {
	if s == nil || !MainWebUIUsesHTTPS(s) {
		return false
	}
	if s.TLSHTTPRedirect == nil {
		return true
	}
	return *s.TLSHTTPRedirect
}

// ApplyDevHTTPSBootstrap 供 --https / 一键脚本使用：强制开启主站 TLS。
// 若已配置 tls_cert_path 与 tls_key_path 则使用 PEM，否则启用 tls_auto_self_sign（仅本地测试）。
func ApplyDevHTTPSBootstrap(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Server.TLSEnabled = true
	cert := strings.TrimSpace(cfg.Server.TLSCertPath)
	key := strings.TrimSpace(cfg.Server.TLSKeyPath)
	if cert != "" && key != "" {
		cfg.Server.TLSAutoSelfSign = false
		return
	}
	cfg.Server.TLSAutoSelfSign = true
}

// ApplyPlainHTTPBootstrap is used by --http / the one-click script: it forces the main site to use plaintext HTTP.
// It overrides the TLS switch, self-signed certificate, and certificate paths in the config file, so that --http is not re-enabled into HTTPS by options in the configuration.
func ApplyPlainHTTPBootstrap(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Server.TLSEnabled = false
	cfg.Server.TLSAutoSelfSign = false
	cfg.Server.TLSCertPath = ""
	cfg.Server.TLSKeyPath = ""
	disabled := false
	cfg.Server.TLSHTTPRedirect = &disabled
}
