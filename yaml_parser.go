package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// K8sResource represents the minimal structure needed to identify a Kubernetes resource
type K8sResource struct {
	ApiVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template struct {
			Metadata struct {
				Labels map[string]string `yaml:"labels"`
			} `yaml:"metadata"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

// parseServiceName attempts to extract a service name from a resource
func parseServiceName(resource *K8sResource) string {
	// Check for common app labels
	appLabels := []string{"app", "app.kubernetes.io/name", "k8s-app"}

	// First check metadata labels
	if resource.Metadata.Labels != nil {
		for _, label := range appLabels {
			if value, exists := resource.Metadata.Labels[label]; exists && value != "" {
				return value
			}
		}
	}

	// Then check selector labels in deployments, services, etc.
	if resource.Spec.Selector.MatchLabels != nil {
		for _, label := range appLabels {
			if value, exists := resource.Spec.Selector.MatchLabels[label]; exists && value != "" {
				return value
			}
		}
		// For services, "app" might be the most common selector
		if app, exists := resource.Spec.Selector.MatchLabels["app"]; exists && app != "" {
			return app
		}
	}

	// Then check pod template labels in deployments, statefulsets, etc.
	if resource.Spec.Template.Metadata.Labels != nil {
		for _, label := range appLabels {
			if value, exists := resource.Spec.Template.Metadata.Labels[label]; exists && value != "" {
				return value
			}
		}
	}

	// For CRDs and other resources, sometimes the name is a good indicator
	// Often resource names have a pattern like "servicename-resourcetype"
	if resource.Metadata.Name != "" {
		// For resources that likely belong to a service, extract the service name part
		if resource.Kind != "Namespace" && resource.Kind != "ClusterRole" &&
			resource.Kind != "ClusterRoleBinding" && !strings.HasPrefix(resource.Kind, "Cluster") {
			parts := strings.Split(resource.Metadata.Name, "-")
			if len(parts) > 1 {
				return parts[0] // First part is often the service name
			}
		}
	}

	// If we can't determine the service, default to "common" or "other"
	return "common"
}

// printUsage prints the usage help message
func printUsage() {
	fmt.Println("YAML Parser - splits multi-document YAML into separate files")
	fmt.Println("\nUsage:")
	fmt.Println("  cat file.yaml | yaml_parser --outdir=./output")
	fmt.Println("  yaml_parser --file=input.yaml --outdir=./output")
	fmt.Println("\nParameters:")
	fmt.Println("  --file      Input YAML file path (if not specified, stdin will be used)")
	fmt.Println("  --outdir    Output directory for parsed manifests (required)")
	fmt.Println("  --remove    Patterns to remove from each manifest (regex, comma-separated)")
	fmt.Println("  --format    Output filename format:")
	fmt.Println("              'kind-name'   - Flat structure with kind-name.yaml files (default)")
	fmt.Println("              'kind/name'   - Group by kind in directories")
	fmt.Println("              'service'     - Group by service in directories")
	fmt.Println("\nExamples:")
	fmt.Println("  yaml_parser --file=1.yaml --outdir=./manifests")
	fmt.Println("  yaml_parser --file=1.yaml --outdir=./manifests --remove=\"status:.*,generation:.*\"")
	fmt.Println("  yaml_parser --file=1.yaml --outdir=./manifests --format=kind/name")
	fmt.Println("  yaml_parser --file=1.yaml --outdir=./manifests --format=service")
	fmt.Println("  cat 1.yaml | yaml_parser --outdir=./manifests")
	fmt.Println("")
}

func main() {
	// Define command line flags
	inputFile := flag.String("file", "", "Input YAML file path (if not specified, stdin will be used)")
	outputDir := flag.String("outdir", "", "Output directory for parsed manifests")
	removePatterns := flag.String("remove", "", "Comma-separated patterns to remove from each manifest")
	format := flag.String("format", "kind-name", "Output filename format: 'kind-name', 'kind/name', or 'service'")
	help := flag.Bool("help", false, "Show usage information")
	flag.Parse()

	// Show help if requested or no arguments provided
	if *help || (flag.NFlag() == 0 && len(os.Args) == 1) {
		printUsage()
		return
	}

	// Validate required parameters
	if *outputDir == "" {
		log.Fatalf("Output directory must be specified (--outdir)")
	}

	// Validate format parameter
	validFormats := map[string]bool{
		"kind-name": true,
		"kind/name": true,
		"service":   true,
	}
	if !validFormats[*format] {
		log.Fatalf("Invalid format option: %s. Must be 'kind-name', 'kind/name', or 'service'", *format)
	}

	// Prepare input source
	var input io.ReadCloser
	if *inputFile != "" {
		// Read from file
		file, err := os.Open(*inputFile)
		if err != nil {
			log.Fatalf("Error opening YAML file: %v", err)
		}
		defer file.Close()
		input = file
	} else {
		// Read from stdin
		input = os.Stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			// Stdin is a terminal, not a pipe
			log.Println("No input file specified and no data piped in.")
			printUsage()
			os.Exit(1)
		}
		log.Printf("Reading YAML from stdin...")
	}

	// Create the output directory if it doesn't exist
	if err := os.RemoveAll(*outputDir); err != nil {
		log.Fatalf("Error removing previous output directory: %v", err)
	}
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	// Prepare patterns to remove
	var patterns []*regexp.Regexp
	if *removePatterns != "" {
		for _, pattern := range strings.Split(*removePatterns, ",") {
			re, err := regexp.Compile(pattern)
			if err != nil {
				log.Fatalf("Invalid regex pattern '%s': %v", pattern, err)
			}
			patterns = append(patterns, re)
		}
	}

	// Create a YAML decoder
	decoder := yaml.NewDecoder(input)

	// Count of successfully parsed manifests
	count := 0

	// Read and process each document
	for i := 1; ; i++ {
		// Read the whole document into a yaml.Node
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err.Error() == "EOF" {
				break // End of file
			}
			log.Printf("Error parsing document %d: %v", i, err)
			continue
		}

		// Extract the resource identification (apiVersion, kind, name)
		var resource K8sResource
		if err := node.Decode(&resource); err != nil {
			log.Printf("Error extracting resource info from document %d: %v", i, err)
			continue
		}

		// Skip if Kind or Name is empty
		if resource.Kind == "" || resource.Metadata.Name == "" {
			log.Printf("Document %d has empty Kind or Name, skipping", i)
			continue
		}

		var filePath string

		if *format == "kind-name" {
			// Create filename: kind-name.yaml in lowercase
			filename := strings.ToLower(fmt.Sprintf("%s-%s.yaml", resource.Kind, resource.Metadata.Name))
			filePath = filepath.Join(*outputDir, filename)
		} else if *format == "kind/name" {
			// Create directory based on Kind
			kindDir := strings.ToLower(resource.Kind)
			kindDirPath := filepath.Join(*outputDir, kindDir)
			if err := os.MkdirAll(kindDirPath, 0755); err != nil {
				log.Printf("Error creating directory for kind %s: %v", kindDir, err)
				continue
			}

			// Use the original name (preserving case)
			filename := fmt.Sprintf("%s.yaml", resource.Metadata.Name)
			filePath = filepath.Join(kindDirPath, filename)
		} else if *format == "service" {
			// Determine the service name
			serviceName := parseServiceName(&resource)

			// Create a directory for the service
			serviceDirPath := filepath.Join(*outputDir, serviceName)
			if err := os.MkdirAll(serviceDirPath, 0755); err != nil {
				log.Printf("Error creating directory for service %s: %v", serviceName, err)
				continue
			}

			// Use kind-name for the filename to avoid conflicts
			filename := strings.ToLower(fmt.Sprintf("%s-%s.yaml", resource.Kind, resource.Metadata.Name))
			filePath = filepath.Join(serviceDirPath, filename)
		}

		// Re-encode the entire document as clean YAML
		var buf bytes.Buffer
		encoder := yaml.NewEncoder(&buf)
		encoder.SetIndent(2) // Set indentation level
		if err := encoder.Encode(&node); err != nil {
			log.Printf("Error encoding document %d: %v", i, err)
			continue
		}
		if err := encoder.Close(); err != nil {
			log.Printf("Error closing encoder for document %d: %v", i, err)
			continue
		}

		// Get the encoded YAML as string
		yamlContent := buf.String()

		// Apply removal patterns if specified
		if len(patterns) > 0 {
			for _, pattern := range patterns {
				yamlContent = pattern.ReplaceAllString(yamlContent, "")
			}
			// Remove any empty lines that might have been created
			yamlContent = regexp.MustCompile(`(?m)^\s*\n`).ReplaceAllString(yamlContent, "")
		}

		// Save the document to a file
		if err := os.WriteFile(filePath, []byte(yamlContent), 0644); err != nil {
			log.Printf("Error writing file %s: %v", filePath, err)
			continue
		}

		count++
		fmt.Printf("Saved document to %s\n", filePath)
	}

	fmt.Printf("Parsing complete! Saved %d manifests.\n", count)
}
