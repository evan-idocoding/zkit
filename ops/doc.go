// Package ops provides small, standard-library flavored net/http handlers for operational endpoints.
//
// ops is designed to be mounted into your own routing tree. It intentionally:
//   - does not choose routing paths (mount it anywhere),
//   - does not do authn/authz decisions (protect it with your own middleware),
//   - does not start servers or manage process lifecycle.
//
// All handlers are designed to be easy to compose and to have minimal dependencies.
//
// # Formats
//
// Many handlers support both text and JSON output. By default they render text.
// The default can be configured by options, and can be overridden per request by URL query:
//   - ?format=text
//   - ?format=json
//
// Text output is line-based and stable/greppable. JSON output is structured and suitable for tooling.
//
// # What ops provides
//
// This package includes handlers for:
//   - health: HealthzHandler (liveness), ReadyzHandler (readiness checks)
//   - runtime/build: RuntimeHandler, BuildInfoHandler
//   - tasks: TasksSnapshotHandler, TaskTriggerHandler, TaskTriggerAndWaitHandler (rt/task integration)
//   - tuning: TuningSnapshotHandler, TuningOverridesHandler, TuningLookupHandler, TuningSetHandler, Reset* (rt/tuning integration)
//   - logging: LogLevelGetHandler, LogLevelSetHandler (slog.LevelVar)
//   - injected snapshots: ProvidedSnapshotHandler (render provided data as JSON/text)
//
// # Security notes
//
// Operational endpoints often expose sensitive information. Mount these handlers behind your own
// authentication/authorization middleware, and consider restricting write handlers (trigger/set)
// with allowlists such as WithTaskAllowNames / WithTuningAllowKeys.
package ops
