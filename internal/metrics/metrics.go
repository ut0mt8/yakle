package metrics

import (
	"fmt"
	"io"
	"sort"
	"sync"
)

var (
	globalMetrics = make(map[string]uint64)
	mutex         sync.RWMutex
)

func Set(metric string, value uint64) {
	mutex.Lock()
	globalMetrics[metric] = value
	mutex.Unlock()
}

func WritePrometheus(w io.Writer) {
	metrics := make([]string, 0, len(globalMetrics))

	mutex.Lock()
	for metric := range globalMetrics {
		metrics = append(metrics, metric)
	}

	sort.Strings(metrics)

	for _, metric := range metrics {
		fmt.Fprintf(w, "%s %d\n", metric, globalMetrics[metric])
	}
	mutex.Unlock()
}
