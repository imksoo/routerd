package conntrack

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Paths struct {
	Entries string
	Count   string
	Max     string
}

type Snapshot struct {
	Count int
	Max   int
}

func DefaultPaths() Paths {
	return Paths{
		Entries: "/proc/net/nf_conntrack",
		Count:   "/proc/sys/net/netfilter/nf_conntrack_count",
		Max:     "/proc/sys/net/netfilter/nf_conntrack_max",
	}
}

func ReadSnapshot(paths Paths) (Snapshot, error) {
	if paths.Entries == "" {
		paths.Entries = "/proc/net/nf_conntrack"
	}
	if paths.Count == "" {
		paths.Count = "/proc/sys/net/netfilter/nf_conntrack_count"
	}
	if paths.Max == "" {
		paths.Max = "/proc/sys/net/netfilter/nf_conntrack_max"
	}
	count, countErr := readInt(paths.Count)
	if countErr != nil {
		var err error
		count, err = countEntries(paths.Entries)
		if err != nil {
			return Snapshot{}, err
		}
	}
	max, _ := readInt(paths.Max)
	return Snapshot{Count: count, Max: max}, nil
}

func countEntries(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count, scanner.Err()
}

func readInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func RecordMetrics(ctx context.Context, snapshot Snapshot, createdDelta int64) {
	meter := otel.Meter("routerd.conntrack")
	countGauge, _ := meter.Int64Gauge("routerd.conntrack.entries.count")
	countGauge.Record(ctx, int64(snapshot.Count))
	if snapshot.Max > 0 {
		maxGauge, _ := meter.Int64Gauge("routerd.conntrack.entries.max")
		maxGauge.Record(ctx, int64(snapshot.Max))
	}
	if createdDelta > 0 {
		created, _ := meter.Int64Counter("routerd.conntrack.entries.created")
		created.Add(ctx, createdDelta, metric.WithAttributes(attribute.String("routerd.conntrack.source", "procfs")))
	}
}
