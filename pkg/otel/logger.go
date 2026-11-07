// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"errors"
	"log/slog"
)

// LoggerWithLocalOutput retains supervised local logs while exporting the same
// controller records through the configured OTLP logger. It changes no globals.
func (r *Runtime) LoggerWithLocalOutput() *slog.Logger {
	local := slog.Default()
	if r == nil || r.Logger == nil || r.Logger == local {
		return local
	}
	return slog.New(localAndExportHandler{local.Handler(), r.Logger.Handler()})
}

type localAndExportHandler struct{ local, export slog.Handler }

func (h localAndExportHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.local.Enabled(ctx, level) || h.export.Enabled(ctx, level)
}
func (h localAndExportHandler) Handle(ctx context.Context, record slog.Record) error {
	var localErr, exportErr error
	if h.local.Enabled(ctx, record.Level) {
		localErr = h.local.Handle(ctx, record.Clone())
	}
	if h.export.Enabled(ctx, record.Level) {
		exportErr = h.export.Handle(ctx, record.Clone())
	}
	return errors.Join(localErr, exportErr)
}
func (h localAndExportHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return localAndExportHandler{h.local.WithAttrs(attrs), h.export.WithAttrs(attrs)}
}
func (h localAndExportHandler) WithGroup(name string) slog.Handler {
	return localAndExportHandler{h.local.WithGroup(name), h.export.WithGroup(name)}
}
