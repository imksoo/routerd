// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// This executable is a real supervised child. Its TERM handler acknowledges
// cancellation but cannot finish until the parent releases the private socket.
func TestSupervisedRuntimeExitChild(t *testing.T) {
	socket := os.Getenv("ROUTERD_TEST_SUPERVISED_EXIT_SOCKET")
	if socket == "" {
		return
	}
	terms := make(chan os.Signal, 1)
	signal.Notify(terms, syscall.SIGTERM)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintln(connection, "ready"); err != nil {
		t.Fatal(err)
	}
	<-terms
	if _, err := fmt.Fprintln(connection, "stopping"); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(connection).ReadString('\n'); err != nil || line != "release\n" {
		t.Fatalf("stop release=%q error=%v", line, err)
	}
}

// Exercise actual Runner.Start/Wait, the existing supervisor, exec.Wait and
// SQLite. Generation cleanup alone must not acknowledge a still-running child.
func TestRuntimeWaitJoinsSupervisedChildAfterCancellation(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	useSupervisedDaemonMarkerTestRoot(t)
	dir := t.TempDir()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(filepath.Join(dir, "routerd-pppoe-client"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	socket := filepath.Join(dir, "child.sock")
	t.Setenv("ROUTERD_TEST_SUPERVISED_EXIT_SOCKET", socket)
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := &Runner{Router: &api.Router{}, Store: store, Bus: bus.New(), Opts: Options{Logger: logger, EnabledControllers: []string{"private-supervisor"}}}
	spec := supervisedDaemonSpec{ResourceName: "private-runtime-exit", Binary: "routerd-pppoe-client", Args: []string{"-test.run=^TestSupervisedRuntimeExitChild$", "--", "daemon", "--resource", "private-runtime-exit"}}
	runner.generationBuilder = func(ctx context.Context, _ *slog.Logger, _ eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
		controller := framework.FuncController{ControllerName: "private-supervisor", Every: time.Hour, PeriodicFunc: func(ctx context.Context) (bool, error) {
			return didWorkError(runner.reconcileSupervisedDaemonSpecs(ctx, logger, []supervisedDaemonSpec{spec}))
		}}
		return []framework.Controller{controller}, DaemonStatusController{}, controllerLifetimes{}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	var connection net.Conn
	var childDone <-chan struct{}
	t.Cleanup(func() {
		cancel()
		if connection != nil {
			_, _ = fmt.Fprintln(connection, "release")
			_ = connection.Close()
		}
		if childDone != nil {
			select {
			case <-childDone:
			case <-time.After(3 * time.Second):
				t.Error("private supervisor did not reap its child")
			}
		}
	})
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		connection, err = net.DialTimeout("unix", socket, 100*time.Millisecond)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("private supervised child was not ready")
		}
		time.Sleep(time.Millisecond)
	}
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	line, err := reader.ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("child readiness=%q %v", line, err)
	}
	runner.supervisedMu.Lock()
	for _, state := range runner.clientDaemonStates {
		childDone = state.Done
	}
	runner.supervisedMu.Unlock()
	if childDone == nil {
		t.Fatal("actual supervisor has no completion acknowledgement")
	}
	cancel()
	line, err = reader.ReadString('\n')
	if err != nil || line != "stopping\n" {
		t.Fatalf("child TERM acknowledgement=%q %v", line, err)
	}
	// The child is deterministically alive here: it cannot exit before release.
	waitCtx, stop := context.WithTimeout(context.Background(), 80*time.Millisecond)
	err = runner.Wait(waitCtx)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("parent acknowledged exit before supervised child completed: %v", err)
	}
	select {
	case <-childDone:
		t.Fatal("child exited before the release barrier")
	default:
	}
	if _, err := fmt.Fprintln(connection, "release"); err != nil {
		t.Fatal(err)
	}
	waitCtx, stop = context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := runner.Wait(waitCtx); err != nil {
		t.Fatalf("released child did not join parent: %v", err)
	}
	select {
	case <-childDone:
	default:
		t.Fatal("parent returned before actual exec.Wait acknowledgement")
	}
}
