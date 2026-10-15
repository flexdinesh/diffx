package review

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestSharedCommentContracts(t *testing.T) {
	raw, err := os.ReadFile("../../test/contracts/comments.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name             string
		Comment          ReviewComment
		Repository       *RepositoryDiff
		Patch            *FilePatch
		Valid            bool
		Placement        CommentPlacement
		Export           *string
		ExportUnresolved *string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := c.Comment.Valid(); got != c.Valid {
				t.Fatalf("valid=%v want %v", got, c.Valid)
			}
			placement, err := Locate(c.Comment, c.Repository, func(ChangedFile) (FilePatch, error) {
				if c.Patch == nil {
					return FilePatch{}, errors.New("example has no patch")
				}
				return *c.Patch, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(placement, c.Placement) {
				t.Fatalf("placement=%#v want %#v", placement, c.Placement)
			}
			placed := c.Comment
			placed.Placement = &placement
			if c.Export != nil {
				if got := FormatComments([]ReviewComment{placed}, true); got != *c.Export {
					t.Fatalf("export mismatch:\n%s", got)
				}
			}
			if c.ExportUnresolved != nil {
				if got := FormatComments([]ReviewComment{placed}, false); got != *c.ExportUnresolved {
					t.Fatalf("unresolved export mismatch:\n%s", got)
				}
			}
		})
	}
}
