/*
TWO-STEP APPROACH

This example has the following goal:
Step 0)
  - Round time.Now() within 5 minutes

Step 1)
- Perform a query_range API call to promethes
- Capture all the incidents within the time window
- Displays the datapoints differences with 5m, 30m and 1h step
- Display incident status
  - Show the firing/resolved flapping state of an incident

Step 2)
- For each incident execute an instant query to get the latest timestamp
- Display incident status
  - Show the consistent state
*/
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

const (
	// defaultRoundedDuration is 5 minutes
	defaultRoundedDuration = 5 * time.Minute
	// defaultTimeRange is 15 days
	defaultTimeRange = 15 * 24 * time.Hour
	// defaultPrometheusURL points to localhost
	defaultPrometheusURL = "http://localhost:9090/"
)

func main() {
	ctx := context.Background()

	// Implement Step 0
	//now := roundedTimeNow(defaultRoundedDuration, time.Now().Add(-2*24*time.Hour))
	now := roundedTimeNow(defaultRoundedDuration, time.Now())

	// Init Prometheus Loader
	prometheusLoader, err := prom.NewLoader(defaultPrometheusURL)
	if err != nil {
		panic(err)
	}
	steps := []time.Duration{
		5 * time.Minute,
		30 * time.Minute,
		time.Hour,
		24 * time.Hour,
	}

	for _, step := range steps {
		groupId := "4c6ec4fb-342d-483d-8f1f-effc122d70c5"

		fmt.Println("--------------------------")
		fmt.Printf("now: %s | Displaying informations for %s with a %v step\n\n", formatToRFC3339(now), groupId, step)
		displayIncidentInformation(ctx, now, *prometheusLoader, groupId, step)
	}
}

func roundedTimeNow(duration time.Duration, t time.Time) time.Time {
	return t.Truncate(duration)
}

func displayIncidentInformation(ctx context.Context, now time.Time, loader prom.Loader, groupId string, step time.Duration) {

	start := now.Add(-defaultTimeRange)

	rangeVector, err := loader.LoadVectorRange(
		ctx,
		fmt.Sprintf(`cluster_health_components_map{group_id="%s"}`, groupId),
		start,
		now,
		step,
	)
	if err != nil {
		panic(err)
	}

	for _, data := range rangeVector {
		labels := data.Metric.MLabels()
		groupId := labels["group_id"]
		alertName := labels["src_alertname"]

		if alertName == "" {
			continue
		}

		total := len(data.Samples)
		if total == 0 {
			fmt.Println("empty samples")
			continue
		}

		//first := data.Samples[0].Timestamp
		//last := data.Samples[total-1].Timestamp
		//resolved := now.Sub(last.Time()) > step
		//if resolved {
		//fmt.Printf("incident > start_time: %v | end_time: -\n", formatToRFC3339(first.Time()))
		//} else {
		//fmt.Printf("incident > start_time: %v | end_time: %v\n", formatToRFC3339(first.Time()), formatToRFC3339(last.Time()))
		//}

		// passing start instead of first datapoint timestamp for the incident (data.Samples[0].Timestamp) because an alert may exists before the incident was triggered
		alertsData, err := loader.LoadVectorRange(ctx, fmt.Sprintf(`ALERTS{alertstate!="pending", alertname="%s"}`, alertName), start, now, step)
		if err != nil {
			panic(err)
		}
		for _, alert := range alertsData {
			// let's assume that ALERTS data exists, because it caused the producing of cluster_health_components_map metric
			// so skipping safety checks
			first := alert.Samples[0].Timestamp

			if len(alert.Samples) == 1 {
				resolved := now.Sub(first.Time()) > step
				printRow(groupId, alertName, 1, first.Time(), first.Time(), 0, statusString(resolved))
				continue
			}
			last := data.Samples[total-1].Timestamp
			delta := last - data.Samples[total-2].Timestamp
			resolved := now.Sub(last.Time()) > step
			if resolved {
				printRow(groupId, alertName, total, first.Time(), last.Time(), int64(delta/1000), statusString(resolved))
				continue
			}

			printRow(groupId, alertName, total, first.Time(), time.Time{}, int64(delta/1000), statusString(resolved))
		}
	}
}

// formatToRFC3339 formats a time to RFC3339 string, returns empty string for zero time
func formatToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func statusString(status bool) string {
	if status {
		return "resolved"
	}
	return "firing"
}

func printRow(groupId, alertName string, total int, start, end time.Time, step int64, status string) {
	fmt.Printf("%s | %s\t| #datapoints: %v\t| start: %v | end: %v | step: %v | status:%s\n", groupId, alertName, total, formatToRFC3339(start), formatToRFC3339(end), step, status)
}
