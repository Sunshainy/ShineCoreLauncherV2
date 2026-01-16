package logging

import "log/slog"

// Init keeps logging setup minimal for the frontend-only window.
func Init() {
	slog.SetDefault(slog.Default())
}
