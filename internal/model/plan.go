package model

import "time"

const PlanInputSchema = "kubecompat.io/plan-input/v1alpha1"
const PlanSchema = "kubecompat.io/upgrade-plan/v1alpha1"

type OperatorReceipt struct {
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	Owner      string    `json:"owner"`
	ObservedAt time.Time `json:"observedAt"`
	ValidUntil time.Time `json:"validUntil,omitempty"`
	EvidenceID string    `json:"evidenceId"`
	Notes      string    `json:"notes,omitempty"`
}

type PlanInput struct {
	SchemaVersion string            `json:"schemaVersion"`
	ClusterID     string            `json:"clusterId"`
	SourceRelease string            `json:"sourceRelease"`
	ObservedAt    time.Time         `json:"observedAt"`
	Releases      map[string]string `json:"releases,omitempty"`
	Hops          []HopInput        `json:"hops,omitempty"`
}

type HopInput struct {
	FromRelease     string            `json:"fromRelease"`
	ToRelease       string            `json:"toRelease"`
	ClusterID       string            `json:"clusterId"`
	InventoryFile   string            `json:"inventoryFile,omitempty"`
	InventorySHA256 string            `json:"inventorySHA256,omitempty"`
	ObservedAt      time.Time         `json:"observedAt"`
	ValidUntil      time.Time         `json:"validUntil,omitempty"`
	EvidenceOwner   string            `json:"evidenceOwner,omitempty"`
	Datastore       string            `json:"datastore,omitempty"`
	Servers         []string          `json:"servers,omitempty"`
	Order           []string          `json:"order,omitempty"`
	Receipts        []OperatorReceipt `json:"receipts,omitempty"`
	Inventory       *Inventory        `json:"-"`
}

type PlanStep struct {
	ID            string   `json:"id"`
	Scope         string   `json:"scope"`
	Description   string   `json:"description"`
	EvidenceIDs   []string `json:"evidenceIds,omitempty"`
	StopCondition string   `json:"stopCondition"`
}

type PlanHop struct {
	ID                 string     `json:"id"`
	FromRelease        string     `json:"fromRelease,omitempty"`
	ToRelease          string     `json:"toRelease,omitempty"`
	CompatibilityMinor string     `json:"compatibilityMinor"`
	ResolvedTarget     string     `json:"resolvedTarget"`
	DependsOn          string     `json:"dependsOn,omitempty"`
	InventorySHA256    string     `json:"inventorySHA256,omitempty"`
	ObservedAt         time.Time  `json:"observedAt,omitempty"`
	Assessment         Assessment `json:"assessment"`
	Steps              []PlanStep `json:"steps"`
	StopConditions     []string   `json:"stopConditions"`
}

type UpgradePlan struct {
	SchemaVersion    string            `json:"schemaVersion"`
	AssessedAt       time.Time         `json:"assessedAt"`
	Distribution     string            `json:"distribution"`
	ClusterID        string            `json:"clusterId"`
	FromKubernetes   string            `json:"fromKubernetes"`
	TargetKubernetes string            `json:"targetKubernetes"`
	SourceRelease    string            `json:"sourceRelease,omitempty"`
	InputSHA256      string            `json:"inputSHA256,omitempty"`
	PolicyRevision   string            `json:"policyRevision"`
	PolicySources    map[string]string `json:"policySources"`
	Hops             []PlanHop         `json:"hops"`
	Decision         string            `json:"decision"`
	ExitCode         int               `json:"exitCode"`
	Omissions        []string          `json:"omissions"`
}
