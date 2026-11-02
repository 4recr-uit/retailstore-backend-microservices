package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws-containers/retail-store-sample-app/cart/pkg/chaos"
	appconfig "github.com/aws-containers/retail-store-sample-app/cart/pkg/config"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/metrics"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/store"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/worker"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// AppConfig contains runtime settings for API and Worker.
type AppConfig struct {
	AppMode           string // "api" | "worker" | "all"
	Port              string
	Service           string
	QueueName         string
	QueueURL          string
	MaxMessages       int32
	WorkerConcurrency int
	DBMode            string
	AWSRegion         string
	AWSEndpointURL    string
}

func loadConfig() AppConfig {
	mode := os.Getenv("APP_MODE")
	if mode == "" {
		mode = "api"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	svc := os.Getenv("SERVICE_NAME")
	if svc == "" {
		svc = "cart"
	}
	qName := os.Getenv("SQS_QUEUE_NAME")
	if qName == "" {
		qName = "cart-queue"
	}
	qURL := os.Getenv("SQS_QUEUE_URL")
	maxMsgs := int32(1)
	if v := os.Getenv("SQS_MAX_MESSAGES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 10 {
			maxMsgs = int32(n)
		}
	}
	concurrency := 1
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			concurrency = n
		}
	}
	dbMode := os.Getenv("WORKER_DB_MODE")
	if dbMode == "" {
		dbMode = "light"
	}
	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = os.Getenv("AWS_DEFAULT_REGION")
	}
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if endpointURL == "" {
		endpointURL = os.Getenv("LOCALSTACK_ENDPOINT")
	}

	return AppConfig{
		AppMode:           mode,
		Port:              port,
		Service:           svc,
		QueueName:         qName,
		QueueURL:          qURL,
		MaxMessages:       maxMsgs,
		WorkerConcurrency: concurrency,
		DBMode:            dbMode,
		AWSRegion:         awsRegion,
		AWSEndpointURL:    endpointURL,
	}
}

// InitStore creates and returns the configured persistence provider.
func InitStore() (store.Store, error) {
	provider := os.Getenv("RETAIL_CART_PERSISTENCE_PROVIDER")
	dbHost := os.Getenv("DB_HOST")
	dbURL := os.Getenv("DATABASE_URL")

	if provider == "postgres" || dbHost != "" || dbURL != "" {
		var connStr string
		if dbURL != "" {
			connStr = dbURL
		} else {
			dbUser, dbPass, dbName := os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME")
			if dbHost == "" || dbUser == "" || dbPass == "" || dbName == "" {
				return nil, fmt.Errorf("PostgreSQL requires DB_HOST, DB_NAME, DB_USER and DB_PASSWORD or DATABASE_URL")
			}
			dbPort := os.Getenv("DB_PORT")
			if dbPort == "" {
				dbPort = "5432"
			}
			dbSSL := os.Getenv("DB_SSLMODE")
			if dbSSL == "" {
				dbSSL = "disable"
			}
			connection := url.URL{Scheme: "postgres", Host: net.JoinHostPort(dbHost, dbPort), Path: "/" + dbName, User: url.UserPassword(dbUser, dbPass)}
			options := url.Values{"sslmode": {dbSSL}, "connect_timeout": {strconv.Itoa(int(appconfig.EnvSeconds("DB_CONNECT_TIMEOUT_SECONDS", 10) / time.Second))}}
			if ca := os.Getenv("DB_SSLROOTCERT"); ca != "" {
				options.Set("sslrootcert", ca)
			}
			connection.RawQuery = options.Encode()
			connStr = connection.String()
		}

		db, err := sql.Open("postgres", connStr)
		if err != nil {
			return nil, fmt.Errorf("failed to open postgres database: %w", err)
		}
		return store.NewPostgresStore(db)
	}

	return store.NewMemoryStore(), nil
}

// InitSQSClient configures and initializes an AWS SDK SQS client.
func InitSQSClient(ctx context.Context, region, endpointURL string) (*sqs.Client, error) {
	optFns := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	// Use the standard SDK chain: env credentials locally or EKS Pod Identity.

	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, err
	}

	sqsOpts := func(o *sqs.Options) {
		if endpointURL != "" {
			o.BaseEndpoint = aws.String(endpointURL)
		}
	}

	return sqs.NewFromConfig(cfg, sqsOpts), nil
}

