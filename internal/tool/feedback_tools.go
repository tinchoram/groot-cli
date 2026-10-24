package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tinchoram/groot-cli/internal/api"
	"github.com/tinchoram/groot-cli/internal/feedback"
)

// FeedbackReport files a bug or a feature request in the backlog.
type FeedbackReport struct {
	store                 *feedback.Store
	space, agent, version string
}

// NewFeedbackReport returns the feedback_report tool for an agent in a space.
func NewFeedbackReport(store *feedback.Store, space, agent, version string) *FeedbackReport {
	return &FeedbackReport{store: store, space: space, agent: agent, version: version}
}

type feedbackReportInput struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Area  string `json:"area"`
}

func (*FeedbackReport) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "feedback_report",
		Description: "Write a bug or a feature request about Groot itself into the backlog. type is bug or feat. Give a short title and a body with the steps, the expected and the actual behaviour.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"type":{"type":"string","enum":["bug","feat"]},"title":{"type":"string"},"body":{"type":"string"},"area":{"type":"string"}},"required":["type","title"]}`),
	}
}

func (*FeedbackReport) Risk() Risk            { return RiskWrite }
func (f *FeedbackReport) RiskFor(string) Risk { return f.Risk() }

func (*FeedbackReport) Preview(input string) string {
	var in feedbackReportInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "feedback_report: invalid input"
	}
	return fmt.Sprintf("report %s: %s", in.Type, in.Title)
}

func (f *FeedbackReport) Execute(_ context.Context, input string) (string, bool) {
	var in feedbackReportInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	it, err := f.store.Report(feedback.Item{
		Type: in.Type, Title: in.Title, Body: in.Body, Area: in.Area, Space: f.space, Agent: f.agent, Version: f.version,
	})
	if err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("reported %s (open)", it.ID), false
}

// FeedbackList lists the backlog items, optionally by status.
type FeedbackList struct{ store *feedback.Store }

// NewFeedbackList returns the feedback_list tool.
func NewFeedbackList(store *feedback.Store) *FeedbackList { return &FeedbackList{store: store} }

func (*FeedbackList) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "feedback_list",
		Description: "List the backlog of bugs and feature requests about Groot, optionally by status (open, in_progress, done, wontfix). Use it to find duplicates before reporting.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}}}`),
	}
}

func (*FeedbackList) Risk() Risk            { return RiskRead }
func (f *FeedbackList) RiskFor(string) Risk { return f.Risk() }
func (*FeedbackList) Preview(string) string { return "feedback_list" }

func (f *FeedbackList) Execute(_ context.Context, input string) (string, bool) {
	var in struct {
		Status string `json:"status"`
	}
	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return "invalid input: " + err.Error(), true
		}
	}
	items, err := f.store.List(in.Status)
	if err != nil {
		return err.Error(), true
	}
	if len(items) == 0 {
		return "no items", false
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "%s %s — %s\n", it.ID, it.Status, it.Title)
	}
	return strings.TrimRight(b.String(), "\n"), false
}

// FeedbackUpdate changes the status of a backlog item.
type FeedbackUpdate struct{ store *feedback.Store }

// NewFeedbackUpdate returns the feedback_update tool.
func NewFeedbackUpdate(store *feedback.Store) *FeedbackUpdate {
	return &FeedbackUpdate{store: store}
}

func (*FeedbackUpdate) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "feedback_update",
		Description: "Set the status of a backlog item (open, in_progress, done, wontfix) and add an optional note. Use it when the user says an item is done or dropped.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string","enum":["open","in_progress","done","wontfix"]},"note":{"type":"string"}},"required":["id","status"]}`),
	}
}

func (*FeedbackUpdate) Risk() Risk            { return RiskWrite }
func (f *FeedbackUpdate) RiskFor(string) Risk { return f.Risk() }

func (*FeedbackUpdate) Preview(input string) string {
	var in struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "feedback_update: invalid input"
	}
	return fmt.Sprintf("%s → %s", in.ID, in.Status)
}

func (f *FeedbackUpdate) Execute(_ context.Context, input string) (string, bool) {
	var in struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "invalid input: " + err.Error(), true
	}
	if err := f.store.Update(in.ID, in.Status, in.Note); err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("%s is now %s", in.ID, in.Status), false
}
