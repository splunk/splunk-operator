package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/splunk/splunk-operator/e2e/framework/graph"
)

var (
	neo4jURI      string
	neo4jUser     string
	neo4jPassword string
	neo4jDatabase string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "e2e-query",
		Short: "Query E2E test knowledge graph",
		Long:  `CLI tool to query and analyze E2E test results stored in Neo4j`,
	}

	// Add persistent flags
	rootCmd.PersistentFlags().StringVar(&neo4jURI, "neo4j-uri", os.Getenv("E2E_NEO4J_URI"), "Neo4j connection URI")
	rootCmd.PersistentFlags().StringVar(&neo4jUser, "neo4j-user", os.Getenv("E2E_NEO4J_USER"), "Neo4j username")
	rootCmd.PersistentFlags().StringVar(&neo4jPassword, "neo4j-password", os.Getenv("E2E_NEO4J_PASSWORD"), "Neo4j password")
	rootCmd.PersistentFlags().StringVar(&neo4jDatabase, "neo4j-database", getEnvOrDefault("E2E_NEO4J_DATABASE", "neo4j"), "Neo4j database name")

	rootCmd.AddCommand(
		newSimilarFailuresCmd(),
		newResolutionsCmd(),
		newUntestedCmd(),
		newSuccessRateCmd(),
		newFlakyTestsCmd(),
		newTimingsCmd(),
		newErrorPatternCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newSimilarFailuresCmd() *cobra.Command {
	var errorCategory string
	var limit int

	cmd := &cobra.Command{
		Use:   "similar-failures",
		Short: "Find tests with similar failures",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			failures, err := qi.FindSimilarFailures(ctx, errorCategory, limit)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(failures) == 0 {
				fmt.Printf("No failures found for category: %s\n", errorCategory)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TEST NAME\tSTATUS\tOPERATOR IMAGE\tSPLUNK IMAGE\tCLUSTER")
			for _, f := range failures {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					f.TestName, f.Status, f.OperatorImage, f.SplunkImage, f.ClusterProvider)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().StringVar(&errorCategory, "category", "", "Error category to search for (required)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")
	cmd.MarkFlagRequired("category")

	return cmd
}

func newResolutionsCmd() *cobra.Command {
	var errorCategory string

	cmd := &cobra.Command{
		Use:   "resolutions",
		Short: "Find documented resolutions for an error",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			resolutions, err := qi.FindResolutionsForError(ctx, errorCategory)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(resolutions) == 0 {
				fmt.Printf("No resolutions found for category: %s\n", errorCategory)
				return nil
			}

			for i, res := range resolutions {
				fmt.Printf("\n--- Resolution %d ---\n", i+1)
				data, _ := json.MarshalIndent(res, "", "  ")
				fmt.Println(string(data))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&errorCategory, "category", "", "Error category to search for (required)")
	cmd.MarkFlagRequired("category")

	return cmd
}

func newUntestedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "untested",
		Short: "Find untested combinations of versions and providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			combinations, err := qi.FindUntestedCombinations(ctx)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(combinations) == 0 {
				fmt.Println("All combinations have been tested!")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SPLUNK IMAGE\tCLUSTER PROVIDER")
			for _, c := range combinations {
				fmt.Fprintf(w, "%s\t%s\n", c["splunk_image"], c["cluster_provider"])
			}
			w.Flush()

			return nil
		},
	}

	return cmd
}

func newSuccessRateCmd() *cobra.Command {
	var topology string
	var cluster string

	cmd := &cobra.Command{
		Use:   "success-rate",
		Short: "Calculate test success rate by filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			filters := make(map[string]string)
			if topology != "" {
				filters["topology"] = topology
			}
			if cluster != "" {
				filters["cluster"] = cluster
			}

			stats, err := qi.GetTestSuccessRate(ctx, filters)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			fmt.Println("\n=== Test Success Rate ===")
			fmt.Printf("Total:        %v\n", stats["total"])
			fmt.Printf("Passed:       %v\n", stats["passed"])
			fmt.Printf("Failed:       %v\n", stats["failed"])
			fmt.Printf("Skipped:      %v\n", stats["skipped"])
			if rate, ok := stats["success_rate"]; ok {
				fmt.Printf("Success Rate: %.2f%%\n", rate)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&topology, "topology", "", "Filter by topology (s1, c3, m4)")
	cmd.Flags().StringVar(&cluster, "cluster", "", "Filter by cluster provider (eks, gke, aks)")

	return cmd
}

func newFlakyTestsCmd() *cobra.Command {
	var threshold float64

	cmd := &cobra.Command{
		Use:   "flaky-tests",
		Short: "Find tests with inconsistent pass/fail patterns",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			tests, err := qi.FindFlakyTests(ctx, threshold)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(tests) == 0 {
				fmt.Println("No flaky tests found!")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TEST NAME\tPASSED\tFAILED\tTOTAL\tPASS RATE")
			for _, t := range tests {
				fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%.2f%%\n",
					t["test_name"], t["passed"], t["failed"], t["total"], t["pass_rate"].(float64)*100)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().Float64Var(&threshold, "threshold", 0.2, "Threshold for flakiness (0.2 = 20%)")

	return cmd
}

func newTimingsCmd() *cobra.Command {
	var topology string

	cmd := &cobra.Command{
		Use:   "timings",
		Short: "Get average timing metrics for a topology",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			timings, err := qi.GetAverageTimings(ctx, topology)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(timings) == 0 {
				fmt.Printf("No timing data found for topology: %s\n", topology)
				return nil
			}

			fmt.Printf("\n=== Average Timings for %s ===\n", topology)
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "METRIC\tAVG TIME (seconds)")
			for metric, avgTime := range timings {
				fmt.Fprintf(w, "%s\t%.2f\n", metric, avgTime)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().StringVar(&topology, "topology", "", "Topology to query (required)")
	cmd.MarkFlagRequired("topology")

	return cmd
}

func newErrorPatternCmd() *cobra.Command {
	var pattern string

	cmd := &cobra.Command{
		Use:   "error-pattern",
		Short: "Find tests matching an error pattern (regex)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			qi, err := graph.NewQueryInterface(neo4jURI, neo4jUser, neo4jPassword, neo4jDatabase)
			if err != nil {
				return fmt.Errorf("failed to connect to Neo4j: %w", err)
			}
			defer qi.Close(ctx)

			failures, err := qi.FindTestsByErrorPattern(ctx, pattern)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(failures) == 0 {
				fmt.Printf("No tests found matching pattern: %s\n", pattern)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TEST NAME\tERROR CATEGORY\tOPERATOR IMAGE\tCLUSTER")
			for _, f := range failures {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					f.TestName, f.ErrorCategory, f.OperatorImage, f.ClusterProvider)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().StringVar(&pattern, "pattern", "", "Error message pattern (regex) (required)")
	cmd.MarkFlagRequired("pattern")

	return cmd
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