// SetupRouter sets up Gin HTTP handlers for API, Worker health/metrics, and Chaos.
func SetupRouter(st store.Store, sqsClient worker.SQSAPI, cfg AppConfig, m *metrics.Metrics, cc *chaos.Controller) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cc.Middleware())

	// Health check handler
	healthHandler := func(c *gin.Context) {
		if !cc.IsHealthy() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "DOWN", "error": "chaos health check failed"})
			return
		}
		if st != nil {
			if err := st.Ping(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "DOWN", "error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	}

	// Health and Actuator endpoints
	r.GET("/health", healthHandler)
	r.GET("/actuator/health", healthHandler)
	r.GET("/actuator/health/liveness", healthHandler)
	r.GET("/actuator/health/readiness", healthHandler)

	// Prometheus metrics endpoint
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Chaos management endpoints
	cc.RegisterRoutes(r)

	// API routes (only in api or all mode)
	if cfg.AppMode == "api" || cfg.AppMode == "all" {
		registerCartRoutes(r, st)
		registerJobProducerRoutes(r, sqsClient, cfg, m)
	}

	return r
}

func registerCartRoutes(r *gin.Engine, st store.Store) {
	// OpenAPI specification: /carts/{customerId}
	carts := r.Group("/carts")
	{
		// GET /carts/{customerId} -> 200 OK
		carts.GET("/:customerId", func(c *gin.Context) {
			custID := c.Param("customerId")
			cart, err := st.GetCart(c.Request.Context(), custID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, cart)
		})

		// DELETE /carts/{customerId} -> 202 Accepted Cart
		carts.DELETE("/:customerId", func(c *gin.Context) {
			custID := c.Param("customerId")
			cart, err := st.DeleteCart(c.Request.Context(), custID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusAccepted, cart)
		})

		// GET /carts/{customerId}/merge?sessionId=... -> 202 Accepted
		carts.GET("/:customerId/merge", func(c *gin.Context) {
			custID := c.Param("customerId")
			sessionID := c.Query("sessionId")
			if sessionID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId query parameter required"})
				return
			}
			if err := st.MergeCarts(c.Request.Context(), custID, sessionID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.Status(http.StatusAccepted)
		})

		// GET /carts/{customerId}/items -> 200 OK []Item
		carts.GET("/:customerId/items", func(c *gin.Context) {
			custID := c.Param("customerId")
			items, err := st.GetItems(c.Request.Context(), custID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, items)
		})

		// POST /carts/{customerId}/items -> 201 Created Item
		carts.POST("/:customerId/items", func(c *gin.Context) {
			custID := c.Param("customerId")
			var item model.Item
			if err := c.ShouldBindJSON(&item); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			res, err := st.AddItem(c.Request.Context(), custID, item)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusCreated, res)
		})

		// PATCH /carts/{customerId}/items -> 202 Accepted
		carts.PATCH("/:customerId/items", func(c *gin.Context) {
			custID := c.Param("customerId")
			var item model.Item
			if err := c.ShouldBindJSON(&item); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if err := st.UpdateItem(c.Request.Context(), custID, item); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.Status(http.StatusAccepted)
		})

		// GET /carts/{customerId}/items/{itemId} -> 200 OK Item (or 404)
		carts.GET("/:customerId/items/:itemId", func(c *gin.Context) {
			custID := c.Param("customerId")
			itemID := c.Param("itemId")
			item, err := st.GetItem(c.Request.Context(), custID, itemID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, item)
		})

		// DELETE /carts/{customerId}/items/{itemId} -> 202 Accepted
		carts.DELETE("/:customerId/items/:itemId", func(c *gin.Context) {
			custID := c.Param("customerId")
			itemID := c.Param("itemId")
			if err := st.DeleteItem(c.Request.Context(), custID, itemID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.Status(http.StatusAccepted)
		})
	}
}

func registerJobProducerRoutes(r *gin.Engine, sqsClient worker.SQSAPI, cfg AppConfig, m *metrics.Metrics) {
	// Producer benchmark endpoint: POST /jobs
	r.POST("/jobs", func(c *gin.Context) {
		var req model.JobRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			req = model.JobRequest{
				BatchSize: 1,
				WorkUnits: 100,
				DBMode:    "light",
			}
		}

		if req.BatchSize == 0 {
			req.BatchSize = 1
		}
		if req.WorkUnits == 0 {
			req.WorkUnits = 100
		}
		if req.DBMode == "" {
			req.DBMode = "light"
		}

		// Validation
		if req.WorkUnits < 1 || req.WorkUnits > 10000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "workUnits must be between 1 and 10000"})
			return
		}
		if req.BatchSize < 1 || req.BatchSize > 10 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "batchSize must be between 1 and 10"})
			return
		}
		if req.DBMode != "none" && req.DBMode != "light" && req.DBMode != "full" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dbMode, must be none, light, or full"})
			return
		}

		if sqsClient == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "sqs client not available"})
			return
		}

		jobIDs := make([]string, 0, req.BatchSize)
		now := time.Now().UnixMilli()

		if req.BatchSize == 1 {
			jobID := uuid.New().String()
			jobIDs = append(jobIDs, jobID)
			msg := model.JobMessage{
				JobID:      jobID,
				EnqueuedAt: now,
				WorkUnits:  req.WorkUnits,
				DBMode:     req.DBMode,
				CustomerID: req.CustomerID,
				Payload:    req.Payload,
			}
			bodyBytes, _ := json.Marshal(msg)

			_, err := sqsClient.SendMessage(c.Request.Context(), &sqs.SendMessageInput{
				QueueUrl:    aws.String(cfg.QueueURL),
				MessageBody: aws.String(string(bodyBytes)),
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to send message: %v", err)})
				return
			}
		} else {
			entries := make([]sqstypes.SendMessageBatchRequestEntry, req.BatchSize)
			for i := 0; i < req.BatchSize; i++ {
				jobID := uuid.New().String()
				jobIDs = append(jobIDs, jobID)
				msg := model.JobMessage{
					JobID:      jobID,
					EnqueuedAt: now,
					WorkUnits:  req.WorkUnits,
					DBMode:     req.DBMode,
					CustomerID: req.CustomerID,
					Payload:    req.Payload,
				}
				bodyBytes, _ := json.Marshal(msg)
				entries[i] = sqstypes.SendMessageBatchRequestEntry{
					Id:          aws.String(fmt.Sprintf("msg-%d", i)),
					MessageBody: aws.String(string(bodyBytes)),
				}
			}

			accepted, err := worker.PublishBatch(c.Request.Context(), sqsClient, cfg.QueueURL, entries)
			if err != nil {
				successfulJobs, failedJobs := make([]string, 0), make([]string, 0)
				for i, id := range jobIDs {
					if accepted[fmt.Sprintf("msg-%d", i)] {
						successfulJobs = append(successfulJobs, id)
					} else {
						failedJobs = append(failedJobs, id)
					}
				}
				m.SQSMessagesPublished.WithLabelValues(cfg.Service, cfg.QueueName).Add(float64(len(successfulJobs)))
				c.JSON(http.StatusBadGateway, gin.H{"status": "partial_failure", "count": len(successfulJobs), "jobIds": successfulJobs, "failedJobIds": failedJobs})
				return
			}

		}

		m.SQSMessagesPublished.WithLabelValues(cfg.Service, cfg.QueueName).Add(float64(req.BatchSize))

		c.JSON(http.StatusCreated, model.JobResponse{
			Status: "published",
			Queue:  cfg.QueueName,
			Count:  req.BatchSize,
			JobIDs: jobIDs,
		})
	})
}

