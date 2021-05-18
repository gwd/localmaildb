package localmaildb

import (
	"testing"

	"errors"
	"io/ioutil"
	"os"
	"path"
)

const charsetPath = "charset-testcases"

func TestCharset(t *testing.T) {
	dbfile, err := os.CreateTemp("", "charset-test")
	if err != nil {
		t.Errorf("Creating temp file for test database: %v", err)
		return
	}

	ents, err := os.ReadDir(charsetPath)
	if err != nil {
		t.Errorf("Opening charset test dir %s: %v", charsetPath, err)
		return
	}

	mdb, err := OpenMailDB(dbfile.Name())
	if err != nil {
		t.Errorf("Opening maildb file %s: %v", dbfile.Name(), err)
		return
	}

	for _, ent := range ents {
		fname := path.Join(charsetPath, ent.Name())
		rawmail, err := ioutil.ReadFile(fname)
		if err != nil {
			t.Errorf("Reading file %s: %v", fname, err)
			continue
		}

		// Try to add it to the maildb
		err = mdb.AddMessage(rawmail)
		if err != nil {
			if errors.Is(err, ErrParseError) {
				t.Logf("Error (%v).Is(ErrParseError), skipping message", err)
				continue
			}
			t.Errorf("ERROR: Adding message %s: %v", ent.Name(), err)
		}
	}
}
