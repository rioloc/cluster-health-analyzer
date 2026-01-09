package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
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

	res, err := loader.LoadInstantValue(
		ctx,
		`min_over_time(timestamp(cluster_health_components_map)[15d:5m])`,
		now,
	)
	if err != nil {
		panic(err)
	}
	minTimestamps := res.(model.Vector)

	res, err = loader.LoadInstantValue(
		ctx,
		`last_over_time(timestamp(cluster_health_components_map)[15d:5m])`,
		now,
	)
	if err != nil {
		panic(err)
	}
	lastTimestamps := res.(model.Vector)

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

		matcher := common.LabelsIntersectionMatcher{
			Labels: labels,
		}

		start := time.Time{}
		for _, sample := range minTimestamps {
			match, _ := matcher.Matches(model.LabelSet(sample.Metric))
			if match {
				start = time.Unix(int64(sample.Value), 0)
				break
			}
		}
		if start.IsZero() {
			panic("start not found")
		}

		end := time.Time{}
		for _, sample := range lastTimestamps {
			match, _ := matcher.Matches(model.LabelSet(sample.Metric))
			if match {
				end = time.Unix(int64(sample.Value), 0)
				break
			}
		}
		if end.IsZero() {
			panic("end not found")
		}

		printRow(w, groupId, alertName, namespace, severity, total, firstRelative.Time(), lastRelative.Time(), start, end, step)
	}
	w.Flush()
}

func printRow(w *tabwriter.Writer, groupId, alertname, namespace, severity string, total int, firstRelative, lastRelative, first, last time.Time, step time.Duration) {
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t", groupId, alertname, namespace, severity, total, firstRelative, lastRelative, first, last, step))
}
