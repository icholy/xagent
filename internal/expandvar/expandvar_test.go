package expandvar

import (
	"errors"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		replace func(namespace, value string) (string, error)
		want    string
		wantErr bool
	}{
		{
			name:  "no variables",
			input: "hello world",
			replace: func(ns, v string) (string, error) {
				return "", nil
			},
			want: "hello world",
		},
		{
			name:  "single variable",
			input: "hello ${env:NAME}",
			replace: func(ns, v string) (string, error) {
				if ns == "env" && v == "NAME" {
					return "world", nil
				}
				return "", nil
			},
			want: "hello world",
		},
		{
			name:  "multiple variables",
			input: "${env:GREETING} ${env:NAME}!",
			replace: func(ns, v string) (string, error) {
				switch v {
				case "GREETING":
					return "Hello", nil
				case "NAME":
					return "World", nil
				}
				return "", nil
			},
			want: "Hello World!",
		},
		{
			name:  "different namespaces",
			input: "${env:FOO} ${secret:BAR}",
			replace: func(ns, v string) (string, error) {
				return ns + ":" + v, nil
			},
			want: "env:FOO secret:BAR",
		},
		{
			name:  "variable at start",
			input: "${env:START}end",
			replace: func(ns, v string) (string, error) {
				return "begin", nil
			},
			want: "beginend",
		},
		{
			name:  "variable at end",
			input: "start${env:END}",
			replace: func(ns, v string) (string, error) {
				return "finish", nil
			},
			want: "startfinish",
		},
		{
			name:  "only variable",
			input: "${env:ONLY}",
			replace: func(ns, v string) (string, error) {
				return "replaced", nil
			},
			want: "replaced",
		},
		{
			name:  "empty replacement",
			input: "before${env:EMPTY}after",
			replace: func(ns, v string) (string, error) {
				return "", nil
			},
			want: "beforeafter",
		},
		{
			name:  "replace error",
			input: "hello ${env:BAD}",
			replace: func(ns, v string) (string, error) {
				return "", errors.New("expansion failed")
			},
			want:    "hello ${env:BAD}",
			wantErr: true,
		},
		{
			name:  "unclosed brace ignored",
			input: "hello ${env:NAME",
			replace: func(ns, v string) (string, error) {
				return "REPLACED", nil
			},
			want: "hello ${env:NAME",
		},
		{
			name:  "no colon ignored",
			input: "hello ${envNAME}",
			replace: func(ns, v string) (string, error) {
				return "REPLACED", nil
			},
			want: "hello ${envNAME}",
		},
		{
			name:  "dollar sign without brace",
			input: "price is $100",
			replace: func(ns, v string) (string, error) {
				return "REPLACED", nil
			},
			want: "price is $100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Expand(tt.input, tt.replace)
			if (err != nil) != tt.wantErr {
				t.Errorf("Expand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Expand() = %q, want %q", got, tt.want)
			}
		})
	}
}
