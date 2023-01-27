package localmaildb

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/emersion/go-imap"
	"github.com/jmoiron/sqlx"
	"gitlab.com/martyros/sqlutil/txutil"
)

type MessageTree struct {
	RawMessage       []byte
	Envelope         imap.Envelope
	Replies          []*MessageTree
	Earliest, Latest time.Time
}

// "Standard" code to scan a message row
func scanMessage(rows *sqlx.Rows) (*MessageTree, error) {
	message := &MessageTree{}

	var messageString string

	err := rows.Scan(&message.Envelope.MessageId,
		&message.Envelope.Subject,
		&message.Envelope.Date,
		&messageString)

	if err != nil {
		return nil, err
	}

	message.Latest = message.Envelope.Date
	message.RawMessage = []byte(messageString)

	return message, nil
}

func scanMessageList(eq sqlx.Ext, rows *sqlx.Rows) ([]*MessageTree, error) {
	messages := []*MessageTree{}

	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			log.Printf("Scanning results: %v", err)
			return nil, err
		}
		messages = append(messages, message)
	}

	rows.Close()

	// Sort by date order ascending
	sort.Slice(messages, func(i, j int) bool { return messages[i].Envelope.Date.Before(messages[j].Envelope.Date) })

	for i := range messages {
		// FIXME: Do the rest of the header parts
		message := messages[i]
		err := sqlx.Select(eq, &message.Envelope.From, `
		select personalname, mailboxname, hostname
			from lmdb_messages 
				natural join lmdb_envelopejoin
				natural join lmdb_addresses
			where messageid=?
			  and envelopepart=?`, message.Envelope.MessageId, HeaderPartFrom)
		if err != nil {
			return nil, fmt.Errorf("Getting From envelope for messageid %s: %w",
				message.Envelope.MessageId, err)
		}
	}

	return messages, nil
}

func (mdb *MailDB) GetMessageRoots(mailboxname string) ([]*MessageTree, error) {
	var messages []*MessageTree

	err := txutil.TxLoopDb(mdb.db, func(eq sqlx.Ext) error {
		mboxid, err := mailboxNameToIdTx(eq, mailboxname)
		if err != nil {
			return err
		}

		// Find all messages in the inbox.
		// Find the "roots" of all these trees
		// Return them.
		rows, err := eq.Queryx(`
        WITH RECURSIVE
            ancestor(messageid) AS
                (select messageid 
                     from lmdb_mailbox_join
                     where mailboxid=?
                 union
                 select lmdb_messages.inreplyto
                     from lmdb_messages join ancestor using(messageid))
        select self.messageid, self.subject, self.date, self.message
            from lmdb_messages as self
                left join lmdb_messages as parent
                on self.inreplyto = parent.messageid
        where self.messageid in ancestor
              and IFNULL(parent.messageid, TRUE)`, mboxid)
		if err != nil {
			return fmt.Errorf("Error getting 'root' message list: %w", err)
		}

		messages, err = scanMessageList(eq, rows)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return messages, nil
}

func getTreeTx(eq sqlx.Ext, root *MessageTree) error {
	// Get all messages in-reply-to the root
	rows, err := eq.Queryx(`
        select messageid, subject, date, message
            from lmdb_messages where inreplyto = $messageid`,
		root.Envelope.MessageId)
	if err != nil {
		return fmt.Errorf("Getting reply message list for messageid %s: %w",
			root.Envelope.MessageId, err)
	}

	if root.Replies, err = scanMessageList(eq, rows); err != nil {
		return err
	}

	// And get all the messages for those
	for _, message := range root.Replies {
		if err := getTreeTx(eq, message); err != nil {
			return err
		}
	}

	return nil
}

func (mdb *MailDB) GetTree(root *MessageTree) error {
	return txutil.TxLoopDb(mdb.db, func(eq sqlx.Ext) error {
		return getTreeTx(eq, root)
	})
}
