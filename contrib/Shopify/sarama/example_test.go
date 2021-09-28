// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016 Datadog, Inc.

package sarama_test

import (
	"log"

	"github.com/Shopify/sarama"

	saramatrace "gopkg.in/CodapeWild/dd-trace-go.v1/contrib/Shopify/sarama"
	"gopkg.in/CodapeWild/dd-trace-go.v1/ddtrace/tracer"
)

func Example_asyncProducer() {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V0_11_0_0 // minimum version that supports headers which are required for tracing

	producer, err := sarama.NewAsyncProducer([]string{"localhost:9092"}, cfg)
	if err != nil {
		panic(err)
	}
	defer producer.Close()

	producer = saramatrace.WrapAsyncProducer(cfg, producer)

	msg := &sarama.ProducerMessage{
		Topic: "some-topic",
		Value: sarama.StringEncoder("Hello World"),
	}
	producer.Input() <- msg
}

func Example_syncProducer() {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer([]string{"localhost:9092"}, cfg)
	if err != nil {
		panic(err)
	}
	defer producer.Close()

	producer = saramatrace.WrapSyncProducer(cfg, producer)

	msg := &sarama.ProducerMessage{
		Topic: "some-topic",
		Value: sarama.StringEncoder("Hello World"),
	}
	_, _, err = producer.SendMessage(msg)
	if err != nil {
		panic(err)
	}
}

func Example_consumer() {
	consumer, err := sarama.NewConsumer([]string{"localhost:9092"}, nil)
	if err != nil {
		panic(err)
	}
	defer consumer.Close()

	consumer = saramatrace.WrapConsumer(consumer)

	partitionConsumer, err := consumer.ConsumePartition("some-topic", 0, sarama.OffsetNewest)
	if err != nil {
		panic(err)
	}
	defer partitionConsumer.Close()

	consumed := 0
	for msg := range partitionConsumer.Messages() {
		// if you want to use the kafka message as a parent span:
		if spanctx, err := tracer.Extract(saramatrace.NewConsumerMessageCarrier(msg)); err == nil {
			// you can create a span using ChildOf(spanctx)
			_ = spanctx
		}

		log.Printf("Consumed message offset %d\n", msg.Offset)
		consumed++
	}
}
