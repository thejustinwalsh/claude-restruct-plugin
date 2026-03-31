package permit

import "testing"

func TestTokenizeBash(t *testing.T) {
	tests := []struct {
		name    string
		command string
		count   int
		first   string
	}{
		{"simple", "ls -la", 1, "ls"},
		{"pipe", "cat file | grep foo", 2, "cat"},
		{"and", "cd dir && make build", 2, "cd"},
		{"or", "test -f file || echo missing", 2, "test"},
		{"semicolon", "echo a; echo b", 2, "echo"},
		{"env assignment", "FOO=bar cmd arg", 1, "cmd"},
		{"multiple env", "A=1 B=2 exec cmd", 1, "exec"},
		{"empty", "", 0, ""},
		{"pipe chain", "cat f | sort | uniq | wc -l", 4, "cat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := TokenizeBash(tt.command)
			if len(tokens) != tt.count {
				t.Errorf("TokenizeBash(%q) = %d tokens, want %d", tt.command, len(tokens), tt.count)
				return
			}
			if tt.count > 0 && tokens[0].Command != tt.first {
				t.Errorf("first command = %q, want %q", tokens[0].Command, tt.first)
			}
		})
	}
}

func TestHasRedirection(t *testing.T) {
	if !HasRedirection("echo hello > file.txt") {
		t.Error("should detect >")
	}
	if !HasRedirection("ls >> log.txt") {
		t.Error("should detect >>")
	}
	if HasRedirection("ls -la") {
		t.Error("should not detect redirection in ls -la")
	}
}

func TestHasSubshell(t *testing.T) {
	if !HasSubshell("echo $(date)") {
		t.Error("should detect $()")
	}
	if !HasSubshell("echo `date`") {
		t.Error("should detect backticks")
	}
	if HasSubshell("echo hello") {
		t.Error("should not detect subshell")
	}
}

func TestHasEnvExpansion(t *testing.T) {
	if !HasEnvExpansion("echo $HOME") {
		t.Error("should detect $HOME")
	}
	if !HasEnvExpansion("echo ${PATH}") {
		t.Error("should detect ${PATH}")
	}
	if HasEnvExpansion("echo $? $!") {
		t.Error("should not detect $? or $!")
	}
	if HasEnvExpansion("echo hello") {
		t.Error("should not detect env expansion")
	}
}
