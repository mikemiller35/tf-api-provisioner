package status

import "time"

type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
)

type Status struct {
	JobID        string            `json:"jobId"`
	CustomerID   string            `json:"customerId"`
	ProductCode  string            `json:"productCode"`
	CompanyName  string            `json:"companyName"`
	ContactEmail string            `json:"contactEmail"`
	State        State             `json:"state"`
	StateKey     string            `json:"stateKey"`
	StartedAt    time.Time         `json:"startedAt"`
	EndedAt      *time.Time        `json:"endedAt,omitempty"`
	Error        string            `json:"error,omitempty"`
	Outputs      map[string]string `json:"outputs,omitempty"`
}

// StatusKey returns the S3 key where this status object lives.
func StatusKey(customerID, productCode string) string {
	return "customers/" + customerID + "/" + productCode + ".status.json"
}

// StateKey returns the S3 key where the Terraform state for this tuple lives.
func StateKey(customerID, productCode string) string {
	return "customers/" + customerID + "/" + productCode + ".tfstate"
}

// CustomerPrefix returns the S3 key prefix to list all statuses for a customer.
func CustomerPrefix(customerID string) string {
	return "customers/" + customerID + "/"
}
