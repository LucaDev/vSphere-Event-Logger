# vSphere Event Logger

A lightweight, statically compiled Go application that connects to a VMware vCenter Server, streams the live event feed, and outputs flattened, structured JSON directly to standard output for easy ingestion by log aggregators like [Grafana Alloy](https://grafana.com/docs/alloy/latest/), Promtail, or Filebeat.

## Features

- **Flattened JSON Output**: All nested vSphere objects (like `vm` or `host`) are deeply flattened (e.g., `vm_name`, `vm_id`) making them infinitely easier to index and query in databases like Elasticsearch or Loki.
- **Enhanced Fields**: Automatically extracts the generic event `level` (e.g., `info`, `warning`) and maps `createdTime` to `time` and `fullFormattedMessage` to `message` for instant Grafana dashboards compatibility.
- **Username Redaction**: Built-in support to securely hash usernames using SHA-256 for privacy and compliance.
- **Custom TLS CA Support**: Natively injects custom X.509 Certificate Authorities for secure connections without disabling TLS verification.
- **Zero-Dependency Container**: Provided Dockerfile builds a micro-sized `scratch` container containing only the compiled binary and root certificates.

## Configuration

The application is configured using standard `govmomi` environment variables:

| Environment Variable | Description |
|---|---|
| `GOVMOMI_URL` | **(Required)** The vCenter SDK URL (e.g., `https://vcenter.local/sdk`). |
| `GOVMOMI_USERNAME` | The vCenter username. Can also be embedded in the URL. |
| `GOVMOMI_PASSWORD` | The vCenter password. Can also be embedded in the URL. |
| `GOVMOMI_INSECURE` | Set to `true` or `1` to bypass TLS certificate verification entirely. |
| `GOVMOMI_TLS_CA_CERTS` | Path to a custom PEM-encoded CA certificate file to trust. Highly recommended over bypassing verification. |
| `REDACT_USERNAME` | Set to `true` or `1` to compute an 8-character SHA-256 hash of the `userName` field before logging it. |

*Note: You can also use the `--redact-username` CLI flag instead of the environment variable.*

## Building

A `Makefile` is provided to easily build the application.

```bash
# Build the purely static Go binary locally
make build

# Build the minimal Docker container image
make container
```

### Local Simulation (Development)
If you have `vcsim` and `govc` from the govmomi tools installed, you can spin up a local vCenter simulator and stream mocked events:
```bash
make simulate
```

## Kubernetes Deployment Example

Because the container is completely stateless, you can effortlessly deploy it in a Kubernetes cluster using a standard deployment, mounting custom CAs via a Secret if required:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vsphere-eventlogger
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vsphere-eventlogger
  template:
    metadata:
      labels:
        app: vsphere-eventlogger
    spec:
      containers:
      - name: logger
        image: your-registry/vsphere-eventlogger:latest
        env:
        - name: GOVMOMI_URL
          value: "https://vcenter.local/sdk"
        - name: GOVMOMI_USERNAME
          valueFrom:
            secretKeyRef:
              name: vcenter-credentials
              key: username
        - name: GOVMOMI_PASSWORD
          valueFrom:
            secretKeyRef:
              name: vcenter-credentials
              key: password
        # Optional: Redact usernames for compliance
        - name: REDACT_USERNAME
          value: "true"
        # Optional: Inject your company's CA certificate
        - name: GOVMOMI_TLS_CA_CERTS
          value: "/etc/ssl/vcenter/ca.crt"
        volumeMounts:
        - name: ca-cert
          mountPath: /etc/ssl/vcenter
          readOnly: true
      volumes:
      - name: ca-cert
        secret:
          secretName: vcenter-ca-secret
```

### Using Helm

A Helm chart is available to easily deploy the application, published as an OCI artifact to GitHub Container Registry:

```bash
helm install my-logger oci://ghcr.io/lucadev/charts/vsphere-eventlogger \
  --version 0.1.0 \
  --set govmomi.url="https://vcenter.local/sdk" \
  --set govmomi.username="Administrator@vsphere.local" \
  --set govmomi.password="Secret123!"
```

Alternatively, using an existing secret for credentials:

```bash
helm install my-logger oci://ghcr.io/lucadev/charts/vsphere-eventlogger \
  --version 0.1.0 \
  --set govmomi.url="https://vcenter.local/sdk" \
  --set govmomi.existingSecret="vcenter-credentials"
```