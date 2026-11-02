package worker

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	st "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"testing"
)

type batchClient struct {
	SQSAPI
	calls    [][]st.SendMessageBatchRequestEntry
	response func(int) (*sqs.SendMessageBatchOutput, error)
}

func (c *batchClient) SendMessageBatch(ctx context.Context, in *sqs.SendMessageBatchInput, opts ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	c.calls = append(c.calls, append([]st.SendMessageBatchRequestEntry(nil), in.Entries...))
	return c.response(len(c.calls))
}
func batchEntries() []st.SendMessageBatchRequestEntry {
	return []st.SendMessageBatchRequestEntry{{Id: aws.String("0"), MessageBody: aws.String("a")}, {Id: aws.String("1"), MessageBody: aws.String("b")}, {Id: aws.String("2"), MessageBody: aws.String("c")}}
}
func TestPublishBatchRetriesOnlyRetryableEntries(t *testing.T) {
	c := &batchClient{response: func(call int) (*sqs.SendMessageBatchOutput, error) {
		if call == 1 {
			return &sqs.SendMessageBatchOutput{Successful: []st.SendMessageBatchResultEntry{{Id: aws.String("0")}}, Failed: []st.BatchResultErrorEntry{{Id: aws.String("1")}, {Id: aws.String("2"), SenderFault: true}}}, nil
		}
		return &sqs.SendMessageBatchOutput{Successful: []st.SendMessageBatchResultEntry{{Id: aws.String("1")}}}, nil
	}}
	accepted, err := PublishBatch(context.Background(), c, "queue", batchEntries())
	if err == nil || len(accepted) != 2 || len(c.calls) != 2 {
		t.Fatalf("accepted=%v err=%v calls=%v", accepted, err, len(c.calls))
	}
	if len(c.calls[1]) != 1 || aws.ToString(c.calls[1][0].Id) != "1" || aws.ToString(c.calls[1][0].MessageBody) != "b" {
		t.Fatal("retried an accepted/permanent entry or changed its payload")
	}
}
func TestPublishBatchBoundsRetryAndPreservesPartialResults(t *testing.T) {
	c := &batchClient{response: func(call int) (*sqs.SendMessageBatchOutput, error) { return &sqs.SendMessageBatchOutput{}, nil }}
	_, err := PublishBatch(context.Background(), c, "queue", batchEntries())
	if err == nil || len(c.calls) != 3 {
		t.Fatal("retry was not bounded")
	}
	c.calls = nil
	c.response = func(call int) (*sqs.SendMessageBatchOutput, error) {
		if call == 1 {
			return &sqs.SendMessageBatchOutput{Successful: []st.SendMessageBatchResultEntry{{Id: aws.String("0")}}}, nil
		}
		return nil, errors.New("offline")
	}
	accepted, err := PublishBatch(context.Background(), c, "queue", batchEntries())
	if err == nil || !accepted["0"] || len(accepted) != 1 {
		t.Fatal("lost partial acknowledgements")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.calls = nil
	c.response = func(call int) (*sqs.SendMessageBatchOutput, error) { return &sqs.SendMessageBatchOutput{}, nil }
	_, err = PublishBatch(ctx, c, "queue", batchEntries())
	if !errors.Is(err, context.Canceled) {
		t.Fatal("retry ignored cancellation")
	}
}
