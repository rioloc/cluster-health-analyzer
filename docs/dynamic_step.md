
## Naive implementation

```go

        queryTimeRange := v1.Range{
                Start: timeNow.Add(-time.Duration(timeRange) * time.Hour),
                End:   timeNow,
-               Step:  300 * time.Second,
+               Step:  calculateStepByTimeRange(timeRange),
        }

...

+func calculateStepByTimeRange(timeRange int) time.Duration {
+       // if less than a day use a 5m step
+       if timeRange < 24 {
+               return 5 * time.Minute
+       }
+       // if between one day and one week use a 30m step
+       if timeRange < 168 {
+               return 30 * time.Minute
+       }
+       // more than a week (with a Max of 15days)
+       return time.Hour
+}
```

## Problem Statement
Software interfaces built on top of time-series database often face data consistency challenges when calculating the status of an entity (e.g., "firing" vs. "resolved") across varying time windows. This issue stems from the use of a dynamic step parameter in range queries, which is typically implemented to balance granularity with database performance.
For instance, a query spanning 1 hour might use a 5-minute step, while a 15-day query might use a 1-hour step. Because Prometheus samples data at these specific intervals, an incident that begins and ends between two steps may be misrepresented or entirely missed. This creates a scenario where an incident's status appears to change based solely on the user's "zoom level" rather than the underlying data.
## Example

**time_range 360h (15 days) with a 1h step has status firing**

```json
{"incidents":{"total":2,"items":[{"id":"4c6ec4fb-342d-483d-8f1f-effc122d70c5","severity":"warning","start_time":"2025-12-22T13:12:50+01:00","status":"firing","end_time":"","url_details":"https://console-openshift-console.apps.cluster.criolo.2025.12.29.ccxdev.devshift.net/monitoring/incidents?groupId=4c6ec4fb-342d-483d-8f1f-effc122d70c5","alerts":[{"name":"LongAlert-1","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-22T13:12:50+01:00","status":"firing"},{"name":"LongAlert-2","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-23T06:12:50+01:00","status":"firing"},{"name":"LongAlert-3","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-23T22:12:50+01:00","status":"firing"},{"name":"LongAlert-4","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-24T15:12:50+01:00","status":"firing"},{"name":"LongAlert-5","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-25T08:12:50+01:00","status":"firing"},{"name":"LongAlert-6","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-26T00:12:50+01:00","status":"firing"},{"name":"LongAlert-7","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-26T17:12:50+01:00","status":"firing"},{"name":"LongAlert-8","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-27T10:12:50+01:00","status":"firing"},{"name":"LongAlert-9","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-28T02:12:50+01:00","status":"firing"},{"name":"LongAlert-10","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-28T19:12:50+01:00","status":"firing"}],"affected_components":["Others"]},{"id":"eb4b0c2e-48d4-45eb-b5f5-81a92e90fde0","severity":"warning","start_time":"2025-12-29T12:12:50+01:00","status":"firing","end_time":"","url_details":"https://console-openshift-console.apps.cluster.criolo.2025.12.29.ccxdev.devshift.net/monitoring/incidents?groupId=eb4b0c2e-48d4-45eb-b5f5-81a92e90fde0","alerts":[{"name":"AlertmanagerReceiversNotConfigured","namespace":"openshift-monitoring","severity":"warning","silenced":"false","start_time":"2025-12-29T11:12:50+01:00","status":"firing"},{"container":"insights-operator","description":"OLM operator installations and upgrades fail when the current OCP cluster version is too old","endpoint":"https","info_link":"https://console.redhat.com/openshift/insights/advisor/clusters/4aeb0ea0-aef9-4510-a591-e48fa6c2e810?first=ccx_rules_ocp.external.rules.cluster_does_not_support_olm_operators_network_policies%7COCP_DOES_NOT_SUPPORT_OLM_OPERATORS_NETWORK_POLICIES","instance":"10.130.0.33:8443","job":"metrics","name":"InsightsRecommendationActive","namespace":"openshift-insights","service":"metrics","severity":"info","silenced":"false","start_time":"2025-12-29T11:12:50+01:00","status":"firing","total_risk":"Moderate"}],"affected_components":["insights","monitoring"]}]}}
```

**time_range 1h with 5m step has status resolved**

