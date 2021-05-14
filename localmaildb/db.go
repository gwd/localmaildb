package localmaildb

import (
	"fmt"
	"io/ioutil"
	"log"

	"github.com/emersion/go-imap"
	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"

	"gitlab.com/martyros/sqlutil/txutil"
)

// Open maildb

// Add connection info

// Update new mail

// Set up hooks / notifications for events

type HeaderPart int

const (
	HeaderPartFrom    = HeaderPart(1)
	HeaderPartSender  = HeaderPart(2)
	HeaderPartReplyTo = HeaderPart(3)
	HeaderPartTo      = HeaderPart(4)
	HeaderPartCc      = HeaderPart(5)
	HeaderPartBcc     = HeaderPart(6)
)

// Fill in mbd.mailbox.mailboxId from mbd.mailbox.MailboxName, if it
// hasn't been filled in already.  Errors are currently ignored.
func (mdb *MailDB) getMailboxIdTx(eq sqlx.Ext) (int, error) {
	if mdb.mailbox.mailboxId > 0 {
		return mdb.mailbox.mailboxId, nil
	}

	log.Printf("Looking up mailbox %s", mdb.mailbox.MailboxName)
	err := sqlx.Get(eq, &mdb.mailbox.mailboxId, `select mailboxid from lmdb_mailboxes where mailboxname = ?`,
		mdb.mailbox.MailboxName)
	if err != nil {
		return 0, nil
	}

	return mdb.mailbox.mailboxId, nil
}

// Create the mailbox as described if it doesn't exist
func (mdb *MailDB) initMailboxIdTx(eq sqlx.Ext) error {
	mboxid, err := mdb.getMailboxIdTx(eq)
	if err != nil {
		return err
	}

	if mboxid != 0 {
		return nil
	}

	_, err = eq.Exec(`insert into lmdb_mailboxes(mailboxname) values(?)`,
		mdb.mailbox.MailboxName)
	if err != nil {
		return fmt.Errorf("Inserting mailbox id: %w", err)
	}

	mboxid, err = mdb.getMailboxIdTx(eq)

	if mboxid == 0 {
		err = fmt.Errorf("Mailbox %s not present after creation!", mdb.mailbox.MailboxName)
	}

	return err
}

func AttachMailDB(db *sqlx.DB, mailbox *MailboxInfo) (*MailDB, error) {
	mdb := &MailDB{db: db, mailbox: *mailbox}

	log.Println("Creating tables if they don't exist")
	// Create tables if they don't exist
	tx, err := db.Beginx()
	if err != nil {
		goto out_close
	}

	_, err = tx.Exec(`
        create table if not exists lmdb_params(
            key       text primary key,
            value     text not null)`)
	if err != nil {
		err = fmt.Errorf("Creating table params: %v", err)
		goto out_rollback
	}
	_, err = tx.Exec(`
        insert into lmdb_params(key, value)
            values ("dbversion", "1")
            on conflict do nothing`)
	if err != nil {
		err = fmt.Errorf("Inserting version param: %v", err)
		goto out_rollback
	}

	_, err = tx.Exec(`
        create table if not exists lmdb_messages(
            messageid text primary key,
            subject   text not null,
            date      integer  not null, /* Unix seconds */
            message   text not null,
            inreplyto text,
            size      integer  not null)`)
	if err != nil {
		err = fmt.Errorf("Creating table messages: %v", err)
		goto out_rollback
	}
	_, err = tx.Exec(`
        create table if not exists lmdb_addresses(
            addressid    integer primary key,
            personalname text,
            mailboxname  text,
            hostname     text,
            unique(personalname, mailboxname, hostname))`)
	if err != nil {
		err = fmt.Errorf("Creating table addresses: %v", err)
		goto out_rollback
	}

	_, err = tx.Exec(`
        create table if not exists lmdb_envelopejoin(
            messageid text not null,
            addressid integer not null,
            envelopepart integer not null,
            foreign key(messageid) references lmdb_messages,
            foreign key(addressid) references lmdb_addresses)`)
	if err != nil {
		err = fmt.Errorf("Creating table envelopejoin: %v", err)
		goto out_rollback
	}

	_, err = tx.Exec(`
        create table if not exists lmdb_mailboxes(
            mailboxid integer primary key,
            mailboxname text)`)

	err = mdb.initMailboxIdTx(tx)
	if err != nil {
		goto out_rollback
	}

	_, err = tx.Exec(`
        create table if not exists lmdb_mailbox_join(
            mailboxid integer,
            messageid text,
            foreign key(mailboxid) references lmdb_mailboxes,
            foreign key(messageid) references lmdb_messages)`)

	tx.Commit()

	return mdb, nil

out_rollback:
	tx.Rollback()
out_close:
	db.Close()
	return nil, err
}

func OpenMailDB(filename string, mailbox *MailboxInfo) (*MailDB, error) {
	log.Printf("Opening database %s", filename)
	db, err := sqlx.Open("sqlite3", "file:"+filename+"?_fk=true&mode=rwc")

	if err != nil {
		return nil, fmt.Errorf("Opening database: %v", err)
	}

	return AttachMailDB(db, mailbox)
}

// NB: It would be nice to be able to run a query like:
//   select messageid from msgidquerylist
//   where messageid not in (select messageid from messages);
//
// Theoretically this could be done by using `with`:
//   with msgidquerylist(messageid) as (values (?), (?))
// But:
//
//  1) This requires generating a query with enough `(?), ` instances,
//  and making sure they line up right in the bound variables
//
//  2) There is a limit to the number of bound variables; by default
//  999 apparently; which is less than the number of messages
//  currently in my inbox.  So this would rely on several sets of
//  filtering... sounds like a pain.
//
// The other option might be to add a vtable callback for msgidquerylist.
//
// For now, just go with the slow-and-simple option of checking each
// message id individually.
//
// In reality, this is only an optimizaiton: any races mean that we
// waste time downloading a message that ends up being present already
// when we're done.

