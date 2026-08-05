package config

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid cpp", Config{Executable: "main", CppVersion: 17}, false},
		{"valid c", Config{Executable: "main", Language: "c", CVersion: 17}, false},
		{"valid hybrid", Config{Executable: "main", Language: "hybrid", CppVersion: 17, CVersion: 17}, false},
		{"empty executable", Config{Executable: "", CppVersion: 17}, true},
		{"bad cpp version", Config{Executable: "main", CppVersion: 999}, true},
		{"bad c version for c lang", Config{Executable: "main", Language: "c", CVersion: 999}, true},
		{"unknown language", Config{Executable: "main", Language: "rust", CppVersion: 17}, true},
		{"bad sanitizer name", Config{Executable: "main", CppVersion: 17, Sanitizers: []string{"asan"}}, true},
		{"valid sanitizer name", Config{Executable: "main", CppVersion: 17, Sanitizers: []string{"address"}}, false},
		{"schema version too new", Config{Executable: "main", CppVersion: 17, SchemaVersion: CurrentSchemaVersion + 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%+v) error = %v, wantErr %v", tt.cfg, err, tt.wantErr)
			}
		})
	}
}

func TestLanguageOrDefault(t *testing.T) {
	if got := LanguageOrDefault(""); got != "cpp" {
		t.Errorf("LanguageOrDefault(\"\") = %q, want \"cpp\"", got)
	}
	if got := LanguageOrDefault("c"); got != "c" {
		t.Errorf("LanguageOrDefault(\"c\") = %q, want \"c\"", got)
	}
}
