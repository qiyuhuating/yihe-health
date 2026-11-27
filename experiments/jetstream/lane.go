// Package lane is an isolated JetStream transport experiment. It is not an
// authenticated device API, business database or production queue adapter.
package lane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Class string

const (
	Telemetry Class = "telemetry"
	SOS       Class = "sos"
)

// Lane owns a bounded, file-backed work queue. Each class must use an
// independent broker deployment to isolate disk, connections and consumers.
type Lane struct {
	js       jetstream.JetStream
	consumer jetstream.Consumer
	subject  string
}

// Open creates or validates a lane; it never updates an existing stream to
// silently weaken its durability, capacity or retention policy.
func Open(ctx context.Context, nc *nats.Conn, class Class, replicas int, maxBytes int64, maxMessages int64) (*Lane, error) {
	if (class != Telemetry && class != SOS) || replicas < 1 || replicas > 5 || maxBytes <= 0 || maxMessages <= 0 {
		return nil, errors.New("invalid lane configuration")
	}
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	name := "YIHE_" + string(class)
	subject := "yihe.next." + string(class)
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: name, Subjects: []string{subject},
		Storage: jetstream.FileStorage, Retention: jetstream.WorkQueuePolicy,
		Discard: jetstream.DiscardNew, Replicas: replicas,
		MaxBytes: maxBytes, MaxMsgs: maxMessages, MaxMsgSize: 256 * 1024,
		Duplicates: 2 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s stream: %w", class, err)
	}
	consumer, err := stream.CreateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "commit", FilterSubject: subject,
		AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy,
		AckWait: 30 * time.Second, MaxDeliver: -1, MaxAckPending: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s consumer: %w", class, err)
	}
	return &Lane{js: js, consumer: consumer, subject: subject}, nil
}

// Publish waits for a broker acknowledgement of the exact encoded event.
// An ACK is not a business commit or a notification-delivery receipt.
// Byte-identical retries are suppressed within the broker's deduplication
// window. The caller must retain the encoded event on uncertain results.
func (l *Lane) Publish(ctx context.Context, encoded []byte) (*jetstream.PubAck, error) {
	if len(encoded) == 0 || len(encoded) > 256*1024 {
		return nil, errors.New("invalid encoded event length")
	}
	digest := sha256.Sum256(encoded)
	return l.js.Publish(ctx, l.subject, encoded, jetstream.WithMsgID(hex.EncodeToString(digest[:])))
}

// CommitOne acknowledges the broker only after commit succeeds. The sink
// must atomically deduplicate device/epoch/request and apply the business
// mutation; a lost ACK can cause a redelivery even after a successful commit.
func (l *Lane) CommitOne(ctx context.Context, commit func(context.Context, []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	msg, err := l.consumer.Next(jetstream.FetchContext(fetchCtx))
	if err != nil {
		return err
	}
	if err := commit(ctx, msg.Data()); err != nil {
		return errors.Join(err, msg.Nak())
	}
	return msg.DoubleAck(ctx)
}
