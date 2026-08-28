package session

import (
	"os"
	"testing"
)

func setupManager(t *testing.T) (*Manager, string) {
	tmpFile, err := os.CreateTemp("", "session_test*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFileName := tmpFile.Name()
	tmpFile.Close()

	m, err := NewManager(tmpFileName, "Default Prompt")
	if err != nil {
		t.Fatal(err)
	}
	return m, tmpFileName
}
