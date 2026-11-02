// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of this
// software and associated documentation files (the "Software"), to deal in the Software
// without restriction, including without limitation the rights to use, copy, modify,
// merge, publish, distribute, sublicense, and/or sell copies of the Software, and to
// permit persons to whom the Software is furnished to do so.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED,
// INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A
// PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
// OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
// SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package config

// Configuration exported
type AppConfiguration struct {
	Port                   int `env:"PORT,default=8080"`
	ShutdownTimeoutSeconds int `env:"HTTP_SHUTDOWN_TIMEOUT_SECONDS,default=5"`
	Database               DatabaseConfiguration
	OpenSearch             OpenSearchConfiguration
}

// DatabaseConfiguration exported
type DatabaseConfiguration struct {
	MaxOpenConns           int    `env:"DB_MAX_OPEN_CONNS,default=10"`
	MaxIdleConns           int    `env:"DB_MAX_IDLE_CONNS,default=5"`
	ConnMaxLifetimeSeconds int    `env:"DB_CONN_MAX_LIFETIME_SECONDS,default=1800"`
	ConnectAttempts        int    `env:"DB_CONNECT_ATTEMPTS,default=6"`
	ConnectRetrySeconds    int    `env:"DB_CONNECT_RETRY_SECONDS,default=5"`
	TLS                    bool   `env:"RETAIL_CATALOG_PERSISTENCE_TLS,default=false"`
	Type                   string `env:"RETAIL_CATALOG_PERSISTENCE_PROVIDER,default=in-memory"`
	Endpoint               string `env:"RETAIL_CATALOG_PERSISTENCE_ENDPOINT"`
	Name                   string `env:"RETAIL_CATALOG_PERSISTENCE_DB_NAME,default=catalogdb"`
	User                   string `env:"RETAIL_CATALOG_PERSISTENCE_USER,default=catalog_user"`
	Password               string `env:"RETAIL_CATALOG_PERSISTENCE_PASSWORD"`
	ConnectTimeout         int    `env:"RETAIL_CATALOG_PERSISTENCE_CONNECT_TIMEOUT,default=5"`
}

// OpenSearchConfiguration exported
type OpenSearchConfiguration struct {
	Enabled       bool   `env:"RETAIL_CATALOG_SEARCH_ENABLED,default=false"`
	Type          string `env:"RETAIL_CATALOG_SEARCH_PROVIDER,default=self-hosted"`
	Endpoint      string `env:"RETAIL_CATALOG_SEARCH_OS_ENDPOINT"`
	IndexName     string `env:"RETAIL_CATALOG_SEARCH_OS_INDEX,default=products"`
	Username      string `env:"RETAIL_CATALOG_SEARCH_OS_USERNAME"`
	Password      string `env:"RETAIL_CATALOG_SEARCH_OS_PASSWORD"`
	TLSSkipVerify bool   `env:"RETAIL_CATALOG_SEARCH_OS_TLS_SKIP_VERIFY,default=false"`
}
