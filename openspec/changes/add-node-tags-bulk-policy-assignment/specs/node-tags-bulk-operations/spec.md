## ADDED Requirements

### Requirement: Node Tags
The system SHALL allow operators to assign zero or more normalized tags to each node and return those tags in node list and detail responses.

#### Scenario: Update node tags
- **WHEN** an authorized operator updates a node with tags containing whitespace, duplicates, or mixed case
- **THEN** the system stores a trimmed, lower-case, de-duplicated tag list and returns it on subsequent node reads

#### Scenario: Reject invalid tags
- **WHEN** an authorized operator submits an empty tag, an excessively long tag, or too many tags for one node
- **THEN** the system rejects the update with a validation error

### Requirement: Tag Discovery And Filtering
The system SHALL expose known node tags and allow node listing to be filtered by one or more tags.

#### Scenario: List known tags
- **WHEN** an authorized user requests known node tags
- **THEN** the system returns the distinct tags currently assigned to nodes in sorted order

#### Scenario: Filter nodes by tag
- **WHEN** an authorized user lists nodes with tag filters
- **THEN** the system returns only nodes containing every requested tag

### Requirement: Bulk Policy Assignment
The system SHALL allow authorized operators to clone one existing backup policy into per-node backup policies for multiple target nodes in one request.

#### Scenario: Assign policy to selected nodes
- **WHEN** an authorized operator submits a source policy ID with multiple target node IDs
- **THEN** the system creates one normal backup policy per target node from the source policy settings and marks each created policy as unsynced

#### Scenario: Assign policy by tags
- **WHEN** an authorized operator submits a source policy ID and tag selectors instead of explicit node IDs
- **THEN** the system resolves nodes matching every selector tag and creates one normal backup policy per resolved node

#### Scenario: Report partial failures
- **WHEN** some requested targets are invalid but other targets can receive the policy
- **THEN** the system returns per-target results showing created policy IDs for successes and validation errors for failures

#### Scenario: Preserve per-node repository defaults
- **WHEN** a bulk policy assignment clones a source policy to target nodes
- **THEN** each created policy uses the existing per-node default repository path based on that target node ID

### Requirement: Bulk Operation Permissions And Audit
The system SHALL protect node tag updates with node write permission and bulk policy assignment with policy write permission, and SHALL record successful batch mutations in audit logs.

#### Scenario: Viewer cannot mutate tags or bulk policies
- **WHEN** a viewer attempts to update node tags or bulk assign policies
- **THEN** the system denies the request

#### Scenario: Audit batch policy assignment
- **WHEN** an authorized operator bulk assigns policies
- **THEN** the system records an audit event for the policy mutation through the existing audit flow
