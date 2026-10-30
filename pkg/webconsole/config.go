// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) config(w http.ResponseWriter) {
	path := strings.TrimSpace(h.opts.ConfigPath)
	if path == "" {
		writeError(w, http.StatusNotFound, "config path is unavailable")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	redacted, err := api.RedactYAMLSecrets(string(data))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redact config: "+err.Error())
		return
	}
	writeJSON(w, ConfigSnapshot{Path: path, Text: redacted})
}

func (h Handler) generations(w http.ResponseWriter, r *http.Request) {
	reader, ok := h.opts.Store.(routerstate.GenerationHistoryReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "generation history is unavailable")
		return
	}
	rows, err := reader.ListGenerations(intQuery(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (h Handler) generationDetail(w http.ResponseWriter, r *http.Request, suffix string) {
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) == 2 && parts[1] == "config" {
		h.generationConfig(w, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "diff" {
		h.generationDiff(w, r, parts[0], parts[2])
		return
	}
	http.NotFound(w, r)
}

func (h Handler) generationConfig(w http.ResponseWriter, idText string) {
	reader, ok := h.opts.Store.(routerstate.GenerationHistoryReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "generation history is unavailable")
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid generation id")
		return
	}
	configYAML, found, err := reader.GenerationConfig(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusConflict, "this generation has no stored YAML; diff is available for newly applied generations")
		return
	}
	configYAML, err = api.RedactYAMLSecrets(configYAML)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redact generation config: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(configYAML))
}

func (h Handler) generationDiff(w http.ResponseWriter, r *http.Request, fromText, toText string) {
	reader, ok := h.opts.Store.(routerstate.GenerationHistoryReader)
	if !ok {
		writeError(w, http.StatusNotImplemented, "generation history is unavailable")
		return
	}
	from, err := strconv.ParseInt(fromText, 10, 64)
	if err != nil || from <= 0 {
		writeError(w, http.StatusBadRequest, "invalid from generation id")
		return
	}
	currentFile := toText == "current"
	var to int64
	if !currentFile {
		to, err = strconv.ParseInt(toText, 10, 64)
		if err != nil || to <= 0 {
			writeError(w, http.StatusBadRequest, "invalid to generation id")
			return
		}
	}
	fromYAML, found, err := reader.GenerationConfig(from)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusConflict, fmt.Sprintf("generation %d has no stored YAML", from))
		return
	}
	fromYAML, err = api.RedactYAMLSecrets(fromYAML)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redact from generation config: "+err.Error())
		return
	}
	toName := fmt.Sprintf("generation-%d.yaml", to)
	var toYAML string
	if currentFile {
		if strings.TrimSpace(h.opts.ConfigPath) == "" {
			writeError(w, http.StatusNotFound, "config path is unavailable")
			return
		}
		data, err := os.ReadFile(h.opts.ConfigPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		toYAML = string(data)
		toName = "current-file.yaml"
	} else {
		toYAML, found, err = reader.GenerationConfig(to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusConflict, fmt.Sprintf("generation %d has no stored YAML", to))
			return
		}
	}
	toYAML, err = api.RedactYAMLSecrets(toYAML)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redact to generation config: "+err.Error())
		return
	}
	diff, err := unifiedDiff(r.Context(), fmt.Sprintf("generation-%d.yaml", from), toName, fromYAML, toYAML)
	if err != nil {
		writeError(w, http.StatusRequestTimeout, err.Error())
		return
	}
	if !currentFile && strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, GenerationDiff{From: from, To: to, Diff: diff})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(diff))
}

func unifiedDiff(ctx context.Context, fromName, toName, fromText, toText string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if fromText == toText {
		return fmt.Sprintf("--- %s\n+++ %s\n", fromName, toName), nil
	}
	fromLines := splitDiffLines(fromText)
	toLines := splitDiffLines(toText)
	ops, err := diffLineOps(ctx, fromLines, toLines)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", fromName)
	fmt.Fprintf(&b, "+++ %s\n", toName)
	fromStart, toStart := 1, 1
	if len(fromLines) == 0 {
		fromStart = 0
	}
	if len(toLines) == 0 {
		toStart = 0
	}
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", fromStart, len(fromLines), toStart, len(toLines))
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		b.WriteByte(op.prefix)
		b.WriteString(op.line)
		if !strings.HasSuffix(op.line, "\n") {
			b.WriteString("\n\\ No newline at end of file\n")
		}
	}
	return b.String(), nil
}

type diffLineOp struct {
	prefix byte
	line   string
}

func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func diffLineOps(ctx context.Context, a, b []string) ([]diffLineOp, error) {
	// Preserve common edges and replace the changed span. This is a valid diff
	// even for separated edits, with linear work and memory for large configs.
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		suffix++
	}
	ops := make([]diffLineOp, 0, len(a)+len(b)-prefix-suffix)
	appendLines := func(lines []string, marker byte) error {
		for _, line := range lines {
			if err := ctx.Err(); err != nil {
				return err
			}
			ops = append(ops, diffLineOp{prefix: marker, line: line})
		}
		return nil
	}
	for _, span := range []struct {
		lines  []string
		marker byte
	}{
		{a[:prefix], ' '},
		{a[prefix : len(a)-suffix], '-'},
		{b[prefix : len(b)-suffix], '+'},
		{a[len(a)-suffix:], ' '},
	} {
		if err := appendLines(span.lines, span.marker); err != nil {
			return nil, err
		}
	}
	return ops, nil
}
