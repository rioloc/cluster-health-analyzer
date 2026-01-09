package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/prometheus/common/model"
)

const (
	// defaultRoundedDuration is 5 minutes
	defaultRoundedDuration = 5 * time.Minute
	// defaultPrometheusURL points to localhost
	defaultPrometheusURL = "http://localhost:9090/"
)

func main() {
	ctx := context.Background()

	daysPtr := flag.Int("d", 0, "number of days")
	stepPtr := flag.Int("s", 0, "step")

	// This scans the arguments passed to the program
	flag.Parse()

	if *daysPtr == 0 {
		fmt.Println("missing number of days")
		return
	}

	if *stepPtr == 0 {
		fmt.Println("missing step")
		return
	}

	days := time.Duration(*daysPtr) * 24 * time.Hour
	step := time.Duration(*stepPtr) * time.Minute

	// Implement Step 0
	//now := roundedTimeNow(defaultRoundedDuration, time.Now().Add(-2*24*time.Hour))
	now := roundedTimeNow(defaultRoundedDuration, time.Now())

	// Init Prometheus Loader
	prometheusLoader, err := prom.NewLoader(defaultPrometheusURL)
	if err != nil {
		panic(err)
	}
	displayIncidentInformation(ctx, now, prometheusLoader, days, step)
}

func roundedTimeNow(duration time.Duration, t time.Time) time.Time {
	return t.Truncate(duration)
}

func displayIncidentInformation(ctx context.Context, now time.Time, loader prom.Loader, days, step time.Duration) {
	start := now.Add(-days)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "group_id\talertname\tnamespace\tseverity\ttotal\tstart_relative\tend_relative\tstart\tend\tstep")

	incidents, err := loader.LoadVectorRange(
		ctx,
		`cluster_health_components_map{}`,
		start,
		now,
		step,
	)
	if err != nil {
		panic(err)
	}

	for _, data := range incidents {
		labels := data.Metric
		groupId := string(labels["group_id"])
		alertName := string(labels["src_alertname"])
		namespace := string(labels["src_namespace"])
		severity := string(labels["src_severity"])

		if alertName == "" {
			continue
		}

		total := len(data.Samples)
		if total == 0 {
			fmt.Println("empty samples")
			continue
		}

		firstRelative := data.Samples[0].Timestamp
		lastRelative := data.Samples[total-1].Timestamp

		first, last, err := getFirstAndLastOverTime(loader, now, labels)
		if err != nil {
			panic(err)
		}

		printRow(w, groupId, alertName, namespace, severity, total, firstRelative.Time(), lastRelative.Time(), first, last, step)
	}
	w.Flush()
}

func getFirstAndLastOverTime(loader prom.Loader, timestamp time.Time, labels model.LabelSet) (time.Time, time.Time, error) {
	labelPairs := []string{}
	for k, v := range labels {
		if k != "group_id" && k != "src_alertname" && k != "namespace" && k != "severity" {
			// skipping
			continue
		}
		labelPairs = append(labelPairs, fmt.Sprintf(`%s="%s"`, k, v))
	}
	query := fmt.Sprintf("cluster_health_components_map{%s}", strings.Join(labelPairs, ", "))

	//from := time.Now()
	minOverTimeQuery := fmt.Sprintf(`min_over_time(timestamp(%s)[15d:5m])`, query)
	minOverTime, err := loader.LoadInstantValue(context.Background(), minOverTimeQuery, timestamp)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	//fmt.Printf("min_over_time took %.1f seconds\n", time.Since(from).Seconds())

	//from = time.Now()
	lastOverTimeQuery := fmt.Sprintf(`last_over_time(timestamp(%s)[15d:5m])`, query)
	lastOverTime, err := loader.LoadInstantValue(context.Background(), lastOverTimeQuery, timestamp)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	//fmt.Printf("last_over_time took %.1f seconds\n", time.Since(from).Seconds())

	first := minOverTime.(model.Vector)[0].Value
	last := lastOverTime.(model.Vector)[0].Value

	return time.Unix(int64(first), 0), time.Unix(int64(last), 0), nil
}

func printRow(w *tabwriter.Writer, groupId, alertname, namespace, severity string, total int, firstRelative, lastRelative, first, last time.Time, step time.Duration) {
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t", groupId, alertname, namespace, severity, total, firstRelative, lastRelative, first, last, step))
}
