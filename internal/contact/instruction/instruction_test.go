package instruction

import (
	"context"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestInstructionKinds(t *testing.T) {
	tests := []struct {
		kind    string
		contact manifest.Contact
		want    string
	}{
		{"url", manifest.Contact{URL: "https://example.test/contact"}, "open https://example.test/contact"},
		{"email", manifest.Contact{Address: "owner@example.test", SubjectPrefix: "[lib]"}, "subject prefix [lib]"},
		{"mattermost", manifest.Contact{Server: "chat.example.test", Channel: "dev", Handle: "@owner"}, "server=chat.example.test channel=dev handle=@owner"},
	}
	for _, test := range tests {
		record, err := (Driver{ContactKind: test.kind}).Deliver(context.Background(), contact.Request{Agent: "owner", Contact: test.contact, Message: "hello"})
		if err != nil || record.State != "instruction" || !strings.Contains(record.Instruction, test.want) {
			t.Errorf("%s: record=%#v err=%v", test.kind, record, err)
		}
		if strings.Contains(record.Instruction, "hello") {
			t.Errorf("%s: instruction duplicated message content", test.kind)
		}
	}
}
