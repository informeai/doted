package notify

import (
	"slices"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	mac := Command("darwin", `say "hi"`, `back\slash`)
	if mac[0] != "osascript" || mac[2] != `display notification "back\\slash" with title "say \"hi\""` {
		t.Fatalf("darwin: %q", mac)
	}
	if got := Command("linux", "t", "b"); !slices.Equal(got, []string{"notify-send", "--app-name=doted", "t", "b"}) {
		t.Fatalf("linux: %q", got)
	}
	if win := Command("windows", "it's", "b"); win[0] != "powershell" || !strings.Contains(win[4], "'it''s'") {
		t.Fatalf("windows: %q", win)
	}
}
