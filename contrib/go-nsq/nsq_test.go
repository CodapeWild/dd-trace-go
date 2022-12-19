// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.
// Author: CodapeWild (https://github.com/CodapeWild/)

package nsq

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

type instruction struct {
	delay time.Duration
}

type mockNSQD struct {
	t *testing.T
}

func TestTCPComm(t *testing.T) {
	listner, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		t.Fatal(err.Error())
	}
	go func() {
		for {
			conn, err := listner.Accept()
			if err != nil {
				t.Error(err.Error())
				time.Sleep(time.Second)
				continue
			}
			fmt.Printf("connection: %s", conn.RemoteAddr().String())

			go func() {
				for {
					buf := make([]byte, 1024)
					if _, err := conn.Read(buf); err != nil {
						fmt.Println(err.Error())
					} else {
						fmt.Println(string(buf))
					}
				}
			}()
		}
	}()

	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		t.Fatal(err.Error())
	}
	for i := 0; i < 10; i++ {
		if _, err := io.WriteString(conn, "hello, world "); err != nil {
			fmt.Printf("write stirng through conn failed: %s\n", err.Error())
		}
	}

	select {}
}
