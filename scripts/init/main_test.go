package main

import "testing"

func TestProjectName(t *testing.T) {
	tests := map[string]string{
		"example.com/my-project":    "my-project",
		"example.com/my-project/v2": "my-project",
		"example.com/org/My-App":    "my-app",
		"project":                   "project",
	}
	for module, want := range tests {
		if got := projectName(module); got != want {
			t.Errorf("projectName(%q) = %q, want %q", module, got, want)
		}
	}
}

func TestDBIdentifier(t *testing.T) {
	tests := map[string]string{
		"my-project": "my_project",
		"my.project": "my_project",
		"project":    "project",
	}
	for name, want := range tests {
		if got := dbIdentifier(name); got != want {
			t.Errorf("dbIdentifier(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRemoveTemplateOnlyBlocks(t *testing.T) {
	const input = `keep one
# template-only:begin
drop
# template-only:end
keep two
  <!-- template-only:begin -->
drop too
  <!-- template-only:end -->
keep three
`
	const want = "keep one\nkeep two\nkeep three\n"

	if got := removeTemplateOnlyBlocks(input); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
