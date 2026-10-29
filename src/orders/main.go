package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/api"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/db"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/worker"
)

func main() {
	cfg := config.LoadConfig()
	log.Printf("Starting orders service: AppMode=%s, Port=%s, PodName=%s, DBProvider=%s",
		cfg.AppMode, cfg.Port, cfg.PodName, cfg.DBProvider)

	// 1. Initialize Repository (Postgres or In-Memory)
	repo, err := db.InitRepository(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize database repository: %v", err)
	}
	defer repo.Close()

	// 2. Initialize SQS Client
	sqsClient, err := initSQSClient(context.Background(), cfg)
	if err != nil {
		log.Printf("Warning: SQS client initialization error: %v", err)
	}

	// 3. Setup context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// 4. Initialize API Server
	apiServer := api.NewServer(cfg, repo, sqsClient)
	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: apiServer.Engine(),
	}

	// Start HTTP server
	go func() {
		log.Printf("HTTP server listening on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// 5. Worker execution if in worker or all mode
	var wrk *worker.Worker
	var workerDone chan struct{}
	if cfg.AppMode == "worker" || cfg.AppMode == "all" {
		wrk = worker.NewWorker(cfg, repo, sqsClient)
		workerDone = make(chan struct{})
		go func() {
			defer close(workerDone)
			wrk.Start(ctx)
		}()
	}

	// Wait for termination signal
	<-ctx.Done()
	log.Println("Termination signal received, beginning graceful shutdown...")

	// Stop worker if running
	if wrk != nil {
		wrk.Stop()
		if !waitForWorker(workerDone, config.EnvSeconds("WORKER_SHUTDOWN_TIMEOUT_SECONDS", 60)) {
			log.Print("Worker drain timed out; unacknowledged messages will be retried by SQS")
		}
	}

	// Shutdown HTTP Server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.EnvSeconds("HTTP_SHUTDOWN_TIMEOUT_SECONDS", 10))
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Orders service shut down successfully.")
}

func initSQSClient(ctx context.Context, cfg *config.Config) (*sqs.Client, error) {
	var optFns []func(*awsConfig.LoadOptions) error
	optFns = append(optFns, awsConfig.WithRegion(cfg.AWSRegion))

	// Credentials come from the SDK chain (env locally, Pod Identity on EKS).

	awsCfg, err := awsConfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.AWSEndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.AWSEndpointURL)
		}
	})

	return client, nil
}
