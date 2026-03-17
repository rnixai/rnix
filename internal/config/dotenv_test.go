package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "basic key=value",
			input: "FOO=bar",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "value with spaces trimmed",
			input: "FOO=  hello world  ",
			want:  map[string]string{"FOO": "hello world"},
		},
		{
			name:  "empty value",
			input: "FOO=",
			want:  map[string]string{"FOO": ""},
		},
		{
			name:  "double quoted value",
			input: `FOO="hello world"`,
			want:  map[string]string{"FOO": "hello world"},
		},
		{
			name:  "double quoted with escapes",
			input: `FOO="line\nbreak\ttab\\slash\"quote"`,
			want:  map[string]string{"FOO": "line\nbreak\ttab\\slash\"quote"},
		},
		{
			name:  "single quoted value",
			input: `FOO='hello world'`,
			want:  map[string]string{"FOO": "hello world"},
		},
		{
			name:  "single quoted no escapes",
			input: `FOO='hello\nworld'`,
			want:  map[string]string{"FOO": `hello\nworld`},
		},
		{
			name:  "comment line",
			input: "# this is a comment\nFOO=bar",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "inline comment",
			input: "FOO=bar # this is a comment",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "export prefix",
			input: "export FOO=bar",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "blank lines ignored",
			input: "\n\nFOO=bar\n\nBAZ=qux\n\n",
			want:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:  "multiple entries",
			input: "A=1\nB=2\nC=3",
			want:  map[string]string{"A": "1", "B": "2", "C": "3"},
		},
		{
			name:  "key with spaces around equals",
			input: "FOO = bar",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:    "missing equals",
			input:   "FOOBAR",
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   "=value",
			wantErr: true,
		},
		{
			name:    "unterminated double quote",
			input:   `FOO="unterminated`,
			wantErr: true,
		},
		{
			name:    "unterminated single quote",
			input:   `FOO='unterminated`,
			wantErr: true,
		},
		{
			name:  "double quoted with inline hash",
			input: `FOO="value # not a comment"`,
			want:  map[string]string{"FOO": "value # not a comment"},
		},
		{
			name:  "empty input",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "only comments",
			input: "# comment1\n# comment2",
			want:  map[string]string{},
		},
		{
			name:  "value with equals sign",
			input: "FOO=bar=baz",
			want:  map[string]string{"FOO": "bar=baz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDotenv(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d: %v", len(got), len(tt.want), got)
			}
			for k, wantV := range tt.want {
				if gotV, ok := got[k]; !ok {
					t.Errorf("missing key %q", k)
				} else if gotV != wantV {
					t.Errorf("key %q: got %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

func TestLoadDotenvFile(t *testing.T) {
	t.Parallel()

	t.Run("file exists", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := filepath.Join(dir, ".env")
		if err := os.WriteFile(p, []byte("FOO=bar\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := LoadDotenvFile(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["FOO"] != "bar" {
			t.Errorf("got %q, want %q", got["FOO"], "bar")
		}
	})

	t.Run("file not exists", func(t *testing.T) {
		t.Parallel()
		got, err := LoadDotenvFile("/nonexistent/.env")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := filepath.Join(dir, ".env")
		if err := os.WriteFile(p, []byte("INVALID_LINE_NO_EQUALS\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadDotenvFile(p)
		if err == nil {
			t.Fatal("expected error for invalid file")
		}
	})
}

func TestValidateEnvName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"development", "development", true},
		{"production", "production", true},
		{"prod-1", "prod-1", true},
		{"test_env", "test_env", true},
		{"STAGING", "STAGING", true},
		{"empty", "", false},
		{"path traversal", "../../etc", false},
		{"spaces", "dev env", false},
		{"slash", "dev/prod", false},
		{"dot", "dev.prod", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidateEnvName(tt.input); got != tt.want {
				t.Errorf("ValidateEnvName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadDotenvDir(t *testing.T) {
	t.Parallel()

	t.Run("merge order", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		writeFile(t, dir, ".env", "A=base\nB=base\n")
		writeFile(t, dir, ".env.local", "A=local\n")
		writeFile(t, dir, ".env.development", "B=dev\nC=dev\n")
		writeFile(t, dir, ".env.development.local", "C=devlocal\n")

		got, err := LoadDotenvDir(dir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expect := map[string]string{"A": "local", "B": "dev", "C": "devlocal"}
		for k, wantV := range expect {
			if got[k] != wantV {
				t.Errorf("key %q: got %q, want %q", k, got[k], wantV)
			}
		}
	})

	t.Run("custom rnixEnv", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		writeFile(t, dir, ".env", "A=base\n")
		writeFile(t, dir, ".env.production", "A=prod\n")

		got, err := LoadDotenvDir(dir, "production")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["A"] != "prod" {
			t.Errorf("got %q, want %q", got["A"], "prod")
		}
	})

	t.Run("invalid rnixEnv", func(t *testing.T) {
		t.Parallel()
		_, err := LoadDotenvDir(t.TempDir(), "../../etc")
		if err == nil {
			t.Fatal("expected error for invalid rnixEnv")
		}
	})

	t.Run("no env files returns nil", func(t *testing.T) {
		t.Parallel()
		got, err := LoadDotenvDir(t.TempDir(), "development")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("default rnixEnv is development", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, ".env.development", "X=devval\n")

		got, err := LoadDotenvDir(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if got["X"] != "devval" {
			t.Errorf("got %q, want %q", got["X"], "devval")
		}
	})
}

func TestNewEnvLookup(t *testing.T) {
	t.Parallel()

	t.Run("dotenv vars take priority", func(t *testing.T) {
		t.Parallel()
		lookup := NewEnvLookup(map[string]string{"MY_KEY": "from-dotenv"})
		if got := lookup("MY_KEY"); got != "from-dotenv" {
			t.Errorf("got %q, want %q", got, "from-dotenv")
		}
	})

	t.Run("fallback to os.Getenv", func(t *testing.T) {
		t.Parallel()
		lookup := NewEnvLookup(map[string]string{})
		// PATH should always exist
		if got := lookup("PATH"); got == "" {
			t.Error("expected non-empty PATH from os.Getenv fallback")
		}
	})

	t.Run("nil map returns os.Getenv", func(t *testing.T) {
		t.Parallel()
		lookup := NewEnvLookup(nil)
		if got := lookup("PATH"); got == "" {
			t.Error("expected non-empty PATH from os.Getenv")
		}
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
