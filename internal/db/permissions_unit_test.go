package db

import (
	"testing"

	"github.com/yourname/fileship/internal/model"
)

func TestPathPermissionAllowedLongestPrefixWins(t *testing.T) {
	perms := []model.PathPermission{
		{Path: "/", CanRead: true, CanWrite: false, CanDelete: false, CanShare: false},
		{Path: "projects", CanRead: true, CanWrite: true, CanDelete: false, CanShare: true},
		{Path: "projects/secret", CanRead: false, CanWrite: false, CanDelete: false, CanShare: false},
	}

	if !PathPermissionAllowed(perms, "readme.md", "read") {
		t.Fatal("expected root read to allow readme.md")
	}
	if PathPermissionAllowed(perms, "readme.md", "write") {
		t.Fatal("expected root write=false to deny readme.md write")
	}
	if !PathPermissionAllowed(perms, "projects/app/main.go", "write") {
		t.Fatal("expected projects write=true to allow projects/app/main.go")
	}
	if PathPermissionAllowed(perms, "projects/secret/keys.txt", "read") {
		t.Fatal("expected projects/secret read=false to override parent rule")
	}
	if !PathPermissionAllowed(perms, "projects-other/file.txt", "read") {
		t.Fatal("expected sibling prefix projects-other to fall back to root read=true")
	}
	if PathPermissionAllowed(perms, "projects-other/file.txt", "write") {
		t.Fatal("expected sibling prefix projects-other not to match projects rule")
	}
}
