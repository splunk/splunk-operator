package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/splunk/splunk-operator/e2e/framework/matrix"
	"gopkg.in/yaml.v3"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "e2e-matrix",
		Short: "Generate E2E test specs from test matrices",
		Long:  `Tool to generate test specifications from matrix definitions`,
	}

	rootCmd.AddCommand(
		newGenerateCmd(),
		newReportCmd(),
		newValidateCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newGenerateCmd() *cobra.Command {
	var matrixFile string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate test specs from a matrix file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load matrix
			data, err := os.ReadFile(matrixFile)
			if err != nil {
				return fmt.Errorf("failed to read matrix file: %w", err)
			}

			var m matrix.Matrix
			if err := yaml.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("failed to parse matrix: %w", err)
			}

			// Generate specs
			gen := matrix.NewGenerator(&m)
			specs, err := gen.Generate()
			if err != nil {
				return fmt.Errorf("failed to generate specs: %w", err)
			}

			// Format as YAML
			var output []byte
			for i, testSpec := range specs {
				specData, err := yaml.Marshal(testSpec)
				if err != nil {
					return fmt.Errorf("failed to marshal spec: %w", err)
				}

				if i > 0 {
					output = append(output, []byte("---\n")...)
				}
				output = append(output, specData...)
			}

			// Write output
			if outputFile == "" || outputFile == "-" {
				fmt.Println(string(output))
			} else {
				if err := os.WriteFile(outputFile, output, 0644); err != nil {
					return fmt.Errorf("failed to write output: %w", err)
				}
				fmt.Printf("Generated %d test specs to %s\n", len(specs), outputFile)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&matrixFile, "matrix", "m", "", "Matrix file path (required)")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (default: stdout)")
	cmd.MarkFlagRequired("matrix")

	return cmd
}

func newReportCmd() *cobra.Command {
	var matrixFile string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a report of test matrix combinations",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load matrix
			data, err := os.ReadFile(matrixFile)
			if err != nil {
				return fmt.Errorf("failed to read matrix file: %w", err)
			}

			var m matrix.Matrix
			if err := yaml.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("failed to parse matrix: %w", err)
			}

			// Generate report
			gen := matrix.NewGenerator(&m)
			report := gen.GenerateReport()
			fmt.Println(report)

			return nil
		},
	}

	cmd.Flags().StringVarP(&matrixFile, "matrix", "m", "", "Matrix file path (required)")
	cmd.MarkFlagRequired("matrix")

	return cmd
}

func newValidateCmd() *cobra.Command {
	var matrixFile string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a matrix file",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load matrix
			data, err := os.ReadFile(matrixFile)
			if err != nil {
				return fmt.Errorf("failed to read matrix file: %w", err)
			}

			var m matrix.Matrix
			if err := yaml.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("failed to parse matrix: %w", err)
			}

			// Validate matrix
			if err := validateMatrix(&m); err != nil {
				return fmt.Errorf("matrix validation failed: %w", err)
			}

			fmt.Println("✓ Matrix file is valid")
			return nil
		},
	}

	cmd.Flags().StringVarP(&matrixFile, "matrix", "m", "", "Matrix file path (required)")
	cmd.MarkFlagRequired("matrix")

	return cmd
}

func validateMatrix(m *matrix.Matrix) error {
	if m.Name == "" {
		return fmt.Errorf("matrix name is required")
	}

	if len(m.Topologies) == 0 {
		return fmt.Errorf("at least one topology is required")
	}

	if len(m.Scenarios) == 0 {
		return fmt.Errorf("at least one scenario is required")
	}

	// Validate scenarios
	for i, scenario := range m.Scenarios {
		if scenario.Name == "" {
			return fmt.Errorf("scenario %d: name is required", i)
		}
		if len(scenario.Steps) == 0 {
			return fmt.Errorf("scenario %s: at least one step is required", scenario.Name)
		}
	}

	return nil
}
