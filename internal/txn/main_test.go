package txn

import (
	"os"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile/cfgfiletest"
)

func TestMain(m *testing.M) {
	line = func(string, string) {}
	cfgfiletest.Main(m)
	os.Exit(0) // not reached: Main exits
}
