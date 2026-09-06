//go:build js && wasm

package d1

import (
	"syscall/js"
	"testing"
)

func TestResult_LastInsertId_RowsAffected(t *testing.T) {
	tests := map[string]struct {
		meta            map[string]any
		wantLastInsert  int64
		wantLastInsertE bool
		wantRowsAffect  int64
		wantRowsAffectE bool
	}{
		"present": {
			meta:           map[string]any{"last_row_id": 7, "changes": 2},
			wantLastInsert: 7,
			wantRowsAffect: 2,
		},
		"missing": {
			meta:            map[string]any{},
			wantLastInsertE: true,
			wantRowsAffectE: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			r := &result{
				resultObj: js.ValueOf(map[string]any{"meta": tc.meta}),
			}

			id, err := r.LastInsertId()
			if (err != nil) != tc.wantLastInsertE {
				t.Fatalf("LastInsertId() error = %v, wantErr %v", err, tc.wantLastInsertE)
			}
			if err == nil && id != tc.wantLastInsert {
				t.Fatalf("LastInsertId() = %v, want %v", id, tc.wantLastInsert)
			}

			n, err := r.RowsAffected()
			if (err != nil) != tc.wantRowsAffectE {
				t.Fatalf("RowsAffected() error = %v, wantErr %v", err, tc.wantRowsAffectE)
			}
			if err == nil && n != tc.wantRowsAffect {
				t.Fatalf("RowsAffected() = %v, want %v", n, tc.wantRowsAffect)
			}
		})
	}
}
