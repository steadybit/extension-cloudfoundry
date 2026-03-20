// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 Steadybit GmbH

package config

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog/log"
)

// Specification contains the configuration for the extension, mapped from environment variables.
type Specification struct {
	// The Cloud Foundry API endpoint URL (e.g., https://api.cf.example.com).
	// Set via STEADYBIT_EXTENSION_API_URL.
	ApiUrl string `json:"apiUrl" split_words:"true" required:"true"`
	// Username for Cloud Foundry authentication via UAA (password grant).
	// Set via STEADYBIT_EXTENSION_USERNAME.
	Username string `json:"username" split_words:"true"`
	// Password for Cloud Foundry authentication via UAA (password grant).
	// Set via STEADYBIT_EXTENSION_PASSWORD.
	Password string `json:"password" split_words:"true"`
	// Static bearer token for Kubernetes-based CF deployments (e.g., Korifi).
	// When set, UAA password grant is skipped and this token is used directly.
	// Set via STEADYBIT_EXTENSION_BEARER_TOKEN.
	BearerToken string `json:"bearerToken" split_words:"true"`
	// Path to a client certificate file for mTLS authentication (e.g., Korifi with kubeconfig certs).
	// Set via STEADYBIT_EXTENSION_CLIENT_CERT_PATH.
	ClientCertPath string `json:"clientCertPath" split_words:"true"`
	// Path to a client key file for mTLS authentication.
	// Set via STEADYBIT_EXTENSION_CLIENT_KEY_PATH.
	ClientKeyPath string `json:"clientKeyPath" split_words:"true"`
	// Skip TLS certificate verification (e.g., for self-signed certs in local Korifi).
	// Set via STEADYBIT_EXTENSION_SKIP_TLS_VERIFY.
	SkipTlsVerify bool `json:"skipTlsVerify" split_words:"true" default:"false"`
	// Attributes to exclude from app discovery results.
	// Set via STEADYBIT_EXTENSION_DISCOVERY_ATTRIBUTES_EXCLUDES_APP.
	DiscoveryAttributesExcludesApp []string `json:"discoveryAttributesExcludesApp" split_words:"true"`
}

var Config Specification

func ParseConfiguration() {
	err := envconfig.Process("steadybit_extension", &Config)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to parse configuration from environment.")
	}
}

func ValidateConfiguration() {
	if Config.ApiUrl == "" {
		log.Fatal().Msg("STEADYBIT_EXTENSION_API_URL must be set")
	}
	hasClientCert := Config.ClientCertPath != "" && Config.ClientKeyPath != ""
	hasBearerToken := Config.BearerToken != ""
	hasPassword := Config.Username != "" && Config.Password != ""
	if !hasClientCert && !hasBearerToken && !hasPassword {
		log.Fatal().Msg("One of the following auth methods must be configured: client certificate (CLIENT_CERT_PATH + CLIENT_KEY_PATH), BEARER_TOKEN, or USERNAME + PASSWORD")
	}
}
