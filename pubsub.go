package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/pubsub/v2"
)

// PubSubMessage represents the message structure
type PubSubMessage struct {
	TaskID  string      `json:"task_id"`
	Event   string      `json:"event,omitempty"`
	Message interface{} `json:"message,omitempty"`
}

// Client wraps the Google Cloud PubSub client
type Client struct {
	client    *pubsub.Client
	publisher *pubsub.Publisher
	ctx       context.Context
}

// NewClient creates a new PubSub client
func NewPubSubClient(ctx context.Context) (*Client, error) {
	projectID := "1087702996606"

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	publisher := client.Publisher("projects/1087702996606/topics/seo-audit-data")

	return &Client{
		client:    client,
		publisher: publisher,
		ctx:       ctx,
	}, nil
}

// Close closes the PubSub client
func (c *Client) Close() error {
	return c.client.Close()
}

// Publish publishes a message to the seo-audit-data topic
func (c *Client) Publish(data PubSubMessage) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	result := c.publisher.Publish(c.ctx, &pubsub.Message{Data: jsonData})

	// Block until the result is returned and a server-generated ID is returned
	_, err = result.Get(c.ctx)
	if err != nil {
		log.Printf("failed to publish message: %v", err)
		return err
	}

	return nil
}

// Subscribe subscribes to messages for a specific task_id
// The callback function is called for each matching message
// Returns a cancel function to stop the subscription
func (c *Client) Subscribe(taskID string, callback func(data PubSubMessage)) (func(), error) {
	messageStart := time.Now().Add(-1 * time.Hour)
	subscription := c.client.Subscriber("projects/1087702996606/subscriptions/seo-audit-data-sub-2")

	ctx, cancel := context.WithCancel(c.ctx)

	go func() {
		err := subscription.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
			// Skip messages published before the start time
			if msg.PublishTime.Before(messageStart) {
				msg.Ack()
				return
			}

			var data PubSubMessage
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				log.Printf("failed to unmarshal message: %v", err)
				msg.Nack()
				return
			}

			// Only process messages for the specified task_id
			if data.TaskID == taskID {
				callback(data)
				msg.Ack()
			}
		})

		if err != nil && ctx.Err() == nil {
			log.Printf("subscription error: %v", err)
		}
	}()

	// Return cancel function
	return cancel, nil
}
