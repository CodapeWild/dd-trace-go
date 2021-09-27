// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.
// Author: CodapeWild (https://github.com/CodapeWild/)

package nsq

import (
	"math"
	"time"

	"github.com/nsqio/go-nsq"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// Producer is a wrap-up class of nsq Producer.
type Producer struct {
	*nsq.Producer
	cfg *clientConfig
}

// NewProducer return a new wrapped nsq Producer that is traced with the configurable client with opts.
func NewProducer(addr string, config *nsq.Config, opts ...Option) (*Producer, error) {
	prodc, err := nsq.NewProducer(addr, config)
	if err != nil {
		return nil, err
	}

	cfg := &clientConfig{}
	defaultConfig(cfg)
	for _, opt := range opts {
		opt(cfg)
	}

	return &Producer{
		Producer: prodc,
		cfg:      cfg,
	}, nil
}

// Publish is a nsq Producer Publish wrapper with tracing.
func (prodc *Producer) Publish(topic string, body []byte) error {
	var (
		opName = "PUBLISH"
		span   = prodc.startSpan(topic, opName)
		err    = prodc.Producer.Publish(topic, body)
		tags   = map[string]interface{}{
			"body_count": 1,
			"body_size":  len(body),
		}
	)
	prodc.finishSpan(span, topic, opName, tags, err)

	return err
}

// MultiPublish is a nsq Producer MultiPublish wrapper with tracing.
func (prodc *Producer) MultiPublish(topic string, body [][]byte) error {
	var (
		opName = "MultiPublish"
		span   = prodc.startSpan(topic, opName)
		err    = prodc.Producer.MultiPublish(topic, body)
	)
	size := 0
	for _, b := range body {
		size += len(b)
	}
	tags := map[string]interface{}{
		"body_count": len(body),
		"body_size":  size,
	}
	prodc.finishSpan(span, topic, opName, tags, err)

	return err
}

// PublishAsync is a nsq Producer PublishAsync wrapper with tracing.
func (prodc *Producer) PublishAsync(topic string, body []byte, doneChan chan *nsq.ProducerTransaction, args ...interface{}) error {
	var (
		opName = "PublishAsync"
		span   = prodc.startSpan(topic, opName)
		err    = prodc.Producer.PublishAsync(topic, body, doneChan, args...)
		tags   = map[string]interface{}{
			"body_count": 1,
			"body_size":  len(body),
			"arg_count":  len(args),
		}
	)
	prodc.finishSpan(span, topic, opName, tags, err)

	return err
}

// MultiPublishAsync is a nsq Producer MultiPublishAsync wrapper with tracing.
func (prodc *Producer) MultiPublishAsync(topic string, body [][]byte, doneChan chan *nsq.ProducerTransaction, args ...interface{}) error {
	var (
		opName = ""
		span   = prodc.startSpan(topic, opName)
		err    = prodc.Producer.MultiPublishAsync(topic, body, doneChan, args...)
	)
	size := 0
	for _, b := range body {
		size += len(b)
	}
	tags := map[string]interface{}{
		"body_count": len(body),
		"body_size":  size,
		"arg_count":  len(args),
	}
	prodc.finishSpan(span, topic, opName, tags, err)

	return err
}

// DeferredPublish is a nsq Producer DeferredPublish wrapper with tracing.
func (prodc *Producer) DeferredPublish(topic string, delay time.Duration, body []byte) error {
	var (
		opName = "DeferredPublish"
		span   = prodc.startSpan(topic, opName)
		err    = prodc.Producer.DeferredPublish(topic, delay, body)
		tags   = map[string]interface{}{
			"body_count": 1,
			"body_size":  len(body),
			"delay":      delay,
		}
	)
	prodc.finishSpan(span, topic, opName, tags, err)

	return err
}

// DeferredPublishAsync is a nsq Producer DeferredPublishAsync wrapper with tracing.
func (prodc *Producer) DeferredPublishAsync(topic string, delay time.Duration, body []byte, doneChan chan *nsq.ProducerTransaction, args ...interface{}) error {
	var (
		opName = "DeferredPublishAsync"
		span   = prodc.startSpan(topic, opName)
		err    = prodc.Producer.DeferredPublishAsync(topic, delay, body, doneChan, args...)
		tags   = map[string]interface{}{
			"body_count": 1,
			"body_size":  len(body),
			"arg_count":  len(args),
			"delay":      delay,
		}
	)
	prodc.finishSpan(span, topic, opName, tags, err)

	return err
}

func (prodc *Producer) startSpan(topic, operation string) ddtrace.Span {
	opts := []ddtrace.StartSpanOption{
		tracer.SpanType(ext.SpanTypeMessageProducer),
		tracer.ServiceName(prodc.cfg.service),
		tracer.ResourceName(topic),
	}
	if !math.IsNaN(prodc.cfg.analyticsRate) {
		opts = append(opts, tracer.Tag(ext.EventSampleRate, prodc.cfg.analyticsRate))
	}

	span, _ := tracer.StartSpanFromContext(prodc.cfg.ctx, operation, opts...)

	return span
}

func (prodc *Producer) finishSpan(span ddtrace.Span, topic, operation string, tags map[string]interface{}, err error) {
	span.SetOperationName(operation)
	for k, v := range tags {
		span.SetTag(k, v)
	}
	span.SetTag(ext.ResourceName, topic)
	var opts []ddtrace.FinishOption
	if err != nil {
		opts = append(opts, tracer.WithError(err))
	}
	span.Finish(opts...)
}