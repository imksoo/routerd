// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
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
	to, err := strconv.ParseInt(toText, 10, 64)
	if err != nil || to <= 0 {
		writeError(w, http.StatusBadRequest, "invalid to generation id")
		return
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
	toYAML, found, err := reader.GenerationConfig(to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusConflict, fmt.Sprintf("generation %d has no stored YAML", to))
		return
	}
	toYAML, err = api.RedactYAMLSecrets(toYAML)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redact to generation config: "+err.Error())
		return
	}
	diff := unifiedDiff(fmt.Sprintf("generation-%d.yaml", from), fmt.Sprintf("generation-%d.yaml", to), fromYAML, toYAML)
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, GenerationDiff{From: from, To: to, Diff: diff})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(diff))
}

func unifiedDiff(fromName, toName, fromText, toText string) string {
	if fromText == toText {
		return fmt.Sprintf("--- %s\n+++ %s\n", fromName, toName)
	}
	fromLines := splitDiffLines(fromText)
	toLines := splitDiffLines(toText)
	ops := diffLineOps(fromLines, toLines)
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", fromName)
	fmt.Fprintf(&b, "+++ %s\n", toName)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(fromLines), len(toLines))
	for _, op := range ops {
		b.WriteByte(op.prefix)
		b.WriteString(op.line)
		if !strings.HasSuffix(op.line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
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

func diffLineOps(a, b []string) []diffLineOp {
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffLineOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffLineOp{prefix: ' ', line: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffLineOp{prefix: '-', line: a[i]})
			i++
		default:
			ops = append(ops, diffLineOp{prefix: '+', line: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffLineOp{prefix: '-', line: a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffLineOp{prefix: '+', line: b[j]})
	}
	return ops
}