```json
{"incidents":{"total":3,"items":[{"id":"da19bd35-df66-4b4d-9f06-18b74ada61a1","severity":"warning","start_time":"2025-12-29T11:29:37+01:00","status":"resolved","end_time":"2025-12-29T11:44:37+01:00","url_details":"https://console-openshift-console.apps.cluster.criolo.2025.12.29.ccxdev.devshift.net/monitoring/incidents?groupId=da19bd35-df66-4b4d-9f06-18b74ada61a1","alerts":[{"condition":"Recommended","end_time":"2025-12-29T11:44:37+01:00","name":"CannotEvaluateConditionalUpdates","reason":"EvaluationFailed","severity":"warning","silenced":"false","start_time":"2025-12-29T11:29:37+01:00","status":"resolved","version":"4.19.21"}],"affected_components":["Others"]},{"id":"eb4b0c2e-48d4-45eb-b5f5-81a92e90fde0","severity":"warning","start_time":"2025-12-29T11:24:37+01:00","status":"firing","end_time":"","url_details":"https://console-openshift-console.apps.cluster.criolo.2025.12.29.ccxdev.devshift.net/monitoring/incidents?groupId=eb4b0c2e-48d4-45eb-b5f5-81a92e90fde0","alerts":[{"container":"insights-operator","description":"OLM operator installations and upgrades fail when the current OCP cluster version is too old","endpoint":"https","info_link":"https://console.redhat.com/openshift/insights/advisor/clusters/4aeb0ea0-aef9-4510-a591-e48fa6c2e810?first=ccx_rules_ocp.external.rules.cluster_does_not_support_olm_operators_network_policies%7COCP_DOES_NOT_SUPPORT_OLM_OPERATORS_NETWORK_POLICIES","instance":"10.130.0.33:8443","job":"metrics","name":"InsightsRecommendationActive","namespace":"openshift-insights","service":"metrics","severity":"info","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"firing","total_risk":"Moderate"},{"name":"AlertmanagerReceiversNotConfigured","namespace":"openshift-monitoring","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"firing"}],"affected_components":["insights","monitoring"]},{"id":"4c6ec4fb-342d-483d-8f1f-effc122d70c5","severity":"warning","start_time":"2025-12-29T11:14:37+01:00","status":"resolved","end_time":"2025-12-29T11:19:37+01:00","url_details":"https://console-openshift-console.apps.cluster.criolo.2025.12.29.ccxdev.devshift.net/monitoring/incidents?groupId=4c6ec4fb-342d-483d-8f1f-effc122d70c5","alerts":[{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-5","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-8","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-1","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-6","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-7","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-9","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-10","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-2","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-3","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"},{"end_time":"2025-12-29T11:19:37+01:00","name":"LongAlert-4","namespace":"openshift-core","severity":"warning","silenced":"false","start_time":"2025-12-29T11:14:37+01:00","status":"resolved"}],"affected_components":["Others"]}]}}
```

Running an analysis script we can see that using a less granular step of the incident with group_id 4c6ec4fb-342d-483d-8f1f-effc122d70c5, leveraging on the current/following formula, changes with different steps:

```go
+   // an incident/alert is considered as solved if the delta between the query end_time
+   // and the last recorded sample is greater than the step
    if qRange.End.Sub(lastSample.Timestamp.Time()).Seconds() > qRange.Step.Seconds() {
      endTime = lastSample.Timestamp.Time()
    }
```



