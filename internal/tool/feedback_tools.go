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
		Description: "Write a bug or a feature request about Groot itself into the backlog. Use it when the user reports a defect in groot or asks for a groot feature, after feedback_list has shown no duplicate. Give a short title and a body with the steps, the expected behavior and the actual behavior. Problems in the user's own project do not belong here; handle them in the conversation.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"type":{"type":"string","enum":["bug","feat"],"description":"bug or feat"},` +
			`"title":{"type":"string","description":"One-line summary of the report"},` +
			`"body":{"type":"string","description":"Steps to reproduce, the expected behavior and the actual behavior; optional but advised"},` +
			`"area":{"type":"string","description":"The part of groot concerned, such as tui or tools; optional"}},` +
			`"required":["type","title"]}`),
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
		Description: "List the backlog of bugs and feature requests about Groot, optionally filtered by status. Use it to find duplicates before feedback_report, or to check the state of an item the user mentions. It only reads the backlog and changes nothing.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","description":"open, in_progress, done or wontfix; omit for every item"}}}`),
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
		Description: "Set the status of one backlog item and add an optional note. Use it when the user says an item is done or dropped, after feedback_list has shown its id. It changes only the status and the note; use feedback_report to file a new item.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"Backlog item id, as feedback_list shows it"},` +
			`"status":{"type":"string","enum":["open","in_progress","done","wontfix"],"description":"open, in_progress, done or wontfix"},` +
			`"note":{"type":"string","description":"Free-text note recorded with the change; optional"}},` +
			`"required":["id","status"]}`),
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
