package gov

import (
	"testing"
)

func TestParseDiffStatLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		files   int
		ins     int
		del     int
	}{
		{
			"standard",
			" 3 files changed, 10 insertions(+), 5 deletions(-)",
			3, 10, 5,
		},
		{
			"single file",
			" 1 file changed, 2 insertions(+)",
			1, 2, 0,
		},
		{
			"only deletions",
			" 2 files changed, 3 deletions(-)",
			2, 0, 3,
		},
		{
			"insertions plural",
			" 5 files changed, 100 insertions(+), 0 deletions(-)",
			5, 100, 0,
		},
		{
			"deletion singular",
			" 1 file changed, 1 insertion(+), 1 deletion(-)",
			1, 1, 1,
		},
		{
			"empty string",
			"",
			0, 0, 0,
		},
		{
			"garbage input",
			"not a diff stat line",
			0, 0, 0,
		},
		{
			"zero insertions and deletions",
			" 1 file changed, 0 insertions(+), 0 deletions(-)",
			1, 0, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, i, d := parseDiffStatLine(tt.input)
			if f != tt.files {
				t.Errorf("files = %d, want %d", f, tt.files)
			}
			if i != tt.ins {
				t.Errorf("insertions = %d, want %d", i, tt.ins)
			}
			if d != tt.del {
				t.Errorf("deletions = %d, want %d", d, tt.del)
			}
		})
	}
}