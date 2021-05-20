package pubinboxsrc

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	lmdb "github.com/gwd/localmaildb/localmaildb"
)

type PublicInboxInfo struct {
	Path string // Path to "top-level" public-inbox for this list / address
}

type PublicInboxSrc struct {
	gitpath string // Path to directory of git repos.
}

func (src *PublicInboxSrc) Close() {
}

// For now just check to make sure the path exists and has the
// expected structure
func Connect(info PublicInboxInfo) (*PublicInboxSrc, error) {
	src := &PublicInboxSrc{gitpath: path.Clean(path.Join(info.Path, "git"))}

	_, err := os.ReadDir(src.gitpath)
	if err != nil {
		return nil, fmt.Errorf("Reading public-inbox path: %w", err)
	}

	return src, nil
}

// For now we don't do any cloning or fetching; just start at he head
// and work backwards until we find a messageid we've seen before
func (src *PublicInboxSrc) Fetch(mdb *lmdb.MailDB) error {
	entries, err := os.ReadDir(src.gitpath)
	if err != nil {
		return fmt.Errorf("Reading gitdir %s: %w", src.gitpath, err)
	}

	lastMsg := time.Now()
	count := 0
	skipped := 0
	log.Printf("Fetching messages...")

	// Entries are already sorted; work backwards
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !e.IsDir() {
			//log.Printf("%s not a directory, skipping", e.Name())
			continue
		}

		rpath := path.Join(src.gitpath, e.Name())

		repo, err := git.PlainOpen(rpath)
		if err != nil {
			// FIXME: Continue if we can tell it's just not a valid git repo?
			return fmt.Errorf("Opening git repo at %s: %w", rpath, err)
		}

		starthash, err := repo.ResolveRevision("master")
		if err != nil {
			return fmt.Errorf("Getting master revision: %w", err)
		}

		log.Printf("Processing directory %s, starting from revision %v", rpath, *starthash)

		wt, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("Getting worktree: %w", err)
		}

		iter, err := repo.Log(&git.LogOptions{From: *starthash, Order: git.LogOrderBSF})
		if err != nil {
			return fmt.Errorf("Getting log iterator: %w", err)
		}

		// There's a single mail file in the repo called 'm'
		mpath := path.Join(rpath, "m")

		repoCount := 0

		err = iter.ForEach(func(c *object.Commit) error {
			if time.Now().Sub(lastMsg) > time.Second*3 {
				lastMsg = time.Now()
				log.Printf("...added %d mails total, %d from this repo (%d skipped).  Current date %v", count, repoCount, skipped, c.Author.When)
			}

			// Check out this version
			err := wt.Checkout(&git.CheckoutOptions{Hash: c.Hash})
			if err != nil {
				return fmt.Errorf("Checking out revision %v: %w",
					c.Hash, err)
			}

			// Read the mail
			rawmail, err := ioutil.ReadFile(mpath)
			if err != nil {
				return fmt.Errorf("Reading file %s: %w", mpath, err)
			}

			// Try to add it to the maildb
			err = mdb.AddMessage(rawmail)
			switch {
			case err == nil:
				count++
				repoCount++
				return nil
			case errors.Is(err, lmdb.ErrMsgidPresent):
				// Unfortunately, the publix-inbox archive for
				// xen-devel has duplicate mails for 2019 April 4 -
				// June 4, so we can't use MsgidPresent to detect
				// where we've gotten to at all.  We'll have to use
				// the git head when updating.
				skipped++
				return nil
			case errors.Is(err, lmdb.ErrParseError):
				skipped++
				return nil
			default:
				fmt.Println(string(rawmail))
				return fmt.Errorf("Adding message to database: %w", err)
			}
		})

		if err != nil {
			switch {
			case errors.Is(err, lmdb.ErrMsgidPresent):
				// We've found a message we've inserted before; break.
				break
			default:
				return err
			}
		}
	}

	log.Printf("Added %d messages (%d skipped)", count, skipped)

	return nil
}
