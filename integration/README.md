## Run integration tests
```shell
make integration-mcp
```

## Requirements to make it work with Podman in rootless mode

Enable the Podman Socket API via systemd service

```shell
systemctl --user enable --now podman.socket
# check
systemctl --user status podman.socket
```

Update your `$HOME/.testcontainers.properties` with the following:
```
docker.host=unix://${XDG_RUNTIME_DIR}/podman/podman.sock
ryuk.disabled=true
```
The first is needed to allow the communication with podman API socket.
The second one is needed to disable Ryuk, which is a garbage collector, also named as _reaper_ in testcontainers context, used to perform some deep cleanup. Unfortunately this is not fully-compatible with Podman, so we have to disable it.

### Known limitations
`testcontainers.FromDocker` won't work for Podman rootless in the case user is onboarded on some ldap, causing it to have a huge UID, that can't be mapped by rootless Podman


### Output

```shell
2025/11/20 17:13:58 github.com/testcontainers/testcontainers-go - Connected to docker:
  Server Version: 5.6.2
  API Version: 1.41
  Operating System: fedora
  Total Memory: 31518 MB
  Testcontainers for Go Version: v0.40.0
  Resolved Docker Host: unix:///run/user/4240887/podman/podman.sock
  Resolved Docker Socket Path: /run/user/4240887/podman/podman.sock
  Test SessionID: af7ebcd747e98ab90d7f396238f296ed13c61b1008713e7e1dc127421e733715
  Test ProcessID: 1a26f09c-bccf-42c4-bdce-02392b3103d2
2025/11/20 17:13:58 🐳 Creating container for image quay.io/rh-ee-criolo/promcker:latest
2025/11/20 17:13:58 ✅ Container created: ff7a41e7e40d
2025/11/20 17:13:58 🐳 Starting container: ff7a41e7e40d
2025/11/20 17:13:58 ✅ Container started: ff7a41e7e40d
2025/11/20 17:13:58 ⏳ Waiting for container id ff7a41e7e40d image: quay.io/rh-ee-criolo/promcker:latest. Waiting for: log message "Starting Prometheus mock server on :9090"
2025/11/20 17:13:58 🔔 Container is ready: ff7a41e7e40d
[1/2] STEP 1/14: FROM golang:1.24.10 AS builder
[1/2] STEP 2/14: ARG TARGETARCH
--> Using cache eef9784bbf94da67c7f253e638f27e58e1b9d94281903f1414dfad24f2b728da
--> eef9784bbf94
[1/2] STEP 3/14: WORKDIR /src
--> Using cache 47e64b270e348741126f02a2b12ebf8f0a72771292c010bd641aeb8cff64aad7
--> 47e64b270e34
[1/2] STEP 4/14: COPY go.mod go.mod
--> 34f584cfe1ad
[1/2] STEP 5/14: COPY go.sum go.sum
--> f047a03074e9
[1/2] STEP 6/14: RUN go mod download
--> 0c0a0ea6a168
[1/2] STEP 7/14: COPY cmd cmd
--> e761b4ab7ee5
[1/2] STEP 8/14: COPY pkg pkg
--> 015cf4581528
[1/2] STEP 9/14: COPY main.go main.go
--> 4f4e6ff1b9b0
[1/2] STEP 10/14: ENV GOOS=${TARGETOS:-linux}
--> 037f06a94567
[1/2] STEP 11/14: ENV GOARCH=${TARGETARCH}
--> 791a24c0cccb
[1/2] STEP 12/14: ENV CGO_ENABLED=1
--> 59ca1b39efad
[1/2] STEP 13/14: ENV GOFLAGS=-mod=readonly
--> 8b7b3874db1b
[1/2] STEP 14/14: RUN go build -tags strictfipsruntime -o /bin/cluster-health-analyzer
--> 91d929228c31
[2/2] STEP 1/5: FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
[2/2] STEP 2/5: WORKDIR /
--> Using cache ffd75ab20358630d0b1faf104eea61bbaf1a509faad519b213b8dfd02fa60f6b
--> ffd75ab20358
[2/2] STEP 3/5: COPY --from=builder /bin/cluster-health-analyzer /bin/cluster-health-analyzer
--> Using cache a9078709e906dd7ea42cef15a73117899f4d3f218ea8315a5bba856d516637e4
--> a9078709e906
[2/2] STEP 4/5: USER 65532:65532
--> Using cache 9b32352bf892adaa80694738ba88473ea38447460957a3910a696d7dac9e3947
--> 9b32352bf892
[2/2] STEP 5/5: ENTRYPOINT ["/bin/cluster-health-analyzer"]
--> Using cache 9ad88d0b329f7347f77158285db915cdd72c820bd8bb517d67ac3e0d8ecaf65b
[2/2] COMMIT cluster-health-analyzer:integration
--> 9ad88d0b329f
Successfully tagged localhost/cluster-health-analyzer:integration
9ad88d0b329f7347f77158285db915cdd72c820bd8bb517d67ac3e0d8ecaf65b
2025/11/20 17:15:20 🐳 Creating container for image cluster-health-analyzer:integration
2025/11/20 17:15:20 ✅ Container created: 70d0a929f1e8
2025/11/20 17:15:20 🐳 Starting container: 70d0a929f1e8
2025/11/20 17:15:20 ✅ Container started: 70d0a929f1e8
2025/11/20 17:15:20 ⏳ Waiting for container id 70d0a929f1e8 image: cluster-health-analyzer:integration. Waiting for: log message "INFO Starting MCP server on  address=:8085"
2025/11/20 17:15:20 🔔 Container is ready: 70d0a929f1e8
=== RUN   Test_IncidentsHandler
--- PASS: Test_IncidentsHandler (0.02s)
PASS
2025/11/20 17:15:20 🐳 Stopping container: ff7a41e7e40d
2025/11/20 17:15:22 ✅ Container stopped: ff7a41e7e40d
2025/11/20 17:15:22 🐳 Terminating container: ff7a41e7e40d
2025/11/20 17:15:22 🚫 Container terminated: ff7a41e7e40d
2025/11/20 17:15:22 🐳 Stopping container: 70d0a929f1e8
2025/11/20 17:15:22 ✅ Container stopped: 70d0a929f1e8
2025/11/20 17:15:22 🐳 Terminating container: 70d0a929f1e8
2025/11/20 17:15:22 🚫 Container terminated: 70d0a929f1e8
ok      github.com/openshift/cluster-health-analyzer/integration        84.189s
```
