package domain

import "testing"

func TestSessionValidateNeedsPlainPathElements(t *testing.T) {
	tests := []struct {
		name    string
		session Session
		wantErr bool
	}{
		{"plain names", Session{Slug: "proj", ConversationID: "0b9e-conv"}, false},
		{"empty slug", Session{Slug: "", ConversationID: "conv"}, true},
		{"dot slug", Session{Slug: ".", ConversationID: "conv"}, true},
		{"parent slug", Session{Slug: "..", ConversationID: "conv"}, true},
		{"nested id", Session{Slug: "proj", ConversationID: "a/b"}, true},
		{"escaping id", Session{Slug: "proj", ConversationID: "../../outside"}, true},
		{"absolute id", Session{Slug: "proj", ConversationID: "/etc"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.session.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
