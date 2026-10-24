package workspace

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tinchoram/groot-cli/internal/audit"
	"github.com/tinchoram/groot-cli/internal/config"
	"github.com/tinchoram/groot-cli/internal/space"
)

// Audit flag marks, in the order AuditFlags writes them.
const (
	flagGuard  = "⚠"
	flagDenied = "⛔"
	flagError  = "✗"
	flagNoBox  = "∅"
)

// defaultSyslogTag is the program name of the syslog sink when none is set.
const defaultSyslogTag = "groot"

// defaultFlush is how often an http sink sends its pending batch when
// flush_seconds is unset.
const defaultFlush = 5 * time.Second

// sinkCloseBudget caps how long quitting waits for the export sinks.
const sinkCloseBudget = 3 * time.Second

// spaceAudit resolves the space named name (the active one when empty) and the
// path of its audit log. It does not create anything.
func (w *Workspace) spaceAudit(name string) (space.Space, string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var (
		sp space.Space
		ok bool
	)
	if name == "" {
		sp, ok = w.opts.Spaces.Current()
		if !ok {
			return space.Space{}, "", errors.New("no active space")
		}
	} else if sp, ok = w.opts.Spaces.Get(name); !ok {
		return space.Space{}, "", fmt.Errorf("unknown space %q", name)
	}
	return sp, auditPathFor(sp.Path, w.opts.Config.Audit.Path), nil
}

// Audit returns the records of one space that match q, newest first, and their
// stats. An empty name is the active space. The stats count every matching
// record, not only the returned ones, and carry the export counters.
func (w *Workspace) Audit(ctx context.Context, name string, q audit.Query) ([]audit.Record, audit.Stats, error) {
	sp, path, err := w.spaceAudit(name)
	if err != nil {
		return nil, audit.Stats{}, err
	}
	recs, err := audit.Read(path, q)
	if err != nil {
		return nil, audit.Stats{}, fmt.Errorf("space %s: %w", sp.Name, err)
	}
	st, err := audit.Summarize(path, q)
	if err != nil {
		return nil, audit.Stats{}, fmt.Errorf("space %s: %w", sp.Name, err)
	}
	for i := range recs {
		if recs[i].Space == "" {
			recs[i].Space = sp.Name
		}
	}
	st.Sinks = w.sinks.Stats()
	return recs, st, nil
}

// AuditRecords is Audit without the stats: the records only.
func (w *Workspace) AuditRecords(_ context.Context, name string, q audit.Query) ([]audit.Record, error) {
	sp, path, err := w.spaceAudit(name)
	if err != nil {
		return nil, err
	}
	recs, err := audit.Read(path, q)
	if err != nil {
		return nil, fmt.Errorf("space %s: %w", sp.Name, err)
	}
	for i := range recs {
		if recs[i].Space == "" {
			recs[i].Space = sp.Name
		}
	}
	return recs, nil
}

