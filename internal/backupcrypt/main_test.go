package backupcrypt

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// gpg keeps sockets in its home directory; a short path keeps them under
	// the length limit, and a private one never touches the user's keyring.
	home, err := os.MkdirTemp("", "g")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("GNUPGHOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