func (mdb *MailDB) IsMsgIdPresent(msgid string) (bool, error) {
	rows, err := mdb.db.Query("select messageid from lmdb_messages where messageid = ?", msgid)
	if err != nil {
		return false, fmt.Errorf("Querying for messageid %v: %v", msgid, err)
	}
	defer rows.Close()

	// If a row was returned, the messageid was present
	if rows.Next() {
		return true, nil
	}

	// If rows.Next() returns false, it may because the results were
	// empty, or because of an error.  figure out which one it was.
	err = rows.Err()
	if err != nil {
		return false, fmt.Errorf("Scanning query results: %v", err)
	}

	return false, nil
}

func AddAddressTx(eq sqlx.Ext, addr *imap.Address) (int, error) {
	// We could remove the `on conflict`, and on success
	// LastInsertId() to get the row Id n the address.  However,
	// there's a part of me that doesn't trust that the rowid will
	// always end up being the address id.  Since we need to write
	// code to get the address id on duplicate entries anyway, just
	// always do the query.

	_, err := eq.Exec(`
        insert into lmdb_addresses(personalname, mailboxname, hostname)
            values (?, ?, ?)
            on conflict do nothing`,
		addr.PersonalName, addr.MailboxName, addr.HostName)
	if err != nil {
		return -1, fmt.Errorf("Inserting address: %w", err)
	}

	rows, err := eq.Query(
		`select addressid from lmdb_addresses
             where personalname=?
                   and mailboxname=?
                   and hostname=?`,
		addr.PersonalName, addr.MailboxName, addr.HostName)
	if err != nil {
		return -1, fmt.Errorf("Getting id for last insert: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return -1, fmt.Errorf("Address query returned no results!")
	}

	addrid := -1
	err = rows.Scan(&addrid)
	if err != nil {
		return -1, err
	}

	return addrid, nil
}

func (mdb *MailDB) AddMessage(envelope *imap.Envelope, body imap.Literal) error {
	db := mdb.db

	// This seems to be the amount of outstanding data left to read;
	// so we must read this first to get an accurate value.
	size := body.Len()

	// Read body into []bytes
	message, err := ioutil.ReadAll(body)
	if err != nil {
		return fmt.Errorf("Reading message text from imap.Literal: %v", err)
	}

	err = txutil.TxLoopDb(db, func(eq sqlx.Ext) error {
		// Uses function-wide tx and envelope variables.
		// Defined here rather than below to allow the goto.
		addressListHelper := func(addrlist []*imap.Address, headerPart HeaderPart, err error) error {
			// Pass through errors to handle once at the end
			if err != nil {
				return err
			}
			for _, addr := range addrlist {
				addrId, err := AddAddressTx(eq, addr)
				if err != nil {
					return fmt.Errorf("Adding address: %v", err)
				}
				_, err = eq.Exec(`
            insert into lmdb_envelopejoin(messageid, addressid, envelopepart)
                values(?, ?, ?)`,
					envelope.MessageId, addrId,
					headerPart)
				if err != nil {
					return fmt.Errorf("Inserting envelope join: %w", err)
				}
			}
			return nil
		}

		// Insert message: msgid, body, date, inreplyto, size
		// NB that automatic date conversion will give you a string instead of an integer
		_, err = eq.Exec(`
        insert into lmdb_messages(messageid, subject, date, message, inreplyto, size)
            values (?, ?, ?, ?, ?, ?)`,
			envelope.MessageId, envelope.Subject, envelope.Date.Unix(),
			message, envelope.InReplyTo, size)
		// FIXME: Gracefully handle duplicate message ids
		if err != nil {
			return fmt.Errorf("Inserting message: %w", err)
		}

		err = addressListHelper(envelope.From, HeaderPartFrom, nil)
		err = addressListHelper(envelope.Sender, HeaderPartSender, err)
		err = addressListHelper(envelope.ReplyTo, HeaderPartReplyTo, err)
		err = addressListHelper(envelope.To, HeaderPartTo, err)
		err = addressListHelper(envelope.Cc, HeaderPartCc, err)
		err = addressListHelper(envelope.Bcc, HeaderPartBcc, err)

		return err
	})

	return err
}

func (mdb *MailDB) updateMailbox(messageIds []string) error {
	tx, err := mdb.db.Beginx()
	if err != nil {
		return fmt.Errorf("Starting database transaction")
	}

	mboxId, err := mdb.getMailboxIdTx(tx)
	if err != nil || mboxId == 0 {
		err = fmt.Errorf("Couldn't get mailbox Id!")
		goto out_rollback
	}

	// First, delete all rows for this mailbox
	_, err = tx.Exec(`delete from lmdb_mailbox_join where mailboxid = ?`, mboxId)
	if err != nil {
		goto out_rollback
	}

	// Then insert new message IDs one by one, warning on errors
	for _, messageId := range messageIds {
		_, err = tx.Exec(`insert into lmdb_mailbox_join(mailboxid, messageid) values(?, ?)`,
			mboxId, messageId)
		if err != nil {
			sqliteErr, ok := err.(sqlite3.Error)
			if ok && sqliteErr.Code == sqlite3.ErrConstraint {
				log.Printf("Inserting record (%d, %s) failed due to constraint, ignoring",
					mboxId, messageId)
			} else {
				goto out_rollback
			}
		}
	}

	tx.Commit()
	return nil

out_rollback:
	tx.Rollback()
	return err
}
