# NATS JetStream Guide

This is a sample markdown file used for testing the mcp-mdx server.

## Introduction

NATS JetStream is a persistence layer for NATS that provides streaming, 
message replay, and exactly-once semantics.

## Consumers

A consumer is a stateful view of a stream. It tracks which messages have 
been delivered and acknowledged.

### Durable Consumers

Durable consumers persist their state across restarts. They are identified
by a unique name within the stream.

```go
// Create a durable consumer
consumer, err := js.CreateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{
    Durable: "processor",
    AckPolicy: jetstream.AckExplicitPolicy,
})
```

### Ephemeral Consumers

Ephemeral consumers are automatically cleaned up when there are no active 
subscriptions for a configured period.

## Push vs Pull Consumers

### Push Consumers

Push consumers deliver messages to a specified subject. Good for real-time
processing where you want messages delivered as they arrive.

### Pull Consumers

Pull consumers require explicit fetching of messages. Better for batch
processing and rate limiting.

```go
// Fetch messages from a pull consumer
msgs, err := consumer.Fetch(10, jetstream.FetchMaxWait(time.Second))
```

## Acknowledgment

Messages must be acknowledged to prevent redelivery:

- `Ack()` - Acknowledge successful processing
- `Nak()` - Negative acknowledge, request redelivery
- `Term()` - Terminate, don't redeliver
- `InProgress()` - Reset ack timeout

## Replay Policies

- `ReplayInstant` - Deliver all messages as fast as possible
- `ReplayOriginal` - Deliver messages at the rate they were published