// AuditAll returns the records of every space that match q, newest first, cut
// to q.Limit, and the stats summed over the spaces. A space whose directory is
// gone has no log and is skipped; its directory is not recreated.
func (w *Workspace) AuditAll(ctx context.Context, q audit.Query) ([]audit.Record, audit.Stats, error) {
	w.mu.Lock()
	spaces := w.opts.Spaces.List()
	auditPath := w.opts.Config.Audit.Path
	w.mu.Unlock()

	limit := q.Limit
	if limit <= 0 {
		limit = audit.DefaultLimit
	}
	perSpace := q
	perSpace.Limit = limit

	st := audit.Stats{ByDecision: map[string]int{}, ByRisk: map[string]int{}}
	var all []audit.Record
	for _, sp := range spaces {
		if _, err := os.Stat(sp.Path); err != nil {
			continue
		}
		path := auditPathFor(sp.Path, auditPath)
		recs, err := audit.Read(path, perSpace)
		if err != nil {
			return nil, audit.Stats{}, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		one, err := audit.Summarize(path, q)
		if err != nil {
			return nil, audit.Stats{}, fmt.Errorf("space %s: %w", sp.Name, err)
		}
		mergeStats(&st, one)
		for i := range recs {
			if recs[i].Space == "" {
				recs[i].Space = sp.Name
			}
		}
		all = append(all, recs...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Time.After(all[j].Time) })
	if len(all) > limit {
		all = all[:limit]
	}
	st.Sinks = w.sinks.Stats()
	return all, st, nil
}

// mergeStats adds src to dst.
func mergeStats(dst *audit.Stats, src audit.Stats) {
	dst.Total += src.Total
	dst.Denied += src.Denied
	dst.Guard += src.Guard
	dst.Errors += src.Errors
	dst.Skipped += src.Skipped
	for k, v := range src.ByDecision {
		dst.ByDecision[k] += v
	}
	for k, v := range src.ByRisk {
		dst.ByRisk[k] += v
	}
}

// AuditReport is the records of a space (the active one when name is empty) as
// the table /audit prints, for the admin tools.
func (w *Workspace) AuditReport(ctx context.Context, name string, q audit.Query) (string, error) {
	recs, err := w.AuditRecords(ctx, name, q)
	if err != nil {
		return "", err
	}
	return FormatAudit(recs, AuditFormat{}), nil
}

// AuditFormat shapes the audit table.
type AuditFormat struct {
	// Width is the longest preview, in runes, ellipsis included. Zero keeps
	// the whole preview.
	Width int
	// Space adds a SPACE column, for listings across spaces.
	Space bool
}

// FormatAudit lays out records as an aligned table, one row per record.
func FormatAudit(recs []audit.Record, f AuditFormat) string {
	if len(recs) == 0 {
		return "no audit records"
	}
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	header := "TIME\tAGENT\tTOOL\tRISK\tDECISION\tDUR\tFLAGS\tPREVIEW"
	if f.Space {
		header = "TIME\tSPACE\tAGENT\tTOOL\tRISK\tDECISION\tDUR\tFLAGS\tPREVIEW"
	}
	fmt.Fprintln(tw, header)
	for _, r := range recs {
		agent := orDash(r.Agent)
		flags := orDash(AuditFlags(r))
		preview := previewCut(r.InputPreview, f.Width)
		when := r.Time.Local().Format("15:04:05")
		dur := fmt.Sprintf("%dms", r.DurationMS)
		if f.Space {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", when, orDash(r.Space), agent, r.Tool, r.Risk, r.Decision, dur, flags, preview)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", when, agent, r.Tool, r.Risk, r.Decision, dur, flags, preview)
	}
	_ = tw.Flush()
	return strings.TrimRight(sb.String(), "\n")
}

// AuditFlags marks what stands out in a record: ⚠ guard findings, ⛔ denied,
// ✗ error, ∅ no sandbox. Marks are separated by spaces; a plain call has none.
func AuditFlags(r audit.Record) string {
	var marks []string
	if len(r.Guard) > 0 {
		marks = append(marks, flagGuard)
	}
	if r.Decision == "deny" {
		marks = append(marks, flagDenied)
	}
	if r.IsError {
		marks = append(marks, flagError)
	}
	if r.Sandbox == "none" {
		marks = append(marks, flagNoBox)
	}
	return strings.Join(marks, " ")
}

// FormatAuditStats lays out the counters of Stats as the block /audit stats prints.
func FormatAuditStats(st audit.Stats) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d records · %d malformed lines skipped\n", st.Total, st.Skipped)
	fmt.Fprintf(&sb, "decisions: %s\n", countsLine(st.ByDecision))
	fmt.Fprintf(&sb, "risk: %s\n", countsLine(st.ByRisk))
	fmt.Fprintf(&sb, "denied %d · guard %d · errors %d", st.Denied, st.Guard, st.Errors)
	if len(st.Sinks) > 0 {
		sb.WriteString("\nsinks:")
		for _, s := range st.Sinks {
			fmt.Fprintf(&sb, "\n  %s: sent %d, dropped %d, failures %d", s.Name, s.Sent, s.Dropped, s.Failures)
			if s.LastError != "" {
				fmt.Fprintf(&sb, ", last error: %s", s.LastError)
			}
		}
	}
	return sb.String()
}

// countsLine renders counts sorted by key, "allow 3, deny 1"; "none" when empty.
func countsLine(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := slices.Sorted(maps.Keys(m))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// previewCut cuts s to width runes, ending with an ellipsis. A width of zero
// or less keeps s whole.
func previewCut(s string, width int) string {
	rs := []rune(s)
	if width <= 0 || len(rs) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(rs[:width-1]) + "…"
}

// buildSinks adds the export sinks of the configuration to f.
func buildSinks(cfg []config.SinkConfig, f *audit.Fanout) error {
	for _, s := range cfg {
		switch s.Type {
		case "file":
			fs, err := audit.NewFileSink(s.Path)
			if err != nil {
				return err
			}
			f.Add("file", fs, 0)
		case "http":
			flush := time.Duration(s.FlushSeconds) * time.Second
			if flush == 0 {
				flush = defaultFlush
			}
			h := audit.NewHTTPSink(audit.HTTPOptions{URL: s.URL, Headers: s.Headers, Batch: s.Batch})
			f.Add("http", h, flush)
		case "syslog":
			tag := s.Tag
			if tag == "" {
				tag = defaultSyslogTag
			}
			sk, err := audit.DialSyslog(tag)
			if err != nil {
				return err
			}
			f.Add("syslog", sk, 0)
		default:
			return fmt.Errorf("audit sink %q: unknown type", s.Type)
		}
	}
	return nil
}

// noteSinkWarning keeps a sink warning until the UI reads it.
func (w *Workspace) noteSinkWarning(msg string) {
	w.sinkMu.Lock()
	defer w.sinkMu.Unlock()
	w.sinkWarns = append(w.sinkWarns, msg)
}

// SinkWarnings returns the sink warnings raised since the last call. Each one is
// returned once.
func (w *Workspace) SinkWarnings() []string {
	w.sinkMu.Lock()
	defer w.sinkMu.Unlock()
	out := w.sinkWarns
	w.sinkWarns = nil
	return out
}

// closeSinks flushes the export sinks, waiting at most sinkCloseBudget.
func (w *Workspace) closeSinks() error {
	ctx, cancel := context.WithTimeout(context.Background(), sinkCloseBudget)
	defer cancel()
	return w.sinks.Close(ctx)
}
