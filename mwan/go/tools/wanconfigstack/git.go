package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// clonePinned checks a component out at its pin: a shallow single-tag clone
// when the pin is a tag, otherwise a full clone checked out at the commit
// (nghttp2-asio pins a commit because it has no release tags).
func (b *builder) clonePinned(ctx context.Context, c component) error {
	dir := b.srcDir(c)
	pin := b.pin(c)
	if err := os.RemoveAll(dir); err != nil {
		return b.fail(ctx, "clear source dir", err, slog.String("dir", dir))
	}
	b.log.InfoContext(ctx, "wanconfigstack: clone", "url", c.url, "pin", pin, "dir", dir)
	_, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           c.url,
		ReferenceName: plumbing.NewTagReferenceName(pin),
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, git.NoMatchingRefSpecError{}) && !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return b.fail(ctx, "clone tag", err, slog.String("url", c.url), slog.String("pin", pin))
	}
	if err := os.RemoveAll(dir); err != nil {
		return b.fail(ctx, "clear source dir", err, slog.String("dir", dir))
	}
	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{URL: c.url, NoCheckout: true, Tags: git.NoTags})
	if err != nil {
		return b.fail(ctx, "clone", err, slog.String("url", c.url))
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(pin))
	if err != nil {
		return b.fail(ctx, "resolve commit", err, slog.String("url", c.url), slog.String("pin", pin))
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return b.fail(ctx, "open worktree", err, slog.String("dir", dir))
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: *hash, Force: true}); err != nil {
		return b.fail(ctx, "checkout commit", err, slog.String("dir", dir), slog.String("commit", hash.String()))
	}
	return nil
}
