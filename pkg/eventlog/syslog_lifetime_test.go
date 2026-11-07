// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"github.com/imksoo/routerd/pkg/api"
	"net"
	"sync"
	"testing"
)

func TestSyslogConcurrentEmitAndClose(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	sink, err := NewSink(api.LogSinkSpec{Type: "syslog", Syslog: api.LogSinkSyslogSpec{Network: "udp", Address: listener.LocalAddr().String()}})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); _ = sink.Emit(Event{Level: LevelInfo, Message: "concurrent"}) }()
	}
	workers.Add(1)
	go func() { defer workers.Done(); _ = sink.Close() }()
	workers.Wait()
	if err := sink.Emit(Event{Level: LevelInfo, Message: "after-close"}); err == nil {
		t.Fatal("closed sink reopened")
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
}
