package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/db"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/metrics"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/middleware"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/worker"
)

type JobRequest struct {
	WorkUnits  int             `json:"workUnits"`
	DBMode     string          `json:"dbMode"`
	BatchSize  int             `json:"batchSize"`
	CustomerID string          `json:"customerId"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type Server struct {
	cfg             *config.Config
	repo            db.Repository
	sqsClient       worker.SQSClient
	chaosController *middleware.ChaosController
	engine          *gin.Engine
}

func NewServer(cfg *config.Config, repo db.Repository, sqsClient worker.SQSClient) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	chaosController := middleware.NewChaosController()

	s := &Server{
		cfg:             cfg,
		repo:            repo,
		sqsClient:       sqsClient,
		chaosController: chaosController,
		engine:          engine,
	}

	s.setupRoutes()
	return s
}

func (s *Server) Engine() *gin.Engine {
	return s.engine
}

func (s *Server) setupRoutes() {
	// Chaos middleware
	s.engine.Use(s.chaosController.ChaosMiddleware())

	// Chaos control routes
	s.chaosController.SetupChaosRoutes(s.engine)

	// Health and Actuator routes
	s.engine.GET("/health", s.handleHealth)
	s.engine.GET("/actuator/health", s.handleActuatorHealth)
	s.engine.GET("/actuator/health/liveness", s.handleActuatorHealth)
	s.engine.GET("/actuator/health/readiness", s.handleActuatorHealth)

	// Metrics endpoint
	s.engine.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Business routes (enabled in "api" and "all" modes)
	if s.cfg.AppMode == "api" || s.cfg.AppMode == "all" || s.cfg.AppMode == "" {
		s.engine.GET("/orders", s.handleListOrders)
		s.engine.POST("/orders", s.handleCreateOrder)
		s.engine.POST("/jobs", s.handleCreateJob)
	}
}

func (s *Server) handleHealth(c *gin.Context) {
	if !s.chaosController.IsHealthy() {
		c.String(http.StatusServiceUnavailable, "FAIL")
		return
	}
	c.String(http.StatusOK, "OK")
}

func (s *Server) handleActuatorHealth(c *gin.Context) {
	if !s.chaosController.IsHealthy() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "DOWN"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}

func (s *Server) handleListOrders(c *gin.Context) {
	orders, err := s.repo.ListOrders(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if orders == nil {
		orders = []db.ExistingOrder{}
	}
	c.JSON(http.StatusOK, orders)
}

func (s *Server) handleCreateOrder(c *gin.Context) {
	var order db.Order
	if err := c.ShouldBindJSON(&order); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order payload"})
		return
	}

	created, err := s.repo.CreateOrder(c.Request.Context(), order)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, created)
}

func (s *Server) handleCreateJob(c *gin.Context) {
	var req JobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate workUnits: 1 <= workUnits <= 10000
	if req.WorkUnits < 1 || req.WorkUnits > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "workUnits must be between 1 and 10000",
		})
		return
	}

	// Default and validate batchSize: 1 <= batchSize <= 10
	if req.BatchSize <= 0 {
		req.BatchSize = 1
	}
	if req.BatchSize < 1 || req.BatchSize > 10 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "batchSize must be between 1 and 10",
		})
		return
	}

	if req.CustomerID == "" {
		req.CustomerID = "cust-1"
	}
	if req.DBMode == "" {
		req.DBMode = "light"
	}

	if s.sqsClient == nil || s.cfg.SQSQueueURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "SQS queue is not configured",
		})
		return
	}

	jobIDs := make([]string, 0, req.BatchSize)
	enqueuedAt := time.Now().UnixMilli()

	if req.BatchSize == 1 {
		jobID := uuid.New().String()
		jobIDs = append(jobIDs, jobID)

		msg := worker.JobMessage{
			JobID:      jobID,
			EnqueuedAt: enqueuedAt,
			WorkUnits:  req.WorkUnits,
			DBMode:     req.DBMode,
			CustomerID: req.CustomerID,
			Payload:    req.Payload,
		}

		bodyBytes, err := json.Marshal(msg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal message"})
			return
		}

		_, err = s.sqsClient.SendMessage(c.Request.Context(), &sqs.SendMessageInput{
			QueueUrl:    aws.String(s.cfg.SQSQueueURL),
			MessageBody: aws.String(string(bodyBytes)),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to send SQS message: %v", err)})
			return
		}
	} else {
		entries := make([]sqsTypes.SendMessageBatchRequestEntry, 0, req.BatchSize)

		for i := 0; i < req.BatchSize; i++ {
			jobID := uuid.New().String()
			jobIDs = append(jobIDs, jobID)

			msg := worker.JobMessage{
				JobID:      jobID,
				EnqueuedAt: enqueuedAt,
				WorkUnits:  req.WorkUnits,
				DBMode:     req.DBMode,
				CustomerID: req.CustomerID,
				Payload:    req.Payload,
			}

			bodyBytes, err := json.Marshal(msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal batch message"})
				return
			}

			entries = append(entries, sqsTypes.SendMessageBatchRequestEntry{
				Id:          aws.String(fmt.Sprintf("msg-%d", i)),
				MessageBody: aws.String(string(bodyBytes)),
			})
		}

		accepted, err := worker.PublishBatch(c.Request.Context(), s.sqsClient, s.cfg.SQSQueueURL, entries)
		if err != nil {
			successfulJobs, failedJobs := make([]string, 0), make([]string, 0)
			for i, id := range jobIDs {
				if accepted[fmt.Sprintf("msg-%d", i)] {
					successfulJobs = append(successfulJobs, id)
				} else {
					failedJobs = append(failedJobs, id)
				}
			}
			metrics.SqsMessagesPublishedTotal.WithLabelValues(s.cfg.ServiceName, s.cfg.QueueName).Add(float64(len(successfulJobs)))
			c.JSON(http.StatusBadGateway, gin.H{"status": "partial_failure", "count": len(successfulJobs), "jobIds": successfulJobs, "failedJobIds": failedJobs})
			return
		}

	}

	// Increment Prometheus counter
	metrics.SqsMessagesPublishedTotal.WithLabelValues(s.cfg.ServiceName, s.cfg.QueueName).Add(float64(req.BatchSize))

	c.JSON(http.StatusOK, gin.H{
		"status":     "published",
		"batchSize":  req.BatchSize,
		"enqueuedAt": enqueuedAt,
		"jobIds":     jobIDs,
	})
}
