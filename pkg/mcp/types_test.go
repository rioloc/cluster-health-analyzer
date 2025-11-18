package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_DecodeRequestCursosr(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		want    *RequestCursor
		wantErr error
	}{
		{
			name: "happy path",
			arg:  "eyJzdGFydCI6MTc2MjYxODI3MywiZW5kIjoxNzYzNTQ4ODczfQ==",
			want: &RequestCursor{
				Start: 1762618273,
				End:   1763548873,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeRequestCursor(tt.arg)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantErr, err)
		})
	}

}
