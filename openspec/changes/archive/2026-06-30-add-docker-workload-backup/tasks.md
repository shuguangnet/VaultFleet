## 1. Policy Model And API

- [x] 1.1 Extend backup policy storage, API request/response types, and protocol payloads with optional pre/post backup hook fields.
- [x] 1.2 Add backend validation for hook structure, empty commands, and timeout bounds while preserving compatibility for policies without hooks.
- [x] 1.3 Add backend tests covering policy create/update flows with and without Docker backup hooks.

## 2. Agent Execution Flow

- [x] 2.1 Add agent-side hook execution support around backup jobs, including command launch, timeout handling, cancellation, and stage-aware error reporting.
- [x] 2.2 Ensure pre-hook failure prevents backup execution and post-hook failure is recorded as a failed task result after data collection completes.
- [x] 2.3 Add agent and executor tests for successful hooks, failing hooks, timeout cases, and legacy policies with no hooks.

## 3. Policy UI And Guidance

- [x] 3.1 Update the policy form and frontend types/services to edit optional backup hooks.
- [x] 3.2 Add Docker-focused guidance in the policy workflow, including recommended mounted paths, compose files, and consistency command examples.
- [x] 3.3 Add frontend tests covering hook form submission, validation feedback, and Docker guidance visibility.

## 4. Documentation And Rollout

- [x] 4.1 Update `README.md` and `README.en.md` to document Docker workload backup scope, supported examples, and explicit non-goals.
- [x] 4.2 Document operational caveats for hook permissions, failure semantics, and recommended usage for database containers.
- [x] 4.3 Run relevant Go and frontend test suites and capture any release notes or follow-up items discovered during verification.
