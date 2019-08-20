package localmaildb

import (
	"database/sql"
	"fmt"
	"github.com/emersion/go-imap"
	"log"
	"sort"
	"time"
)

type MessageTree struct {
	RawMessage       []byte
	Envelope         imap.Envelope
	Replies          []*MessageTree
	Earliest, Latest time.Time
}

// "Standard" code to scan a message row
func scanMessage(rows *sql.Rows) (*MessageTree, error) {
	message := &MessageTree{}

	var dateSeconds int64
	var messageString string

	err := rows.Scan(&message.Envelope.MessageId,
		&message.Envelope.Subject,
		&dateSeconds,
		&messageString)

	if err != nil {
		return nil, err
	}

	message.Envelope.Date = time.Unix(dateSeconds, 0)
	message.Latest = message.Envelope.Date
	message.RawMessage = []byte(messageString)

	return message, nil
}

func scanMessageList(rows *sql.Rows) ([]*MessageTree, error) {
	messages := []*MessageTree{}

	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			log.Printf("Scanning results: %v", err)
			return nil, err
		}
		messages = append(messages, message)
	}

	// Sort by date order ascending
	sort.Slice(messages, func(i, j int) bool { return messages[i].Envelope.Date.Before(messages[j].Envelope.Date) })

	return messages, nil
}

// FIXME:
func (mdb *MailDB) GetMessageRoots() ([]*MessageTree, error) {
	mboxid := mdb.mailbox.mailboxId
	// FIXME
	if mboxid == 0 {
		return nil, fmt.Errorf("No mailbox id!")
	}

	// Find all messages in the inbox.
	// Find the "roots" of all these trees
	// Return them.
	rows, err := mdb.db.Query(`
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
		log.Printf("Error getting 'root' message list: %v", err)
		return nil, err
	}
	defer rows.Close()

	messages, err := scanMessageList(rows)

	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (mdb *MailDB) GetTree(root *MessageTree) error {
	// Get all messages in-reply-to the root
	rows, err := mdb.db.Query(`
        select messageid, subject, date, message
            from lmdb_messages where inreplyto = $messageid`,
		root.Envelope.MessageId)
	if err != nil {
		log.Printf("Error getting reply message list for messageid %s: %v",
			root.Envelope.MessageId, err)
		return err
	}
	defer rows.Close()

	root.Replies, err = scanMessageList(rows)
	if err != nil {
		return err
	}

	// And get all the messages for those
	for _, messages := range root.Replies {
		err = mdb.GetTree(messages)
		if err != nil {
			return err
		}
	}

	return nil
}
