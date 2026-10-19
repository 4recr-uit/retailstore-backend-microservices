package model

// Item represents an item in a shopping cart.
type Item struct {
	ItemID    string `json:"itemId" binding:"required"`
	Quantity  int    `json:"quantity" binding:"required"`
	UnitPrice int    `json:"unitPrice" binding:"required"`
}

// Cart represents a customer's shopping cart.
type Cart struct {
	CustomerID string `json:"customerId"`
	Items      []Item `json:"items"`
}

// JobRequest represents a request to the POST /jobs benchmark endpoint.
type JobRequest struct {
	BatchSize  int         `json:"batchSize"`  // 1..10, default 1
	WorkUnits  int         `json:"workUnits"`  // 1..10000, default 100
	DBMode     string      `json:"dbMode"`     // "none" | "light" | "full", default "light"
	CustomerID string      `json:"customerId"` // optional
	Payload    interface{} `json:"payload"`    // optional
}

// JobResponse is the response returned by POST /jobs.
type JobResponse struct {
	Status string   `json:"status"`
	Queue  string   `json:"queue"`
	Count  int      `json:"count"`
	JobIDs []string `json:"jobIds"`
}

// JobMessage represents the JSON payload sent across SQS.
type JobMessage struct {
	JobID      string      `json:"jobId"`
	EnqueuedAt int64       `json:"enqueuedAt"` // epoch milliseconds
	WorkUnits  int         `json:"workUnits"`
	DBMode     string      `json:"dbMode"`
	CustomerID string      `json:"customerId,omitempty"`
	Payload    interface{} `json:"payload,omitempty"`
}

// JobLogEntry represents the structured JSON log emitted upon job completion.
type JobLogEntry struct {
	Level       string `json:"level"`
	Service     string `json:"service"`
	Queue       string `json:"queue"`
	JobID       string `json:"jobId"`
	CustomerID  string `json:"customerId,omitempty"`
	EnqueuedAt  int64  `json:"enqueuedAt"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt"`
	QueueWaitMs int64  `json:"queueWaitMs"`
	DurationMs  int64  `json:"durationMs"`
	E2EMs       int64  `json:"e2eMs"`
	IsFirstJob  bool   `json:"isFirstJob"`
	Status      string `json:"status"`
	DBMode      string `json:"dbMode"`
}
