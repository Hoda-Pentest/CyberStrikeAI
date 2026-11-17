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

// ApplyDevHTTPSBootstrap is used by --https / the one-click script: forces main-site TLS on.
// If tls_cert_path and tls_key_path are configured, use the PEM; otherwise enable tls_auto_self_sign (local testing only).
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
