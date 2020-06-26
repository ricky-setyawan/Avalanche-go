package router

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

func setupMultiLevelQueue(t *testing.T, bufferSize int) (MessageQueue, chan struct{}, validators.Set) {
	vdrs := validators.NewSet()
	metrics := &metrics{}
	metrics.Initialize("", prometheus.NewRegistry())
	consumptionRanges := []float64{
		0.5,
		0.75,
		1.5,
		math.MaxFloat64,
	}

	cpuInterval := float64(DefaultCPUInterval)
	// Defines the percentage of CPU time allotted to processing messages
	// from the bucket at the corresponding index.
	consumptionAllotments := []float64{
		cpuInterval * 0.25,
		cpuInterval * 0.25,
		cpuInterval * 0.25,
		cpuInterval * 0.25,
	}

	queue, semaChan := NewMultiLevelQueue(
		vdrs,
		logging.NoLog{},
		metrics,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		float64(time.Second),
		defaultStakerPortion,
	)

	return queue, semaChan, vdrs
}

func TestMultiLevelQueueSendsMessages(t *testing.T) {
	bufferSize := 8
	queue, semaChan, vdrs := setupMultiLevelQueue(t, bufferSize)
	vdrList := []validators.Validator{}
	messages := []message{}
	for i := 0; i < bufferSize; i++ {
		vdr := validators.GenerateRandomValidator(2)
		messages = append(messages, message{
			validatorID: vdr.ID(),
		})
		vdrList = append(vdrList, vdr)
	}

	vdrs.Set(vdrList)
	queue.EndInterval()

	for _, msg := range messages {
		queue.PushMessage(msg)
	}

	for count := 0; count < bufferSize; count++ {
		select {
		case _, ok := <-semaChan:
			if !ok {
				t.Fatal("Semaphore channel was closed early unexpectedly")
			}
			if _, err := queue.PopMessage(); err != nil {
				t.Fatalf("Pop message failed with error: %s", err)
			}
		default:
			t.Fatalf("Should have read message %d from queue", count)
		}
	}

	// Ensure that the 6th message was never added to the queue
	select {
	case _ = <-semaChan:
		t.Fatal("Semaphore channel should have been empty after reading all messages from the queue")
	default:
	}
}

func TestExtraMessageDeadlock(t *testing.T) {
	bufferSize := 8
	oversizedBuffer := bufferSize * 2
	queue, semaChan, vdrs := setupMultiLevelQueue(t, bufferSize)

	vdrList := []validators.Validator{}
	messages := []message{}
	for i := 0; i < oversizedBuffer; i++ {
		vdr := validators.GenerateRandomValidator(2)
		messages = append(messages, message{
			validatorID: vdr.ID(),
		})
		vdrList = append(vdrList, vdr)
	}

	vdrs.Set(vdrList)
	queue.EndInterval()

	// Test messages are dropped when full to avoid blocking when
	// adding a message to a queue or to the counting semaphore channel
	for _, msg := range messages {
		queue.PushMessage(msg)
	}

	// There should now be [bufferSize] messages on the queue
	// Note: this may not be the case where a message is dropped
	// because there is less than [bufferSize] room on the multi-level
	// queue as a result of rounding when calculating the size of the
	// single-level queues.
	for i := 0; i < bufferSize; i++ {
		<-semaChan
	}
	select {
	case <-semaChan:
		t.Fatal("Semaphore channel should have been empty")
	default:
	}
}

func TestMultiLevelQueuePrioritizes(t *testing.T) {
	bufferSize := 8
	vdrs := validators.NewSet()
	validator1 := validators.GenerateRandomValidator(2000)
	validator2 := validators.GenerateRandomValidator(2000)
	vdrs.Set([]validators.Validator{
		validator1,
		validator2,
	})

	metrics := &metrics{}
	metrics.Initialize("", prometheus.NewRegistry())
	// Set tier1 cutoff sufficiently low so that only messages from validators
	// the message queue has not serviced will be placed on it for the test.
	tier1 := 0.001
	tier2 := 1.0
	tier3 := math.MaxFloat64
	consumptionRanges := []float64{
		tier1,
		tier2,
		tier3,
	}

	// cpuInterval := float64(3 * time.Second)
	perTier := float64(time.Second)
	// Give each tier 1 second of processing time
	consumptionAllotments := []float64{
		perTier,
		perTier,
		perTier,
	}

	queue, semaChan := NewMultiLevelQueue(
		vdrs,
		logging.NoLog{},
		metrics,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		float64(time.Second),
		defaultStakerPortion,
	)

	// Utilize CPU such that the next message from validator2 will be placed on a lower
	// level queue (but be sure not to consume the entire CPU allotment for tier1)
	queue.UtilizeCPU(validator2.ID(), perTier/2)

	// Push two messages from from high priority validator and one from
	// low priority validator
	messages := []message{
		message{
			validatorID: validator1.ID(),
			requestID:   1,
		},
		message{
			validatorID: validator1.ID(),
			requestID:   2,
		},
		message{
			validatorID: validator2.ID(),
			requestID:   3,
		},
	}

	for _, msg := range messages {
		queue.PushMessage(msg)
	}

	<-semaChan
	if msg1, err := queue.PopMessage(); err != nil {
		t.Fatal(err)
	} else if !msg1.validatorID.Equals(validator1.ID()) {
		t.Fatal("Expected first message to come from the high priority validator")
	}

	// Utilize the remainder of the time that should be alloted to the highest priority
	// queue.
	queue.UtilizeCPU(validator1.ID(), perTier)

	<-semaChan
	if msg2, err := queue.PopMessage(); err != nil {
		t.Fatal(err)
	} else if !msg2.validatorID.Equals(validator2.ID()) {
		t.Fatal("Expected second message to come from the low priority validator after moving on to the lower level queue")
	}

	<-semaChan
	if msg3, err := queue.PopMessage(); err != nil {
		t.Fatal(err)
	} else if !msg3.validatorID.Equals(validator1.ID()) {
		t.Fatal("Expected final message to come from validator1")
	}
}
