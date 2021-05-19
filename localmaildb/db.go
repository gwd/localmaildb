package localmaildb

import (
	"bytes"
	//"errors"
	"fmt"
	"log"
	"net/mail"

	"regexp"

	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"

	"gitlab.com/martyros/sqlutil/liteutil"
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

var headerPartFieldName = [...]string{
	HeaderPartFrom:    "From",
	HeaderPartReplyTo: "Reply-to",
	HeaderPartTo:      "To",
	HeaderPartCc:      "Cc",
	HeaderPartBcc:     "Bcc",
}

type MailDB struct {
	db *sqlx.DB
}

// Fill in mbd.mailbox.mailboxId from mbd.mailbox.MailboxName, if it
// hasn't been filled in already.
func mailboxNameToIdTx(eq sqlx.Ext, mailboxname string) (int, error) {
	var mailboxId int
	log.Printf("Looking up mailbox %s", mailboxname)
	err := sqlx.Get(eq, &mailboxId, `select mailboxid from lmdb_mailboxes where mailboxname = ?`,
		mailboxname)
	if err != nil {
		return 0, err
	}

	return mailboxId, nil
}

// Create the mailbox as described if it doesn't exist
func (mdb *MailDB) CreateMailbox(mailboxname string) error {
	_, err := mdb.db.Exec(`insert into lmdb_mailboxes(mailboxname) values(?)`,
		mailboxname)
	if err != nil {
		return fmt.Errorf("Inserting mailbox id: %w", err)
	}
	return nil
}

// AttachMailDB takes an existing DB connection and returns a MailDB
// object.  It will create the lmdb schema tables if they don't exist.
func AttachMailDB(db *sqlx.DB) (*MailDB, error) {
	mdb := &MailDB{db: db}

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
            date      date  not null,
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

	// err = mdb.initMailboxIdTx(tx)
	// if err != nil {
	// 	goto out_rollback
	// }

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

func OpenMailDB(filename string) (*MailDB, error) {
	log.Printf("Opening database %s", filename)
	db, err := sqlx.Open("sqlite3", "file:"+filename+"?_fk=true&mode=rwc")

	if err != nil {
		return nil, fmt.Errorf("Opening database: %v", err)
	}

	return AttachMailDB(db)
}

func (mdb *MailDB) Close() {
	if mdb.db != nil {
		mdb.db.Close()
		mdb.db = nil
	}
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

func AddAddressTx(eq sqlx.Ext, addr *Address) (int, error) {
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

type Address struct {
	PersonalName string
	MailboxName  string
	HostName     string
}

var reEmail = regexp.MustCompile("(.*)@(.*)")

func convertAddress(in *mail.Address, out *Address) error {
	out.PersonalName = in.Name

	sub := reEmail.FindStringSubmatch(in.Address)
	if sub == nil {
		return fmt.Errorf("Badly formed email address: %v", in.Address)
	}
	out.MailboxName = sub[1]
	out.HostName = sub[2]

	return nil
}

func mailAddressToOurAddress(in []*mail.Address) ([]Address, error) {
	out := make([]Address, len(in))
	for i := range in {
		err := convertAddress(in[i], &out[i])
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// FIXME: Make a separate type for this and implement Is, so that
// we can specify which message id was present.

type Errno int

const (
	ErrMsgidPresent = Errno(iota)
	ErrParseError
)

var errMessage = []string{
	ErrMsgidPresent: "Message ID Present",
	ErrParseError:   "Parsing message",
}

func (e Errno) Error() string {
	if int(e) < len(errMessage) {
		return errMessage[e]
	}
	return ""
}

func (e Errno) wrap(t error) Error {
	return Error{Code: e, Suberror: t}
}

type Error struct {
	Code     Errno
	Suberror error
}

func (e Error) Unwrap() error {
	return e.Suberror
}

func (e Error) Is(target error) bool {
	switch t := target.(type) {
	case Errno:
		return t == e.Code
	case Error:
		return t.Code == e.Code
	default:
		return false
	}
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Code.Error(), e.Suberror)
}

func (mdb *MailDB) AddMessage(message []byte) error {
	db := mdb.db

	m, err := mail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		return ErrParseError.wrap(err)
	}

	envelope := map[HeaderPart][]Address{}
	for part, fieldName := range headerPartFieldName {
		if fieldName == "" {
			continue
		}

		theiraddr, err := m.Header.AddressList(fieldName)
		if err != nil {
			if err == mail.ErrHeaderNotPresent {
				continue
			}
			return ErrParseError.wrap(fmt.Errorf("Getting address list for field %s: %w",
				fieldName, err))
		}

		envelope[HeaderPart(part)], err = mailAddressToOurAddress(theiraddr)
		if err != nil {
			return fmt.Errorf("Converting addresses: %w", err)
		}
	}

	messageId := m.Header.Get("Message-ID")
	inReplyTo := m.Header.Get("In-Reply-To")
	subject := m.Header.Get("Subject")
	date, err := m.Header.Date()
	if err != nil {
		return ErrParseError.wrap(fmt.Errorf("Parsing message date: %w", err))
	}

	err = txutil.TxLoopDb(db, func(eq sqlx.Ext) error {
		// Uses function-wide tx and envelope variables.
		// Defined here rather than below to allow the goto.
		addressListHelper := func(addrlist []Address, headerPart HeaderPart) error {
			for i := range addrlist {
				addrId, err := AddAddressTx(eq, &addrlist[i])
				if err != nil {
					return fmt.Errorf("Adding address: %w", err)
				}
				_, err = eq.Exec(`
            insert into lmdb_envelopejoin(messageid, addressid, envelopepart)
                values(?, ?, ?)`,
					messageId, addrId,
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
			messageId, subject, date,
			message, inReplyTo, len(message))
		if err != nil {
			if liteutil.IsErrorConstraintUnique(err) {
				return ErrMsgidPresent.wrap(fmt.Errorf("Inserting messageid %s: %w", messageId, err))
			} else {
				return fmt.Errorf("Inserting message: %w", err)
			}
		}

		for part, list := range envelope {
			err := addressListHelper(list, part)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// Replace mailbox messageid list with the messageids
func (mdb *MailDB) UpdateMailbox(mailboxname string, messageIds []string) error {
	return txutil.TxLoopDb(mdb.db, func(eq sqlx.Ext) error {
		mboxId, err := mailboxNameToIdTx(eq, mailboxname)
		if err != nil || mboxId == 0 {
			return fmt.Errorf("Couldn't get mailbox Id!")
		}

		// First, delete all rows for this mailbox
		_, err = eq.Exec(`delete from lmdb_mailbox_join where mailboxid = ?`, mboxId)
		if err != nil {
			return fmt.Errorf("Deleting old mailbox entries: %w", err)
		}

		// Then insert new message IDs one by one, warning on errors
		for _, messageId := range messageIds {
			_, err = eq.Exec(`insert into lmdb_mailbox_join(mailboxid, messageid) values(?, ?)`,
				mboxId, messageId)
			if err != nil {
				// FIXME: What's this about?  We just dropped all the
				// entries above, there shouldn't be any constraint
				// violations unless there are duplicate message ids
				// in the list
				sqliteErr, ok := err.(sqlite3.Error)
				if ok && sqliteErr.Code == sqlite3.ErrConstraint {
					log.Printf("Inserting record (%d, %s) failed due to constraint, ignoring",
						mboxId, messageId)
					continue
				}

				return fmt.Errorf("Inserting record into mailbox: %w", err)
			}
		}

		return nil
	})
}
