// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.
// Author: CodapeWild (https://github.com/CodapeWild/)

package nsq

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/CodapeWild/dd-trace-go.v1/ddtrace/mocktracer"
	"gopkg.in/CodapeWild/dd-trace-go.v1/ddtrace/tracer"
)

func TestInject(t *testing.T) {
	mt := mocktracer.Start()
	defer mt.Stop()

	span := tracer.StartSpan("test.go-nsq.utils")
	defer span.Finish()

	body := []byte("test data")
	injectedBody, err := inject(span, body)
	if err != nil {
		t.Fatal(err.Error())
	}

	spnctx, newbody, err := extract(injectedBody)
	if err != nil {
		t.Fatal(err.Error())
	}

	assert.Equal(t, span.Context().TraceID(), spnctx.TraceID())
	assert.Equal(t, span.Context().SpanID(), spnctx.SpanID())
	assert.Equal(t, newbody, body)
}