```text
2025/12/29 12:39:45 WARN Connecting to Prometheus without TLS
--------------------------
now: 2025-12-29T12:35:00+01:00 | Displaying informations for 4c6ec4fb-342d-483d-8f1f-effc122d70c5 with a 5m0s step

4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-1      | #datapoints: 2001     | start: 2025-12-22T12:40:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-10     | #datapoints: 201      | start: 2025-12-28T18:40:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-2      | #datapoints: 1801     | start: 2025-12-23T05:20:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-3      | #datapoints: 1601     | start: 2025-12-23T22:00:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-4      | #datapoints: 1401     | start: 2025-12-24T14:40:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-5      | #datapoints: 1201     | start: 2025-12-25T07:20:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-6      | #datapoints: 1001     | start: 2025-12-26T00:00:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-7      | #datapoints: 801      | start: 2025-12-26T16:40:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-8      | #datapoints: 601      | start: 2025-12-27T09:20:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-9      | #datapoints: 401      | start: 2025-12-28T02:00:00+01:00 | end: 2025-12-29T11:20:00+01:00 | step: 300 | status:resolved
--------------------------
now: 2025-12-29T12:35:00+01:00 | Displaying informations for 4c6ec4fb-342d-483d-8f1f-effc122d70c5 with a 30m0s step

4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-1      | #datapoints: 335      | start: 2025-12-22T13:05:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-10     | #datapoints: 35       | start: 2025-12-28T19:05:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-2      | #datapoints: 302      | start: 2025-12-23T05:35:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-3      | #datapoints: 269      | start: 2025-12-23T22:05:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-4      | #datapoints: 235      | start: 2025-12-24T15:05:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-5      | #datapoints: 202      | start: 2025-12-25T07:35:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-6      | #datapoints: 169      | start: 2025-12-26T00:05:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-7      | #datapoints: 135      | start: 2025-12-26T17:05:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-8      | #datapoints: 102      | start: 2025-12-27T09:35:00+01:00 | end:  | step: 1800 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-9      | #datapoints: 69       | start: 2025-12-28T02:05:00+01:00 | end:  | step: 1800 | status:firing
--------------------------
now: 2025-12-29T12:35:00+01:00 | Displaying informations for 4c6ec4fb-342d-483d-8f1f-effc122d70c5 with a 1h0m0s step

4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-1      | #datapoints: 167      | start: 2025-12-22T13:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-10     | #datapoints: 17       | start: 2025-12-28T19:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-2      | #datapoints: 151      | start: 2025-12-23T05:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-3      | #datapoints: 134      | start: 2025-12-23T22:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-4      | #datapoints: 117      | start: 2025-12-24T15:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-5      | #datapoints: 101      | start: 2025-12-25T07:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-6      | #datapoints: 84       | start: 2025-12-26T00:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-7      | #datapoints: 67       | start: 2025-12-26T17:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-8      | #datapoints: 51       | start: 2025-12-27T09:35:00+01:00 | end:  | step: 3600 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-9      | #datapoints: 34       | start: 2025-12-28T02:35:00+01:00 | end:  | step: 3600 | status:firing
--------------------------
now: 2025-12-29T12:35:00+01:00 | Displaying informations for 4c6ec4fb-342d-483d-8f1f-effc122d70c5 with a 24h0m0s step

4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-1      | #datapoints: 6        | start: 2025-12-23T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-2      | #datapoints: 6        | start: 2025-12-23T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-3      | #datapoints: 5        | start: 2025-12-24T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-4      | #datapoints: 4        | start: 2025-12-25T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-5      | #datapoints: 4        | start: 2025-12-25T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-6      | #datapoints: 3        | start: 2025-12-26T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-7      | #datapoints: 2        | start: 2025-12-27T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-8      | #datapoints: 2        | start: 2025-12-27T12:35:00+01:00 | end:  | step: 86400 | status:firing
4c6ec4fb-342d-483d-8f1f-effc122d70c5 | LongAlert-9      | #datapoints: 1        | start: 2025-12-28T12:35:00+01:00 | end: 2025-12-28T12:35:00+01:00 | step: 0 | status:firing
```

## Proposed Solution


#### The Two-Step Logic Solution
To ensure logical consistency and high-precision tracking, a decoupled query architecture is proposed. This approach separates the discovery of historical trends from the determination of the current state.

**Step 1: Incident Discovery (Range Query)**
* **Method**: Execute a query_range using a **dynamic step** calculated based on the total time_range.
* **Purpose**: To identify the unique set of metric series that were present at any point during the requested window.
* **Metadata Extraction**: The **start_time** of an incident is defined by the first available data point for that specific series within the results.

**Step 2: State Verification (Instant Query)**
* **Method**: For the entities discovered in Step 1, perform a targeted **Instant Query** using a **Prometheus Subquery**.
* **Expression**: `last_over_time(timestamp(metric)[time_range:resolution])`
* **Purpose**: To bypass the coarse sampling of the range query and retrieve the absolute last timestamp recorded in the database for each entity.
* **Resolution**: The subquery resolution should be set to a high-frequency interval (e.g., **1 minute**) regardless of the broad *time_range*.

**Implementation Logic**
By comparing the precise end_time from Step 2 against a **static threshold** (e.g., 5 minutes), the server can provide a deterministic status:

**Resolved**: If (Now−EndTime>5m).
**Active**: If (Now−EndTime≤5m).

**Key Advantages**
* **Deterministic Accuracy**: Eliminates "aliasing" where incidents disappear or change status based on query resolution.
* **Performance Stability**: Maintains low pressure on the time-series database by using coarse steps for long-term trends while reserving high-resolution checks for state verification.
* **Caching Efficiency**: By rounding the query end time to a standardized window (e.g., 5-minute increments), cache hit rates are significantly improved for repeated requests.

