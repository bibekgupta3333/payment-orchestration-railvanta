package storage

// SchemaMigration maps explicitly to schema_migrations; migrations own constraints.
type SchemaMigration struct {
	Version   int    `gorm:"column:version;primaryKey"`
	Name      string `gorm:"column:name"`
	Checksum  string `gorm:"column:checksum"`
	AppliedAt int64  `gorm:"column:applied_at"`
}

func (SchemaMigration) TableName() string { return "schema_migrations" }

// Workspace maps explicitly to workspaces; migrations own constraints.
type Workspace struct {
	ID          string `gorm:"column:id;primaryKey"`
	Name        string `gorm:"column:name"`
	Environment string `gorm:"column:environment"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (Workspace) TableName() string { return "workspaces" }

// Provider maps explicitly to providers; migrations own constraints.
type Provider struct {
	ID          string `gorm:"column:id;primaryKey"`
	WorkspaceID string `gorm:"column:workspace_id"`
	Code        string `gorm:"column:code"`
	Enabled     int64  `gorm:"column:enabled"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (Provider) TableName() string { return "providers" }

// PaymentMethod maps explicitly to payment_methods; migrations own constraints.
type PaymentMethod struct {
	ID          string  `gorm:"column:id;primaryKey"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	Brand       *string `gorm:"column:brand"`
	LastFour    *string `gorm:"column:last_four"`
	Rail        string  `gorm:"column:rail"`
	CreatedAt   int64   `gorm:"column:created_at;autoCreateTime:milli"`
}

func (PaymentMethod) TableName() string { return "payment_methods" }

// PaymentMethodToken maps explicitly to payment_method_tokens; migrations own constraints.
type PaymentMethodToken struct {
	ID             string `gorm:"column:id;primaryKey"`
	WorkspaceID    string `gorm:"column:workspace_id"`
	MethodID       string `gorm:"column:method_id"`
	ProviderID     string `gorm:"column:provider_id"`
	TokenReference string `gorm:"column:token_reference"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (PaymentMethodToken) TableName() string { return "payment_method_tokens" }

// RoutingRule maps explicitly to routing_rules; migrations own constraints.
type RoutingRule struct {
	ID             string `gorm:"column:id;primaryKey"`
	WorkspaceID    string `gorm:"column:workspace_id"`
	ProviderID     string `gorm:"column:provider_id"`
	Rail           string `gorm:"column:rail"`
	Currency       string `gorm:"column:currency"`
	Priority       int64  `gorm:"column:priority"`
	MinAmountMinor int64  `gorm:"column:min_amount_minor"`
	MaxAmountMinor *int64 `gorm:"column:max_amount_minor"`
	Enabled        int64  `gorm:"column:enabled"`
	Version        int64  `gorm:"column:version"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt      int64  `gorm:"column:updated_at;autoUpdateTime:milli"`
}

func (RoutingRule) TableName() string { return "routing_rules" }

// Payment maps explicitly to payments; migrations own constraints.
type Payment struct {
	ID                string  `gorm:"column:id;primaryKey"`
	WorkspaceID       string  `gorm:"column:workspace_id"`
	PaymentMethodID   string  `gorm:"column:payment_method_id"`
	AmountMinor       int64   `gorm:"column:amount_minor"`
	Currency          string  `gorm:"column:currency"`
	MerchantReference *string `gorm:"column:merchant_reference"`
	Status            string  `gorm:"column:status"`
	Version           int64   `gorm:"column:version"`
	CreatedAt         int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt         int64   `gorm:"column:updated_at;autoUpdateTime:milli"`
}

func (Payment) TableName() string { return "payments" }

// PaymentAttempt maps explicitly to payment_attempts; migrations own constraints.
type PaymentAttempt struct {
	ID                     string  `gorm:"column:id;primaryKey"`
	WorkspaceID            string  `gorm:"column:workspace_id"`
	PaymentID              string  `gorm:"column:payment_id"`
	ProviderID             string  `gorm:"column:provider_id"`
	RoutingRuleID          *string `gorm:"column:routing_rule_id"`
	RoutingReason          string  `gorm:"column:routing_reason"`
	AttemptNumber          int64   `gorm:"column:attempt_number"`
	ProviderIdempotencyKey string  `gorm:"column:provider_idempotency_key"`
	ProviderReference      *string `gorm:"column:provider_reference"`
	Status                 string  `gorm:"column:status"`
	ErrorCode              *string `gorm:"column:error_code"`
	SubmittedAt            *int64  `gorm:"column:submitted_at"`
	CompletedAt            *int64  `gorm:"column:completed_at"`
	CreatedAt              int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt              int64   `gorm:"column:updated_at;autoUpdateTime:milli"`
}

func (PaymentAttempt) TableName() string { return "payment_attempts" }

// PaymentStateTransition maps explicitly to payment_state_transitions; migrations own constraints.
type PaymentStateTransition struct {
	ID             string  `gorm:"column:id;primaryKey"`
	WorkspaceID    string  `gorm:"column:workspace_id"`
	PaymentID      string  `gorm:"column:payment_id"`
	FromStatus     *string `gorm:"column:from_status"`
	ToStatus       string  `gorm:"column:to_status"`
	Reason         string  `gorm:"column:reason"`
	ActorReference string  `gorm:"column:actor_reference"`
	CreatedAt      int64   `gorm:"column:created_at;autoCreateTime:milli"`
}

func (PaymentStateTransition) TableName() string { return "payment_state_transitions" }

// IdempotencyRecord maps explicitly to idempotency_records; migrations own constraints.
type IdempotencyRecord struct {
	ID             string  `gorm:"column:id;primaryKey"`
	WorkspaceID    string  `gorm:"column:workspace_id"`
	Operation      string  `gorm:"column:operation"`
	Key            string  `gorm:"column:key"`
	RequestHash    string  `gorm:"column:request_hash"`
	PaymentID      *string `gorm:"column:payment_id"`
	Status         string  `gorm:"column:status"`
	ResponseStatus *int64  `gorm:"column:response_status"`
	ResponseBody   *string `gorm:"column:response_body"`
	CreatedAt      int64   `gorm:"column:created_at;autoCreateTime:milli"`
	CompletedAt    *int64  `gorm:"column:completed_at"`
}

func (IdempotencyRecord) TableName() string { return "idempotency_records" }

// LedgerAccount maps explicitly to ledger_accounts; migrations own constraints.
type LedgerAccount struct {
	ID          string  `gorm:"column:id;primaryKey"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	Code        string  `gorm:"column:code"`
	Currency    string  `gorm:"column:currency"`
	ProviderID  *string `gorm:"column:provider_id"`
	Kind        string  `gorm:"column:kind"`
	CreatedAt   int64   `gorm:"column:created_at;autoCreateTime:milli"`
}

func (LedgerAccount) TableName() string { return "ledger_accounts" }

// LedgerJournal maps explicitly to ledger_journals; migrations own constraints.
type LedgerJournal struct {
	ID          string  `gorm:"column:id;primaryKey"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	PaymentID   *string `gorm:"column:payment_id"`
	Currency    string  `gorm:"column:currency"`
	PostingKey  string  `gorm:"column:posting_key"`
	Status      string  `gorm:"column:status"`
	ReversalOf  *string `gorm:"column:reversal_of"`
	CreatedAt   int64   `gorm:"column:created_at;autoCreateTime:milli"`
	PostedAt    *int64  `gorm:"column:posted_at"`
}

func (LedgerJournal) TableName() string { return "ledger_journals" }

// LedgerEntry maps explicitly to ledger_entries; migrations own constraints.
type LedgerEntry struct {
	ID          string `gorm:"column:id;primaryKey"`
	WorkspaceID string `gorm:"column:workspace_id"`
	JournalID   string `gorm:"column:journal_id"`
	AccountID   string `gorm:"column:account_id"`
	Currency    string `gorm:"column:currency"`
	Side        string `gorm:"column:side"`
	AmountMinor int64  `gorm:"column:amount_minor"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (LedgerEntry) TableName() string { return "ledger_entries" }

// WebhookInbox maps explicitly to webhook_inbox; migrations own constraints.
type WebhookInbox struct {
	ID              string  `gorm:"column:id;primaryKey"`
	WorkspaceID     string  `gorm:"column:workspace_id"`
	ProviderID      string  `gorm:"column:provider_id"`
	ProviderEventID string  `gorm:"column:provider_event_id"`
	PayloadHash     string  `gorm:"column:payload_hash"`
	Payload         string  `gorm:"column:payload"`
	Status          string  `gorm:"column:status"`
	LeaseOwner      *string `gorm:"column:lease_owner"`
	LeaseUntil      *int64  `gorm:"column:lease_until"`
	DuplicateCount  int64   `gorm:"column:duplicate_count"`
	LastReceivedAt  int64   `gorm:"column:last_received_at"`
	ReceivedAt      int64   `gorm:"column:received_at"`
	ProcessedAt     *int64  `gorm:"column:processed_at"`

	// Added by migration 0005.
	EventType              string  `gorm:"column:event_type"`
	PaymentID              *string `gorm:"column:payment_id"`
	AttemptID              *string `gorm:"column:attempt_id"`
	SignatureStatus        string  `gorm:"column:signature_status"`
	Outcome                *string `gorm:"column:outcome"`
	OutcomeReason          *string `gorm:"column:outcome_reason"`
	InvestigationFlag      int64   `gorm:"column:investigation_flag"`
	ConflictingPayloadHash *string `gorm:"column:conflicting_payload_hash"`
	ConflictCount          int64   `gorm:"column:conflict_count"`
}

func (WebhookInbox) TableName() string { return "webhook_inbox" }

// OutboxEvent maps explicitly to outbox_events; migrations own constraints.
type OutboxEvent struct {
	ID               string  `gorm:"column:id;primaryKey"`
	WorkspaceID      string  `gorm:"column:workspace_id"`
	PaymentID        *string `gorm:"column:payment_id"`
	EventType        string  `gorm:"column:event_type"`
	DeduplicationKey string  `gorm:"column:deduplication_key"`
	Payload          string  `gorm:"column:payload"`
	Status           string  `gorm:"column:status"`
	Attempts         int64   `gorm:"column:attempts"`
	LastError        *string `gorm:"column:last_error"`
	AvailableAt      int64   `gorm:"column:available_at"`
	LeaseOwner       *string `gorm:"column:lease_owner"`
	LeaseUntil       *int64  `gorm:"column:lease_until"`
	CreatedAt        int64   `gorm:"column:created_at;autoCreateTime:milli"`
	DeliveredAt      *int64  `gorm:"column:delivered_at"`
}

func (OutboxEvent) TableName() string { return "outbox_events" }

// ReconciliationRun maps explicitly to reconciliation_runs; migrations own constraints.
type ReconciliationRun struct {
	ID          string `gorm:"column:id;primaryKey"`
	WorkspaceID string `gorm:"column:workspace_id"`
	RunKey      string `gorm:"column:run_key"`
	Status      string `gorm:"column:status"`
	WindowStart int64  `gorm:"column:window_start"`
	WindowEnd   int64  `gorm:"column:window_end"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
	CompletedAt *int64 `gorm:"column:completed_at"`
}

func (ReconciliationRun) TableName() string { return "reconciliation_runs" }

// ReconciliationDiscrepancy maps explicitly to reconciliation_discrepancies; migrations own constraints.
type ReconciliationDiscrepancy struct {
	ID             string  `gorm:"column:id;primaryKey"`
	WorkspaceID    string  `gorm:"column:workspace_id"`
	RunID          string  `gorm:"column:run_id"`
	PaymentID      *string `gorm:"column:payment_id"`
	Fingerprint    string  `gorm:"column:fingerprint"`
	Kind           string  `gorm:"column:kind"`
	Details        string  `gorm:"column:details"`
	Status         string  `gorm:"column:status"`
	ResolutionNote *string `gorm:"column:resolution_note"`
	ResolvedBy     *string `gorm:"column:resolved_by"`
	CreatedAt      int64   `gorm:"column:created_at;autoCreateTime:milli"`
	ResolvedAt     *int64  `gorm:"column:resolved_at"`
}

func (ReconciliationDiscrepancy) TableName() string { return "reconciliation_discrepancies" }

// User maps explicitly to users; migrations own constraints.
type User struct {
	ID        string `gorm:"column:id;primaryKey"`
	Email     string `gorm:"column:email"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (User) TableName() string { return "users" }

// WorkspaceMember maps explicitly to workspace_members; migrations own constraints.
type WorkspaceMember struct {
	UserID      string `gorm:"column:user_id"`
	WorkspaceID string `gorm:"column:workspace_id"`
	Role        string `gorm:"column:role"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (WorkspaceMember) TableName() string { return "workspace_members" }

// Session maps explicitly to sessions; migrations own constraints.
type Session struct {
	ID          string `gorm:"column:id;primaryKey"`
	UserID      string `gorm:"column:user_id"`
	WorkspaceID string `gorm:"column:workspace_id"`
	TokenHash   string `gorm:"column:token_hash"`
	CSRFHash    string `gorm:"column:csrf_hash"`
	ExpiresAt   int64  `gorm:"column:expires_at"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (Session) TableName() string { return "sessions" }

// APIKey maps explicitly to api_keys; migrations own constraints.
type APIKey struct {
	ID          string `gorm:"column:id;primaryKey"`
	WorkspaceID string `gorm:"column:workspace_id"`
	Name        string `gorm:"column:name"`
	Prefix      string `gorm:"column:prefix"`
	KeyHash     string `gorm:"column:key_hash"`
	Role        string `gorm:"column:role"`
	ExpiresAt   *int64 `gorm:"column:expires_at"`
	RevokedAt   *int64 `gorm:"column:revoked_at"`
	CreatedAt   int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (APIKey) TableName() string { return "api_keys" }

// MockProviderCharge maps explicitly to mock_provider_charges; migrations own constraints.
type MockProviderCharge struct {
	ID                     string `gorm:"column:id;primaryKey"`
	WorkspaceID            string `gorm:"column:workspace_id"`
	ProviderID             string `gorm:"column:provider_id"`
	ProviderIdempotencyKey string `gorm:"column:provider_idempotency_key"`
	ProviderReference      string `gorm:"column:provider_reference"`
	AmountMinor            int64  `gorm:"column:amount_minor"`
	Currency               string `gorm:"column:currency"`
	Status                 string `gorm:"column:status"`
	Scenario               string `gorm:"column:scenario"`
	CreatedAt              int64  `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt              int64  `gorm:"column:updated_at;autoUpdateTime:milli"`
}

func (MockProviderCharge) TableName() string { return "mock_provider_charges" }

// SimulationRun maps explicitly to simulation_runs; migrations own constraints.
type SimulationRun struct {
	ID          string  `gorm:"column:id;primaryKey"`
	WorkspaceID string  `gorm:"column:workspace_id"`
	PaymentID   *string `gorm:"column:payment_id"`
	Scenario    string  `gorm:"column:scenario"`
	Seed        string  `gorm:"column:seed"`
	InputJSON   string  `gorm:"column:input_json"`
	ResultJSON  *string `gorm:"column:result_json"`
	Status      string  `gorm:"column:status"`
	CreatedAt   int64   `gorm:"column:created_at;autoCreateTime:milli"`
	CompletedAt *int64  `gorm:"column:completed_at"`
}

func (SimulationRun) TableName() string { return "simulation_runs" }
