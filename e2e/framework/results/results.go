package results

import "time"

// Status indicates outcome for a test or step.
type Status string

const (
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// StepResult captures a single step execution.
type StepResult struct {
	Name      string            `json:"name"`
	Action    string            `json:"action"`
	Status    Status            `json:"status"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	Duration  time.Duration     `json:"duration"`
	Error     string            `json:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// AssertionResult captures a single assertion execution.
type AssertionResult struct {
	Name     string        `json:"name"`
	Status   Status        `json:"status"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// TestResult captures a test execution summary.
type TestResult struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Tags        []string           `json:"tags,omitempty"`
	Status      Status             `json:"status"`
	StartTime   time.Time          `json:"start_time"`
	EndTime     time.Time          `json:"end_time"`
	Duration    time.Duration      `json:"duration"`
	Steps       []StepResult        `json:"steps"`
	Assertions  []AssertionResult   `json:"assertions"`
	Artifacts   map[string]string   `json:"artifacts,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
	Requires    []string           `json:"requires,omitempty"`
}

// RunResult captures the overall run summary.
type RunResult struct {
	RunID     string       `json:"run_id"`
	StartTime time.Time    `json:"start_time"`
	EndTime   time.Time    `json:"end_time"`
	Duration  time.Duration `json:"duration"`
	Tests     []TestResult `json:"tests"`
}
