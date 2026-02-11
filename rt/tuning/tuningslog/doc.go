// Package tuningslog provides small helpers to bind tuning variables to log/slog.
//
// It bridges runtime-tunable variables (package tuning) and slog constructs.
//
// Note: tuning's Set is a blocking model and executes callbacks synchronously. Do NOT call
// Set/SetFromString on latency-sensitive hot paths.
package tuningslog
