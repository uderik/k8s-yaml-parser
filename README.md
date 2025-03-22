# YAML Parser

A command-line tool that splits multi-document YAML files into separate files, with options to organize by kind, service, or flat structure.

## Features

- Parse multi-document YAML files (like Kubernetes manifests)
- Save each manifest to a separate file with configurable naming and directory structure
- Support for input from file or stdin (pipe)
- Remove unwanted sections using regular expressions
- Organize files by:
  - Flat structure with `kind-name.yaml` filenames
  - Group by kind in directories
  - Group by service in directories (based on labels and naming patterns)

## Installation

### Prerequisites

- Go 1.16 or higher

### Build from source

```bash
git clone <repository-url>
cd scripts
go build -o yaml_parser yaml_parser.go
```

For convenience, you can add the binary to your PATH:

```bash
sudo mv yaml_parser /usr/local/bin/
```

## Usage

```
yaml_parser --file=input.yaml --outdir=./output
```

Or using stdin:

```
cat input.yaml | yaml_parser --outdir=./output
```

### Parameters

```
--file      Input YAML file path (if not specified, stdin will be used)
--outdir    Output directory for parsed manifests (required)
--remove    Patterns to remove from each manifest (regex, comma-separated)
--format    Output filename format:
            'kind-name'   - Flat structure with kind-name.yaml files (default)
            'kind/name'   - Group by kind in directories 
            'service'     - Group by service in directories
--help      Show usage information
```

### Examples

Parse a Kubernetes manifest and save to files:

```bash
yaml_parser --file=manifest.yaml --outdir=./manifests
```

Group manifests by kind:

```bash
yaml_parser --file=manifest.yaml --outdir=./manifests --format=kind/name
```

Group manifests by service:

```bash
yaml_parser --file=manifest.yaml --outdir=./manifests --format=service
```

Remove status and generation fields:

```bash
yaml_parser --file=manifest.yaml --outdir=./manifests --remove="status:.*,generation:.*"
```

Read from stdin (pipe):

```bash
kubectl get all -o yaml | yaml_parser --outdir=./manifests
```

## Service Detection

When using the `service` format, resources are grouped by service name, which is determined by:

1. Common app labels (`app`, `app.kubernetes.io/name`, `k8s-app`)
2. Selector labels for services and deployments
3. Pod template labels for workloads
4. Resource name patterns (e.g., "frontend-deployment" → "frontend")
5. If no service can be determined, resources are placed in a "common" directory

## Output Examples

### Flat structure (default)

```
./manifests/
  ├── deployment-nginx.yaml
  ├── service-nginx.yaml
  └── configmap-nginx-config.yaml
```

### Kind-based organization

```
./manifests/
  ├── deployment/
  │   └── nginx.yaml
  ├── service/
  │   └── nginx.yaml
  └── configmap/
      └── nginx-config.yaml
```

### Service-based organization

```
./manifests/
  ├── nginx/
  │   ├── deployment-nginx.yaml
  │   ├── service-nginx.yaml
  │   └── configmap-nginx-config.yaml
  └── common/
      └── namespace-default.yaml
```

## License

MIT 