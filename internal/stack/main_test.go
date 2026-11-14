package stack

import (
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile/cfgfiletest"
)

// The code under test writes configuration files through cfgfile; keep its
// manifest and backups out of the real system directories.
func TestMain(m *testing.M) { cfgfiletest.Main(m) }
