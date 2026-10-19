package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppMode     string // "api" (default), "worker", "all"
	Port        string // default "8080"
	ServiceName string // default "orders"
	PodName     string // hostname / pod id

	// DB
	DBProvider  string // "in-memory" (default), "postgres"
	DatabaseURL string // full postgres URL if provided
	DBHost      string
	DBPort      string
	DBName      string
	DBUser      string
	DBPassword  string
	DBSSLMode   string

	// Worker & SQS
	SQSQueueURL       string
	QueueName         string
	SQSMaxMessages    int    // default 1, max 10
	WorkerConcurrency int    // default 1
	WorkerDBMode      string // "light" (default), "none", "full"
	AWSRegion         string
	AWSEndpointURL    string
}

func LoadConfig() *Config {
	cfg := &Config{
		AppMode:        getEnv("APP_MODE", "api"),
		Port:           getEnv("PORT", "8080"),
		ServiceName:    "orders",
		DBProvider:     getEnv("RETAIL_ORDERS_PERSISTENCE_PROVIDER", ""),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		DBHost:         getEnv("DB_HOST", os.Getenv("RETAIL_ORDERS_PERSISTENCE_ENDPOINT")),
		DBPort:         getEnv("DB_PORT", "5432"),
		DBName:         getEnv("DB_NAME", getEnv("RETAIL_ORDERS_PERSISTENCE_NAME", "retaildb")),
		DBUser:         getEnv("DB_USER", getEnv("RETAIL_ORDERS_PERSISTENCE_USERNAME", "postgres")),
		DBPassword:     getEnv("DB_PASSWORD", getEnv("RETAIL_ORDERS_PERSISTENCE_PASSWORD", "postgres")),
		DBSSLMode:      getEnv("DB_SSLMODE", "disable"),
		SQSQueueURL:    os.Getenv("SQS_QUEUE_URL"),
		WorkerDBMode:   getEnv("WORKER_DB_MODE", "light"),
		AWSRegion:      getEnv("AWS_REGION", getEnv("AWS_DEFAULT_REGION", "us-east-1")),
		AWSEndpointURL: getEnv("AWS_ENDPOINT_URL", os.Getenv("AWS_ENDPOINT_URL_SQS")),
	}

	// Hostname / Pod name
	podName := getEnv("POD_NAME", os.Getenv("HOSTNAME"))
	if podName == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			podName = h
		} else {
			podName = "unknown-pod"
		}
	}
	cfg.PodName = podName

	// Resolve DB Provider
	if cfg.DBProvider == "" {
		if cfg.DatabaseURL != "" || cfg.DBHost != "" {
			cfg.DBProvider = "postgres"
		} else {
			cfg.DBProvider = "in-memory"
		}
	}

	// Worker SQS settings
	maxMsgs, err := strconv.Atoi(os.Getenv("SQS_MAX_MESSAGES"))
	if err != nil || maxMsgs < 1 {
		maxMsgs = 1
	} else if maxMsgs > 10 {
		maxMsgs = 10
	}
	cfg.SQSMaxMessages = maxMsgs

	concurrency, err := strconv.Atoi(os.Getenv("WORKER_CONCURRENCY"))
	if err != nil || concurrency < 1 {
		concurrency = 1
	}
	cfg.WorkerConcurrency = concurrency

	// Derive QueueName
	cfg.QueueName = "orders-queue"
	if cfg.SQSQueueURL != "" {
		if parsed, err := url.Parse(cfg.SQSQueueURL); err == nil {
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) > 0 && parts[len(parts)-1] != "" {
				cfg.QueueName = parts[len(parts)-1]
			}
		}
	}

	return cfg
}

func (c *Config) PostgresConnString() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}

	host := c.DBHost
	port := c.DBPort
	if strings.Contains(host, ":") {
		parts := strings.Split(host, ":")
		host = parts[0]
		port = parts[1]
	}

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
