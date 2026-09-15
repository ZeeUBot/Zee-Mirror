package basic

import "testing"

func TestNormalizeHelpAction(t *testing.T) {
	tests := map[string]string{
		"cat_download":   "download",
		"sub_dl_general": "download",
		"sub_dl_adv":     "download",
		"cat_monitor":    "monitor",
		"cat_files":      "files",
		"cat_media":      "media",
		"cat_task":       "task",
		"cat_storage":    "storage",
		"cat_admin":      "admin",
		"cat_recovery":   "recovery",
		"cat_settings":   "settings",
		"cmd_lang":       "settings",
		"unknown_thing":  "unknown_thing",
		"":               "",
	}
	for in, want := range tests {
		if got := normalizeHelpAction(in); got != want {
			t.Errorf("normalizeHelpAction(%q) = %q, want %q", in, got, want)
		}
	}
}
