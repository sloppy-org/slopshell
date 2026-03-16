package mailtriage

import (
	"strings"
	"testing"
)

func TestDistillReviewedExamplesBuildsBoundedSummary(t *testing.T) {
	training := DistillReviewedExamples([]ReviewedExample{
		{Sender: "Alice <alice@example.com>", Subject: "Timesheet", Folder: "Posteingang", Action: "inbox"},
		{Sender: "Alice <alice@example.com>", Subject: "FuEL follow-up", Folder: "Posteingang", Action: "inbox"},
		{Sender: "List <list@example.com>", Subject: "Weekly digest", Folder: "Posteingang", Action: "cc"},
		{Sender: "Bob <bob@example.com>", Subject: "Scam 1", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Carol <carol@example.com>", Subject: "Scam 2", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Dave <dave@example.com>", Subject: "Scam 3", Folder: "Junk-E-Mail", Action: "trash"},
		{Sender: "Editor <editor@predatory.example>", Subject: "Call for papers", Folder: "Junk-E-Mail", Action: "archive"},
	})
	if training.ReviewCount != 7 {
		t.Fatalf("ReviewCount = %d, want 7", training.ReviewCount)
	}
	if len(training.PolicySummary) == 0 {
		t.Fatal("PolicySummary is empty")
	}
	if len(training.PolicySummary) > maxPolicySummaryLines {
		t.Fatalf("PolicySummary len = %d, want <= %d", len(training.PolicySummary), maxPolicySummaryLines)
	}
	joined := strings.Join(training.PolicySummary, "\n")
	if !strings.Contains(joined, "Manual review distribution: inbox=2, cc=1, archive=1, trash=3") {
		t.Fatalf("summary missing action distribution: %q", joined)
	}
	if !strings.Contains(joined, "Folder rule: Junk-E-Mail usually -> trash") {
		t.Fatalf("summary missing folder rule: %q", joined)
	}
	if !strings.Contains(joined, "Sender rule: alice@example.com usually -> inbox") {
		t.Fatalf("summary missing sender rule: %q", joined)
	}
	if len(training.Examples) == 0 {
		t.Fatal("Examples is empty")
	}
	if len(training.Examples) > maxTrainingExamples {
		t.Fatalf("Examples len = %d, want <= %d", len(training.Examples), maxTrainingExamples)
	}
}
