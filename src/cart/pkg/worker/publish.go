package worker

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"time"
)

// PublishBatch retries only unconfirmed, retryable entries, preserving their IDs.
// The returned set contains only entries explicitly acknowledged by SQS.
func PublishBatch(ctx context.Context, client SQSAPI, queueURL string, entries []sqstypes.SendMessageBatchRequestEntry) (map[string]bool, error) {
	accepted := make(map[string]bool)
	pending := entries
	for attempt := 0; attempt < 3 && len(pending) > 0; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return accepted, ctx.Err()
			case <-timer.C:
			}
		}
		out, err := client.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(queueURL), Entries: pending})
		if err != nil {
			return accepted, err
		}
		permanent := make(map[string]bool)
		if out != nil {
			for _, result := range out.Successful {
				accepted[aws.ToString(result.Id)] = true
			}
			for _, result := range out.Failed {
				if result.SenderFault {
					permanent[aws.ToString(result.Id)] = true
				}
			}
		}
		next := make([]sqstypes.SendMessageBatchRequestEntry, 0)
		for _, entry := range pending {
			id := aws.ToString(entry.Id)
			if !accepted[id] && !permanent[id] {
				next = append(next, entry)
			}
		}
		pending = next
	}
	for _, entry := range entries {
		if !accepted[aws.ToString(entry.Id)] {
			return accepted, fmt.Errorf("SQS did not acknowledge every batch entry")
		}
	}
	return accepted, nil
}
