//go:build !darwin

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func pickSecurityScopedDirectory(ctx context.Context, title, initialDirectory string) (string, []byte, func(), error) {
	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: title, DefaultDirectory: initialDirectory})
	return path, nil, nil, err
}

func resolveSecurityScopedBookmark([]byte) (string, []byte, func(), error) {
	return "", nil, nil, nil
}