func main() {
	cfg := loadConfig()
	log.Printf("Starting cart service in mode: %s on port %s", cfg.AppMode, cfg.Port)

	m := metrics.GetMetrics()
	cc := chaos.NewController()

	st, err := InitStore()
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sqsClient, err := InitSQSClient(ctx, cfg.AWSRegion, cfg.AWSEndpointURL)
	if err != nil {
		log.Printf("Warning: failed to initialize SQS client: %v", err)
	}

	var wkr *worker.Worker
	if cfg.AppMode == "worker" || cfg.AppMode == "all" {
		workerCfg := worker.Config{
			QueueURL:    cfg.QueueURL,
			QueueName:   cfg.QueueName,
			Service:     cfg.Service,
			MaxMessages: cfg.MaxMessages,
			Concurrency: cfg.WorkerConcurrency,
			DBMode:      cfg.DBMode,
		}
		wkr = worker.NewWorker(sqsClient, st, workerCfg, m)
		wkr.Start(ctx)
	}

	router := SetupRouter(st, sqsClient, cfg, m, cc)
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Listen for termination signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("Received signal %s, initiating graceful shutdown...", sig)

	// 1. Stop worker if running
	if wkr != nil {
		wkr.Stop()
	}

	// 2. Shutdown HTTP server with 5s timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), appconfig.EnvSeconds("HTTP_SHUTDOWN_TIMEOUT_SECONDS", 5))
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Printf("Cart service shutdown completed cleanly.")
}
