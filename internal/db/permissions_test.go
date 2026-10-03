//go:build cgo

package db

import (
	"github.com/yourname/fileship/internal/model"
	"path/filepath"
	"testing"
)

func TestPermissionsUseFolderBoundariesAndMostSpecificRule(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "permissions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.CreateUser("reader", "test-password", false, "/"); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByUsername("reader")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []model.PathPermission{
		{UserID: user.ID, Path: "/", CanRead: true, CanWrite: true},
		{UserID: user.ID, Path: "private", CanRead: false, CanWrite: false},
		{UserID: user.ID, Path: "private/public", CanRead: true},
	} {
		if err := database.SetPathPermission(rule); err != nil {
			t.Fatal(err)
		}
	}
	for _, sample := range []struct {
		path    string
		allowed bool
	}{
		{"", true}, {"private", false}, {"private/secret.txt", false},
		{"private2/notes.txt", true}, {"private/public/readme.md", true},
	} {
		allowed, err := database.CheckPermission(user.ID, sample.path, "read")
		if err != nil || allowed != sample.allowed {
			t.Fatalf("%q: allowed=%v err=%v", sample.path, allowed, err)
		}
	}
	database.Close()
	if allowed, err := database.CheckPermission(user.ID, "public.txt", "read"); allowed || err == nil {
		t.Fatal("database failures must deny access")
	}
}
